// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Controller synchronises Gateway API resources (HTTPRoute, GRPCRoute, Gateway)
// from Kubernetes to Vrata via its REST API. Changes are batched and published
// as versioned snapshots.
//
// Usage:
//
//	vrata-controller --config /path/to/config.yaml
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/homedir"

	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	vrataapiv1 "github.com/achetronic/vrata/clients/controller/apis/v1"

	"github.com/achetronic/vrata/clients/controller/internal/batcher"
	"github.com/achetronic/vrata/clients/controller/internal/config"
	"github.com/achetronic/vrata/clients/controller/internal/dedup"
	"github.com/achetronic/vrata/clients/controller/internal/mapper"
	kcmetrics "github.com/achetronic/vrata/clients/controller/internal/metrics"
	"github.com/achetronic/vrata/clients/controller/internal/reconciler"
	"github.com/achetronic/vrata/clients/controller/internal/refgrant"
	"github.com/achetronic/vrata/clients/controller/internal/status"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"
	"github.com/achetronic/vrata/clients/controller/internal/workqueue"
)

// main is the entry point for the controller binary.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "vrata-controller: %v\n", err)
		os.Exit(1)
	}
}

// run executes the controller lifecycle: config, k8s client, reconciler, watch loop.
func run() error {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg)

	// Bridge slog to controller-runtime's logr so that internal components
	// (cache, client, informers) use the same structured logger.
	crlog.SetLogger(slogLogr(logger))

	// Build k8s REST config.
	restCfg, err := buildK8sConfig()
	if err != nil {
		return fmt.Errorf("building k8s config: %w", err)
	}

	// Register Gateway API types.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(gwapiv1beta1.Install(scheme))
	utilruntime.Must(vrataapiv1.Install(scheme))

	// Build controller-runtime k8s client (for status writes).
	k8sClient, err := runtimeclient.New(restCfg, runtimeclient.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating k8s client: %w", err)
	}

	// Build controller-runtime cache for watching resources.
	cacheOpts := cache.Options{Scheme: scheme}
	if len(cfg.Watch.Namespaces) > 0 {
		byNs := make(map[string]cache.Config, len(cfg.Watch.Namespaces))
		for _, ns := range cfg.Watch.Namespaces {
			byNs[ns] = cache.Config{}
		}
		cacheOpts.DefaultNamespaces = byNs
	}

	informerCache, err := cache.New(restCfg, cacheOpts)
	if err != nil {
		return fmt.Errorf("creating informer cache: %w", err)
	}

	// Build Vrata client, reconciler, batcher, status writer, dedup detector.
	var vrataOpts []vrata.Option
	if cfg.TLS != nil {
		httpClient, err := buildVrataHTTPClient(cfg.TLS)
		if err != nil {
			return fmt.Errorf("building vrata TLS client: %w", err)
		}
		vrataOpts = append(vrataOpts, vrata.WithHTTPClient(httpClient))
	}
	if cfg.APIKey != "" {
		vrataOpts = append(vrataOpts, vrata.WithAPIKey(cfg.APIKey))
	}
	vrataClient := vrata.NewClient(cfg.ControlPlaneURL, vrataOpts...)
	rec := reconciler.NewReconciler(vrataClient, logger)
	// Parse errors fall through to zero value, caught by the default below.
	debounce, _ := time.ParseDuration(cfg.Snapshot.Debounce)
	if debounce == 0 {
		debounce = 5 * time.Second
	}
	// Parse errors fall through to zero value, caught by the default below.
	batchIdleTimeout, _ := time.ParseDuration(cfg.Snapshot.BatchIdleTimeout)
	if batchIdleTimeout == 0 {
		batchIdleTimeout = 10 * time.Second
	}
	bat := batcher.New(vrataClient, debounce, cfg.Snapshot.MaxBatch, logger)
	statusWriter := status.NewWriter(k8sClient)

	dupMode := cfg.DuplicatesMode()
	var detector *dedup.Detector
	if dupMode != config.DuplicateModeOff {
		detector = dedup.NewDetector(logger)
	}

	wq := workqueue.New(logger)
	knownSingles := make(map[string]bool)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Init reconciler (rebuild refcount from Vrata).
	if err := rec.Init(ctx); err != nil {
		logger.Warn("reconciler init failed (Vrata may be empty)", slog.String("error", err.Error()))
	}

	// Health endpoint.
	var healthy atomic.Bool
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			if healthy.Load() {
				w.WriteHeader(200)
				// Health probe response — write error is not actionable.
				_, _ = w.Write([]byte("ok"))
			} else {
				w.WriteHeader(503)
				// Health probe response — write error is not actionable.
				_, _ = w.Write([]byte("not ready"))
			}
		})
		srv := &http.Server{Addr: ":8081", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			<-ctx.Done()
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutCancel()
			// Best-effort shutdown — error is not actionable.
			_ = srv.Shutdown(shutCtx)
		}()
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server failed", slog.String("error", err.Error()))
		}
	}()

	// Metrics server.
	var m *kcmetrics.Metrics
	if cfg.Metrics.Enabled {
		m = kcmetrics.New()
		bat.SetOnSnapshot(func() { m.SnapshotsCreated.Inc() })
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", m.Handler())
			srv := &http.Server{Addr: cfg.Metrics.Address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			go func() {
				<-ctx.Done()
				shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutCancel()
				srv.Shutdown(shutCtx)
			}()
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("metrics server failed", slog.String("error", err.Error()))
			}
		}()
		logger.Info("metrics server listening", slog.String("address", cfg.Metrics.Address))
	}

	// ReferenceGrant checker for cross-namespace backend references.
	refGrantChecker := refgrant.NewChecker(k8sClient, logger)

	// Start informer cache in background.
	go func() {
		if err := informerCache.Start(ctx); err != nil {
			logger.Error("informer cache failed", slog.String("error", err.Error()))
		}
	}()

	if !informerCache.WaitForCacheSync(ctx) {
		return fmt.Errorf("informer cache sync failed")
	}

	logger.Info("controller started",
		slog.String("vrata", cfg.ControlPlaneURL),
		slog.Bool("httpRoutes", cfg.WatchHTTPRoutes()),
		slog.Bool("grpcRoutes", cfg.WatchGRPCRoutes()),
		slog.Bool("superHttpRoutes", cfg.WatchSuperHTTPRoutes()),
		slog.Bool("gateways", cfg.WatchGateways()),
		slog.String("gatewayClassName", cfg.GatewayClass()),
	)

	// Build the reconcile loop as a function — called directly or via leader election.
	reconcileLoop := func(ctx context.Context) {
		syncCycle := func() {
			start := time.Now()

			// --- Phase 0: Circuit breaker for Vrata API ---
			// Fast-fail if Vrata API is completely unreachable to prevent O(M*N) failing calls.
			pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
			pingErr := rec.Client().Ping(pingCtx)
			pingCancel()
			if pingErr != nil {
				logger.Error("Vrata API unreachable, skipping sync cycle", slog.String("error", pingErr.Error()))
				if m != nil {
					m.ReconcileErrors.WithLabelValues("ping").Inc()
				}
				return
			}

			// Reset dedup detector before each cycle to clear stale entries.
			if detector != nil {
				detector.Reset()
			}

			// Clear knownSingles so deleted-and-recreated routes are re-enqueued.
			clear(knownSingles)

			// --- Phase 0: GatewayClass claim ---

			claimGatewayClass(ctx, informerCache, statusWriter, logger)

			// --- Phase 1: populate the work queue from the informer cache,
			// and build the full desired groups set for GC ---

			desiredGroups := make(map[string]bool)

			if cfg.WatchHTTPRoutes() {
				var routes gwapiv1.HTTPRouteList
				if err := informerCache.List(ctx, &routes); err != nil {
					logger.Error("listing HTTPRoutes for work queue", slog.String("error", err.Error()))
				} else {
					for i := range routes.Items {
						hr := &routes.Items[i]
						ref := workqueue.RouteRef{Namespace: hr.Namespace, Name: hr.Name}
						wq.Observe(ref, hr.Annotations, knownSingles)
						desiredGroups[fmt.Sprintf("k8s:%s/%s", hr.Namespace, hr.Name)] = true
					}
				}
			}
			if cfg.WatchGRPCRoutes() {
				var routes gwapiv1.GRPCRouteList
				if err := informerCache.List(ctx, &routes); err != nil {
					logger.Error("listing GRPCRoutes for work queue", slog.String("error", err.Error()))
				} else {
					for i := range routes.Items {
						gr := &routes.Items[i]
						ref := workqueue.RouteRef{Namespace: gr.Namespace, Name: gr.Name, GRPC: true}
						wq.Observe(ref, gr.Annotations, knownSingles)
						desiredGroups[fmt.Sprintf("k8s:%s/%s", gr.Namespace, gr.Name)] = true
					}
				}
			}
			if cfg.WatchSuperHTTPRoutes() {
				var routes vrataapiv1.SuperHTTPRouteList
				if err := informerCache.List(ctx, &routes); err != nil {
					logger.Error("listing SuperHTTPRoutes for work queue", slog.String("error", err.Error()))
				} else {
					for i := range routes.Items {
						shr := &routes.Items[i]
						ref := workqueue.RouteRef{Namespace: shr.Namespace, Name: shr.Name, Super: true}
						wq.Observe(ref, shr.Annotations, knownSingles)
						desiredGroups[fmt.Sprintf("k8s:%s/%s", shr.Namespace, shr.Name)] = true
					}
				}
			}

			// --- Phase 2: process the head of the work queue ---

			batchBlocking := false

			head := wq.Head()
			if head != nil {
				switch head.Kind {
				case workqueue.KindSingle:
					ref := head.Single
					changes, gName, err := reconcileSingleRoute(ctx, informerCache, rec, bat, statusWriter, detector, dupMode, refGrantChecker, m, logger, ref)
					if err != nil {
						logger.Error("reconciling single route",
							slog.String("namespace", ref.Namespace),
							slog.String("name", ref.Name),
							slog.String("error", err.Error()),
						)
					}
					if changes > 0 && gName != "" {
						desiredGroups[gName] = true
					}
					wq.Pop()

				case workqueue.KindBatch:
					bg := head.Batch
					if !bg.IsReady(batchIdleTimeout) {
						batchBlocking = true
						logger.Debug("workqueue: batch group accumulating",
							slog.String("batch", bg.Name),
							slog.Int("members", len(bg.Members)),
						)
					} else {
						if bg.IsIncomplete() {
							if cfg.Snapshot.BatchIncompletePolicy == config.BatchIncompletePolicyReject {
								logger.Error("workqueue: incomplete batch rejected, discarding",
									slog.String("batch", bg.Name),
									slog.Int("got", len(bg.Members)),
									slog.Int("expected", bg.ExpectedSize),
								)
								wq.Pop()
								break
							}
							logger.Error("workqueue: batch group timed out before all members arrived, applying partial set",
								slog.String("batch", bg.Name),
								slog.Int("got", len(bg.Members)),
								slog.Int("expected", bg.ExpectedSize),
							)
						} else {
							logger.Info("workqueue: batch group ready",
								slog.String("batch", bg.Name),
								slog.Int("members", len(bg.Members)),
							)
						}
						for _, ref := range bg.Members {
							refCopy := ref
							_, gName, err := reconcileSingleRoute(ctx, informerCache, rec, bat, statusWriter, detector, dupMode, refGrantChecker, m, logger, &refCopy)
							if err != nil {
								logger.Error("reconciling batch member",
									slog.String("batch", bg.Name),
									slog.String("namespace", ref.Namespace),
									slog.String("name", ref.Name),
									slog.String("error", err.Error()),
								)
							}
							if gName != "" {
								desiredGroups[gName] = true
							}
						}
						bat.Flush(ctx)
						wq.Pop()
					}
				}
			}

			// --- Phase 3: Gateways (only when not blocked by a batch) ---

			if !batchBlocking && cfg.WatchGateways() {
				if err := syncAllGateways(ctx, informerCache, rec, bat, statusWriter, cfg.GatewayClass(), logger); err != nil {
					logger.Error("Gateway sync failed", slog.String("error", err.Error()))
					if m != nil {
						m.ReconcileErrors.WithLabelValues("gateway").Inc()
					}
				} else if m != nil {
					m.ReconcileTotal.WithLabelValues("gateway", "success").Inc()
				}
			}

			// --- Phase 4: GC (only when not blocked by a batch) ---

			if !batchBlocking {
				gcOrphanedGroups(ctx, rec, bat, desiredGroups, logger)
			}

			if m != nil {
				m.ReconcileDuration.WithLabelValues("cycle").Observe(time.Since(start).Seconds())
				m.PendingChanges.Set(float64(bat.Pending()))
			}
		}

		syncCycle()
		bat.Flush(ctx)
		healthy.Store(true)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		logger.Info("controller watching for changes")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncCycle()
			}
		}
	}

	// Run with or without leader election.
	if cfg.LeaderElection.Enabled {
		logger.Info("leader election enabled",
			slog.String("lease", cfg.LeaderElection.LeaseName),
			slog.String("namespace", cfg.LeaderElection.LeaseNamespace),
		)
		k8sClientset, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			return fmt.Errorf("creating k8s clientset for leader election: %w", err)
		}
		// Hostname error is non-fatal — empty string is acceptable for leader identity.
		hostname, _ := os.Hostname()
		// Parse errors produce zero durations; leaderelection panics on zero,
		// but applyDefaults guarantees valid strings.
		leaseDuration, _ := time.ParseDuration(cfg.LeaderElection.LeaseDuration)
		renewDeadline, _ := time.ParseDuration(cfg.LeaderElection.RenewDeadline)
		retryPeriod, _ := time.ParseDuration(cfg.LeaderElection.RetryPeriod)

		lock := &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      cfg.LeaderElection.LeaseName,
				Namespace: cfg.LeaderElection.LeaseNamespace,
			},
			Client: k8sClientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: hostname,
			},
		}

		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   leaseDuration,
			RenewDeadline:   renewDeadline,
			RetryPeriod:     retryPeriod,
			ReleaseOnCancel: true,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					logger.Info("became leader")
					reconcileLoop(ctx)
				},
				OnStoppedLeading: func() {
					logger.Info("lost leadership")
				},
				OnNewLeader: func(identity string) {
					if identity != hostname {
						logger.Info("new leader elected", slog.String("leader", identity))
					}
				},
			},
		})
	} else {
		reconcileLoop(ctx)
	}

	logger.Info("controller shutting down")
	bat.Flush(context.Background())
	return nil
}

// claimGatewayClass finds and claims GatewayClasses matching our controller name,
// setting their Accepted condition. Only runs when a gatewayClassName is configured.
func claimGatewayClass(ctx context.Context, c cache.Cache, sw *status.Writer, logger *slog.Logger) {
	var gcList gwapiv1.GatewayClassList
	if err := c.List(ctx, &gcList); err != nil {
		logger.Error("listing GatewayClasses", slog.String("error", err.Error()))
		return
	}
	for i := range gcList.Items {
		gc := &gcList.Items[i]
		if gc.Spec.ControllerName != status.ControllerName {
			continue
		}

		alreadyAccepted := false
		for _, c := range gc.Status.Conditions {
			if c.Type == string(gwapiv1.GatewayClassConditionStatusAccepted) && c.Status == metav1.ConditionTrue && c.ObservedGeneration == gc.Generation {
				alreadyAccepted = true
				break
			}
		}
		if alreadyAccepted {
			continue
		}

		if err := sw.SetGatewayClassAccepted(ctx, gc, true, string(gwapiv1.GatewayClassReasonAccepted), "Accepted by Vrata controller"); err != nil {
			logger.Error("setting GatewayClass status", slog.String("name", gc.Name), slog.String("error", err.Error()))
		}
	}
}

// syncAllGateways lists all Gateways matching our gatewayClassName, reconciles
// their Listeners, writes Gateway status, and removes orphaned listeners.
func syncAllGateways(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, sw *status.Writer, className string, logger *slog.Logger) error {
	var gateways gwapiv1.GatewayList
	if err := c.List(ctx, &gateways); err != nil {
		return fmt.Errorf("listing Gateways: %w", err)
	}

	vrataClient := rec.Client()
	desiredNames := make(map[string]bool)

	for i := range gateways.Items {
		gw := &gateways.Items[i]

		if className != "" && string(gw.Spec.GatewayClassName) != className {
			continue
		}

		input := gatewayToInput(gw)
		listeners := mapper.MapGateway(input)
		allValid := true

		for _, l := range listeners {
			desiredNames[l.Name] = true
			existing, err := findListenerByName(ctx, vrataClient, l.Name)
			if err != nil {
				logger.Error("checking listener", slog.String("name", l.Name), slog.String("error", err.Error()))
				allValid = false
				continue
			}
			if existing == nil {
				if _, err := vrataClient.CreateListener(ctx, l); err != nil {
					logger.Error("creating listener", slog.String("name", l.Name), slog.String("error", err.Error()))
					allValid = false
					continue
				}
				bat.Signal(ctx)
			} else {
				if err := vrataClient.UpdateListener(ctx, existing.ID, l); err != nil {
					logger.Error("updating listener", slog.String("name", l.Name), slog.String("error", err.Error()))
					allValid = false
					continue
				}
				bat.Signal(ctx)
			}

			listenerInput := findListenerInput(input, l.Name)
			if listenerInput != nil {
				var listenerConds []metav1.Condition
				now := metav1.Now()
				if mapper.GatewayListenerProtocolSupported(listenerInput.Protocol) {
					supportedKinds := []gwapiv1.RouteGroupKind{
						{Group: (*gwapiv1.Group)(&gwapiv1.GroupVersion.Group), Kind: "HTTPRoute"},
					}
					if listenerInput.Protocol == "GRPC" || listenerInput.Protocol == "GRPCS" || listenerInput.Protocol == "HTTPS" {
						supportedKinds = append(supportedKinds, gwapiv1.RouteGroupKind{
							Group: (*gwapiv1.Group)(&gwapiv1.GroupVersion.Group), Kind: "GRPCRoute",
						})
					}
					
					listenerConds = append(listenerConds,
						metav1.Condition{
							Type:               string(gwapiv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: gw.Generation,
							LastTransitionTime: now,
							Reason:             string(gwapiv1.ListenerReasonAccepted),
							Message:            "Listener accepted",
						},
						metav1.Condition{
							Type:               string(gwapiv1.ListenerConditionProgrammed),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: gw.Generation,
							LastTransitionTime: now,
							Reason:             string(gwapiv1.ListenerReasonProgrammed),
							Message:            "Listener programmed in Vrata",
						},
					)
					
					// Update SupportedKinds in ListenerStatus
					for i, ls := range gw.Status.Listeners {
						if string(ls.Name) == listenerInput.Name {
							gw.Status.Listeners[i].SupportedKinds = supportedKinds
							break
						}
					}
				} else {
					listenerConds = append(listenerConds,
						metav1.Condition{
							Type:               string(gwapiv1.ListenerConditionAccepted),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: gw.Generation,
							LastTransitionTime: now,
							Reason:             string(gwapiv1.ListenerReasonUnsupportedProtocol),
							Message:            fmt.Sprintf("protocol %q is not supported", listenerInput.Protocol),
						},
					)
					allValid = false
				}
				if err := sw.SetListenerConditions(ctx, gw, listenerInput.Name, listenerConds); err != nil {
					logger.Error("setting listener status", slog.String("name", listenerInput.Name), slog.String("error", err.Error()))
				}
			}
		}

		if allValid {
			if err := sw.SetGatewayAccepted(ctx, gw, true, string(gwapiv1.GatewayReasonAccepted), "Accepted by Vrata controller"); err != nil {
				logger.Error("setting Gateway Accepted status", slog.String("name", gw.Name), slog.String("error", err.Error()))
			}
			if err := sw.SetGatewayProgrammed(ctx, gw, true, string(gwapiv1.GatewayReasonProgrammed), "All listeners programmed"); err != nil {
				logger.Error("setting Gateway Programmed status", slog.String("name", gw.Name), slog.String("error", err.Error()))
			}
		} else {
			if err := sw.SetGatewayAccepted(ctx, gw, true, string(gwapiv1.GatewayReasonListenersNotValid), "Some listeners could not be programmed"); err != nil {
				logger.Error("setting Gateway Accepted status", slog.String("name", gw.Name), slog.String("error", err.Error()))
			}
			if err := sw.SetGatewayProgrammed(ctx, gw, false, string(gwapiv1.GatewayReasonInvalid), "Some listeners failed"); err != nil {
				logger.Error("setting Gateway Programmed status", slog.String("name", gw.Name), slog.String("error", err.Error()))
			}
		}
	}

	// Garbage collect orphaned listeners.
	ownedNames, err := rec.OwnedListenerNames(ctx)
	if err != nil {
		logger.Error("listing owned listeners for GC", slog.String("error", err.Error()))
		return nil
	}
	for _, name := range ownedNames {
		if desiredNames[name] {
			continue
		}
		changes, err := rec.DeleteListenerByName(ctx, name)
		if err != nil {
			logger.Error("deleting orphaned listener", slog.String("name", name), slog.String("error", err.Error()))
			continue
		}
		for i := 0; i < changes; i++ {
			bat.Signal(ctx)
		}
	}

	return nil
}

// findListenerInput returns the GatewayListenerInput matching the given Vrata
// listener name. Returns nil if not found.
func findListenerInput(input mapper.GatewayInput, vrataName string) *mapper.GatewayListenerInput {
	for i, l := range input.Listeners {
		expected := fmt.Sprintf("k8s:%s/%s/%s", input.Namespace, input.Name, l.Name)
		if expected == vrataName {
			return &input.Listeners[i]
		}
	}
	return nil
}

// reconcileSingleRoute looks up one HTTPRoute, GRPCRoute, or SuperHTTPRoute
// from the cache and applies it to Vrata. Returns the number of changes,
// the group name for GC, and any error.
func reconcileSingleRoute(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, sw *status.Writer, detector *dedup.Detector, dupMode config.DuplicateMode, rgc *refgrant.Checker, m *kcmetrics.Metrics, logger *slog.Logger, ref *workqueue.RouteRef) (int, string, error) {
	groupName := fmt.Sprintf("k8s:%s/%s", ref.Namespace, ref.Name)

	if ref.GRPC {
		return reconcileGRPCRoute(ctx, c, rec, bat, sw, detector, dupMode, rgc, m, logger, ref, groupName)
	}
	return reconcileHTTPRoute(ctx, c, rec, bat, sw, detector, dupMode, rgc, m, logger, ref, groupName)
}

// reconcileHTTPRoute handles reconciliation of an HTTPRoute or SuperHTTPRoute.
func reconcileHTTPRoute(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, sw *status.Writer, detector *dedup.Detector, dupMode config.DuplicateMode, rgc *refgrant.Checker, m *kcmetrics.Metrics, logger *slog.Logger, ref *workqueue.RouteRef, groupName string) (int, string, error) {
	var input mapper.HTTPRouteInput
	var hr *gwapiv1.HTTPRoute

	if ref.Super {
		var shr vrataapiv1.SuperHTTPRoute
		if err := c.Get(ctx, runtimeclient.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &shr); err != nil {
			return 0, groupName, fmt.Errorf("getting SuperHTTPRoute %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		input = superHTTPRouteToInput(&shr)
	} else {
		var fetched gwapiv1.HTTPRoute
		if err := c.Get(ctx, runtimeclient.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &fetched); err != nil {
			return 0, groupName, fmt.Errorf("getting HTTPRoute %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		hr = &fetched
		input = gatewayHTTPRouteToInput(hr)

		if rgc != nil {
			denied := false
			for _, rule := range input.Rules {
				for _, br := range rule.BackendRefs {
					if br.ServiceNamespace != hr.Namespace {
						allowed, err := rgc.AllowedBackendRef(ctx, hr.Namespace, br.ServiceNamespace, br.ServiceName)
						if err != nil {
							logger.Error("checking ReferenceGrant",
								slog.String("namespace", hr.Namespace),
								slog.String("name", hr.Name),
								slog.String("error", err.Error()),
							)
						}
						if !allowed {
							denied = true
							if m != nil {
								m.RefGrantDenied.Inc()
							}
							if sw != nil {
								sw.SetResolvedRefs(ctx, hr, false, fmt.Sprintf(
									"cross-namespace backendRef %s/%s denied: no matching ReferenceGrant",
									br.ServiceNamespace, br.ServiceName,
								))
							}
							break
						}
					}
				}
				if denied {
					break
				}
			}
			if denied {
				return 0, groupName, nil
			}
		}
	}

	if detector != nil {
		ols := detector.Check(input)
		if len(ols) > 0 {
			if m != nil {
				m.OverlapsDetected.Inc()
			}
			if dupMode == config.DuplicateModeReject {
				if m != nil {
					m.OverlapsRejected.Inc()
				}
				msg := formatOverlapMessage(ols)
				logger.Warn("rejecting route due to overlapping paths",
					slog.String("namespace", ref.Namespace),
					slog.String("name", ref.Name),
					slog.String("overlaps", msg),
				)
				if sw != nil && hr != nil {
					sw.SetAccepted(ctx, hr, false, "OverlappingRoute", msg)
				}
				return 0, groupName, nil
			}
		}
	}

	mapped := mapper.MapHTTPRoute(input)
	changes, err := rec.ApplyHTTPRoute(ctx, mapped)
	if err != nil {
		if sw != nil && hr != nil {
			sw.SetAccepted(ctx, hr, false, "SyncFailed", err.Error())
		}
		return 0, groupName, fmt.Errorf("applying route %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if changes > 0 {
		for j := 0; j < changes; j++ {
			bat.Signal(ctx)
		}
		if sw != nil && hr != nil {
			sw.SetAccepted(ctx, hr, true, "Synced", "Successfully synced to Vrata")
		}
		if m != nil {
			m.ReconcileTotal.WithLabelValues("httproute", "success").Inc()
		}
	}
	return changes, groupName, nil
}

// convertGRPCToHTTPInput translates a GRPCRouteInput into an HTTPRouteInput
// purely for the purpose of overlap detection.
func convertGRPCToHTTPInput(input mapper.GRPCRouteInput) mapper.HTTPRouteInput {
	out := mapper.HTTPRouteInput{
		Name:      input.Name,
		Namespace: input.Namespace,
		Hostnames: input.Hostnames,
	}

	for _, r := range input.Rules {
		rout := mapper.RuleInput{
			BackendRefs: r.BackendRefs,
			Filters:     r.Filters,
		}
		for _, m := range r.Matches {
			mout := mapper.MatchInput{
				Method: "POST", // gRPC uses POST
				Headers: append([]mapper.HeaderMatchInput{
					{Name: "Content-Type", Value: "application/grpc"},
				}, m.Headers...),
			}

			if m.MatchType == "RegularExpression" {
				mout.PathType = "RegularExpression"
			} else {
				if m.MethodName != "" && m.ServiceName != "" {
					mout.PathType = "Exact"
					mout.PathValue = fmt.Sprintf("/%s/%s", m.ServiceName, m.MethodName)
				} else if m.ServiceName != "" {
					mout.PathType = "PathPrefix"
					mout.PathValue = fmt.Sprintf("/%s/", m.ServiceName)
				} else {
					mout.PathType = "PathPrefix"
					mout.PathValue = "/"
				}
			}
			rout.Matches = append(rout.Matches, mout)
		}
		out.Rules = append(out.Rules, rout)
	}
	return out
}

// reconcileGRPCRoute handles reconciliation of a GRPCRoute.
func reconcileGRPCRoute(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, sw *status.Writer, detector *dedup.Detector, dupMode config.DuplicateMode, rgc *refgrant.Checker, m *kcmetrics.Metrics, logger *slog.Logger, ref *workqueue.RouteRef, groupName string) (int, string, error) {
	var fetched gwapiv1.GRPCRoute
	if err := c.Get(ctx, runtimeclient.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &fetched); err != nil {
		return 0, groupName, fmt.Errorf("getting GRPCRoute %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	gr := &fetched
	input := grpcRouteToInput(gr)

	if rgc != nil {
		denied := false
		for _, rule := range input.Rules {
			for _, br := range rule.BackendRefs {
				if br.ServiceNamespace != gr.Namespace {
					allowed, err := rgc.AllowedBackendRef(ctx, gr.Namespace, br.ServiceNamespace, br.ServiceName)
					if err != nil {
						logger.Error("checking ReferenceGrant",
							slog.String("namespace", gr.Namespace),
							slog.String("name", gr.Name),
							slog.String("error", err.Error()),
						)
					}
					if !allowed {
						denied = true
						if m != nil {
							m.RefGrantDenied.Inc()
						}
						if sw != nil {
							sw.SetGRPCRouteResolvedRefs(ctx, gr, false, fmt.Sprintf(
								"cross-namespace backendRef %s/%s denied: no matching ReferenceGrant",
								br.ServiceNamespace, br.ServiceName,
							))
						}
						break
					}
				}
			}
			if denied {
				break
			}
		}
		if denied {
			return 0, groupName, nil
		}
	}

	if detector != nil {
		ols := detector.Check(convertGRPCToHTTPInput(input))
		if len(ols) > 0 {
			if m != nil {
				m.OverlapsDetected.Inc()
			}
			if dupMode == config.DuplicateModeReject {
				if m != nil {
					m.OverlapsRejected.Inc()
				}
				msg := formatOverlapMessage(ols)
				logger.Warn("rejecting grpc route due to overlapping paths",
					slog.String("namespace", ref.Namespace),
					slog.String("name", ref.Name),
					slog.String("overlaps", msg),
				)
				if sw != nil && gr != nil {
					sw.SetGRPCRouteAccepted(ctx, gr, false, "OverlappingRoute", msg)
				}
				return 0, groupName, nil
			}
		}
	}

	mapped := mapper.MapGRPCRoute(input)
	changes, err := rec.ApplyHTTPRoute(ctx, mapped)
	if err != nil {
		if sw != nil {
			sw.SetGRPCRouteAccepted(ctx, gr, false, "SyncFailed", err.Error())
		}
		return 0, groupName, fmt.Errorf("applying grpc route %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	if changes > 0 {
		for j := 0; j < changes; j++ {
			bat.Signal(ctx)
		}
		if sw != nil {
			sw.SetGRPCRouteAccepted(ctx, gr, true, "Synced", "Successfully synced to Vrata")
		}
		if m != nil {
			m.ReconcileTotal.WithLabelValues("grpcroute", "success").Inc()
		}
	}
	return changes, groupName, nil
}

// formatOverlapMessage builds a human-readable message listing all overlaps.
func formatOverlapMessage(ols []dedup.Overlap) string {
	var parts []string
	for _, ol := range ols {
		parts = append(parts, fmt.Sprintf(
			"%s %s on %s (from %s) overlaps with %s %s on %s (from %s)",
			ol.Incoming.PathType, ol.Incoming.Path, ol.Incoming.Hostname, ol.Incoming.Source,
			ol.Existing.PathType, ol.Existing.Path, ol.Existing.Hostname, ol.Existing.Source,
		))
	}
	return strings.Join(parts, "; ")
}

// superHTTPRouteToInput converts a SuperHTTPRoute to the mapper's input type.
// Since the spec is identical to HTTPRoute, it delegates to httpRouteSpecToInput.
func superHTTPRouteToInput(shr *vrataapiv1.SuperHTTPRoute) mapper.HTTPRouteInput {
	return httpRouteSpecToInput(shr.Namespace, shr.Name, &shr.Spec)
}

// findListenerByName searches for a listener by name in Vrata.
func findListenerByName(ctx context.Context, client *vrata.Client, name string) (*vrata.Entity, error) {
	listeners, err := client.ListListeners(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range listeners {
		if l.Name == name {
			return &vrata.Entity{ID: l.ID, Name: l.Name}, nil
		}
	}
	return nil, nil
}

// parseGroupName extracts namespace and name from a group name "k8s:namespace/name".
// Returns ("", "", false) if the format is invalid.
func parseGroupName(groupName string) (string, string, bool) {
	if !strings.HasPrefix(groupName, "k8s:") {
		return "", "", false
	}
	rest := groupName[4:]
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// gcOrphanedGroups deletes k8s: groups (and their routes, middlewares, destinations)
// from Vrata that no longer correspond to any HTTPRoute, GRPCRoute, or
// SuperHTTPRoute in Kubernetes.
func gcOrphanedGroups(ctx context.Context, rec *reconciler.Reconciler, bat *batcher.Batcher, desiredGroups map[string]bool, logger *slog.Logger) {
	ownedGroups, err := rec.OwnedGroupNames(ctx)
	if err != nil {
		logger.Error("listing owned groups for GC", slog.String("error", err.Error()))
		return
	}
	for _, groupName := range ownedGroups {
		if desiredGroups[groupName] {
			continue
		}
		ns, name, ok := parseGroupName(groupName)
		if !ok {
			continue
		}
		changes, err := rec.DeleteRouteGroup(ctx, ns, name)
		if err != nil {
			logger.Error("deleting orphaned entities",
				slog.String("group", groupName),
				slog.String("error", err.Error()),
			)
			continue
		}
		if changes > 0 {
			logger.Info("reconciler: garbage collected orphaned route group",
				slog.String("group", groupName),
				slog.Int("changes", changes),
			)
			for j := 0; j < changes; j++ {
				bat.Signal(ctx)
			}
		}
	}
}

// gatewayToInput converts a Gateway API Gateway to the mapper's input type.
func gatewayToInput(gw *gwapiv1.Gateway) mapper.GatewayInput {
	input := mapper.GatewayInput{
		Name:      gw.Name,
		Namespace: gw.Namespace,
	}
	for _, l := range gw.Spec.Listeners {
		gl := mapper.GatewayListenerInput{
			Name:     string(l.Name),
			Port:     uint32(l.Port),
			Protocol: string(l.Protocol),
		}
		if l.Hostname != nil {
			gl.Hostname = string(*l.Hostname)
		}
		if l.TLS != nil && len(l.TLS.CertificateRefs) > 0 {
			ref := l.TLS.CertificateRefs[0]
			ns := gw.Namespace
			if ref.Namespace != nil {
				ns = string(*ref.Namespace)
			}
			gl.CertRef = fmt.Sprintf("{{secret:%s/%s/tls.crt}}", ns, ref.Name)
			gl.KeyRef = fmt.Sprintf("{{secret:%s/%s/tls.key}}", ns, ref.Name)
		}
		input.Listeners = append(input.Listeners, gl)
	}
	return input
}

// gatewayHTTPRouteToInput converts a Gateway API HTTPRoute to the mapper's input type.
func gatewayHTTPRouteToInput(hr *gwapiv1.HTTPRoute) mapper.HTTPRouteInput {
	return httpRouteSpecToInput(hr.Namespace, hr.Name, &hr.Spec)
}

// grpcRouteToInput converts a Gateway API GRPCRoute to the mapper's input type.
func grpcRouteToInput(gr *gwapiv1.GRPCRoute) mapper.GRPCRouteInput {
	input := mapper.GRPCRouteInput{
		Name:      gr.Name,
		Namespace: gr.Namespace,
	}

	for _, h := range gr.Spec.Hostnames {
		input.Hostnames = append(input.Hostnames, string(h))
	}

	for _, rule := range gr.Spec.Rules {
		ri := mapper.GRPCRuleInput{}

		for _, match := range rule.Matches {
			mi := mapper.GRPCMatchInput{}
			if match.Method != nil {
				if match.Method.Service != nil {
					mi.ServiceName = *match.Method.Service
				}
				if match.Method.Method != nil {
					mi.MethodName = *match.Method.Method
				}
				if match.Method.Type != nil {
					mi.MatchType = string(*match.Method.Type)
				}
			}
			for _, hm := range match.Headers {
				hi := mapper.HeaderMatchInput{
					Name:  string(hm.Name),
					Value: hm.Value,
				}
				if hm.Type != nil {
					hi.Type = string(*hm.Type)
				}
				mi.Headers = append(mi.Headers, hi)
			}
			ri.Matches = append(ri.Matches, mi)
		}

		for _, br := range rule.BackendRefs {
			if (br.Group != nil && *br.Group != "core" && *br.Group != "") || (br.Kind != nil && *br.Kind != "Service") {
				continue
			}

			ns := gr.Namespace
			if br.Namespace != nil {
				ns = string(*br.Namespace)
			}
			port := uint32(0)
			if br.Port != nil {
				port = uint32(*br.Port)
			}
			weight := uint32(1)
			if br.Weight != nil {
				weight = uint32(*br.Weight)
			}
			ri.BackendRefs = append(ri.BackendRefs, mapper.BackendRefInput{
				ServiceName:      string(br.Name),
				ServiceNamespace: ns,
				Port:             port,
				Weight:           weight,
			})
		}

		for _, f := range rule.Filters {
			fi := mapper.FilterInput{Type: string(f.Type)}
			switch f.Type {
			case gwapiv1.GRPCRouteFilterRequestHeaderModifier:
				if f.RequestHeaderModifier != nil {
					for _, h := range f.RequestHeaderModifier.Add {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: true,
						})
					}
					for _, h := range f.RequestHeaderModifier.Set {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: false,
						})
					}
					for _, name := range f.RequestHeaderModifier.Remove {
						fi.HeadersToRemove = append(fi.HeadersToRemove, name)
					}
				}
			case gwapiv1.GRPCRouteFilterResponseHeaderModifier:
				if f.ResponseHeaderModifier != nil {
					for _, h := range f.ResponseHeaderModifier.Add {
						fi.ResponseHeadersToAdd = append(fi.ResponseHeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: true,
						})
					}
					for _, h := range f.ResponseHeaderModifier.Set {
						fi.ResponseHeadersToAdd = append(fi.ResponseHeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: false,
						})
					}
					for _, name := range f.ResponseHeaderModifier.Remove {
						fi.ResponseHeadersToRemove = append(fi.ResponseHeadersToRemove, name)
					}
				}
			case gwapiv1.GRPCRouteFilterRequestMirror:
				if f.RequestMirror != nil {
					fi.MirrorServiceName = string(f.RequestMirror.BackendRef.Name)
					ns := gr.Namespace
					if f.RequestMirror.BackendRef.Namespace != nil {
						ns = string(*f.RequestMirror.BackendRef.Namespace)
					}
					fi.MirrorServiceNamespace = ns
					if f.RequestMirror.BackendRef.Port != nil {
						fi.MirrorPort = uint32(*f.RequestMirror.BackendRef.Port)
					}
					if f.RequestMirror.Percent != nil {
						fi.MirrorPercent = uint32(*f.RequestMirror.Percent)
					} else if f.RequestMirror.Fraction != nil && f.RequestMirror.Fraction.Denominator != nil && *f.RequestMirror.Fraction.Denominator > 0 {
						fi.MirrorPercent = uint32(float64(f.RequestMirror.Fraction.Numerator) / float64(*f.RequestMirror.Fraction.Denominator) * 100)
					}
				}
			case gwapiv1.GRPCRouteFilterExtensionRef:
				if f.ExtensionRef != nil {
					fi.ExtensionGroup = string(f.ExtensionRef.Group)
					fi.ExtensionKind = string(f.ExtensionRef.Kind)
					fi.ExtensionName = string(f.ExtensionRef.Name)
				}
			}
			ri.Filters = append(ri.Filters, fi)
		}

		input.Rules = append(input.Rules, ri)
	}

	return input
}

// httpRouteSpecToInput converts a Gateway API HTTPRouteSpec into a mapper.HTTPRouteInput.
// Shared by both HTTPRoute and SuperHTTPRoute since their specs are identical.
func httpRouteSpecToInput(namespace, name string, spec *gwapiv1.HTTPRouteSpec) mapper.HTTPRouteInput {
	input := mapper.HTTPRouteInput{
		Name:      name,
		Namespace: namespace,
	}

	for _, h := range spec.Hostnames {
		input.Hostnames = append(input.Hostnames, string(h))
	}

	for _, rule := range spec.Rules {
		ri := mapper.RuleInput{}

		for _, match := range rule.Matches {
			mi := mapper.MatchInput{}
			if match.Path != nil {
				if match.Path.Type != nil {
					mi.PathType = string(*match.Path.Type)
				}
				if match.Path.Value != nil {
					mi.PathValue = *match.Path.Value
				}
			}
			if match.Method != nil {
				mi.Method = string(*match.Method)
			}
			for _, hm := range match.Headers {
				hi := mapper.HeaderMatchInput{
					Name:  string(hm.Name),
					Value: hm.Value,
				}
				if hm.Type != nil {
					hi.Type = string(*hm.Type)
				}
				mi.Headers = append(mi.Headers, hi)
			}
			for _, qp := range match.QueryParams {
				qi := mapper.QueryParamMatchInput{
					Name:  string(qp.Name),
					Value: qp.Value,
				}
				if qp.Type != nil {
					qi.Type = string(*qp.Type)
				}
				mi.QueryParams = append(mi.QueryParams, qi)
			}
			ri.Matches = append(ri.Matches, mi)
		}

		for _, br := range rule.BackendRefs {
			if (br.Group != nil && *br.Group != "core" && *br.Group != "") || (br.Kind != nil && *br.Kind != "Service") {
				continue
			}

			ns := namespace
			if br.Namespace != nil {
				ns = string(*br.Namespace)
			}
			port := uint32(0)
			if br.Port != nil {
				port = uint32(*br.Port)
			}
			weight := uint32(1)
			if br.Weight != nil {
				weight = uint32(*br.Weight)
			}
			ri.BackendRefs = append(ri.BackendRefs, mapper.BackendRefInput{
				ServiceName:      string(br.Name),
				ServiceNamespace: ns,
				Port:             port,
				Weight:           weight,
			})
		}

		for _, f := range rule.Filters {
			fi := mapper.FilterInput{Type: string(f.Type)}
			switch f.Type {
			case gwapiv1.HTTPRouteFilterRequestRedirect:
				if f.RequestRedirect != nil {
					if f.RequestRedirect.Scheme != nil {
						fi.RedirectScheme = *f.RequestRedirect.Scheme
					}
					if f.RequestRedirect.Hostname != nil {
						fi.RedirectHost = string(*f.RequestRedirect.Hostname)
					}
					if f.RequestRedirect.StatusCode != nil {
						fi.RedirectCode = uint32(*f.RequestRedirect.StatusCode)
					}
					if f.RequestRedirect.Port != nil {
						fi.RedirectPort = uint32(*f.RequestRedirect.Port)
					}
					if f.RequestRedirect.Path != nil {
						if f.RequestRedirect.Path.Type == gwapiv1.FullPathHTTPPathModifier && f.RequestRedirect.Path.ReplaceFullPath != nil {
							fi.RedirectPath = *f.RequestRedirect.Path.ReplaceFullPath
						}
						if f.RequestRedirect.Path.Type == gwapiv1.PrefixMatchHTTPPathModifier && f.RequestRedirect.Path.ReplacePrefixMatch != nil {
							fi.RedirectPathPrefix = *f.RequestRedirect.Path.ReplacePrefixMatch
						}
					}
				}
			case gwapiv1.HTTPRouteFilterURLRewrite:
				if f.URLRewrite != nil {
					if f.URLRewrite.Path != nil {
						if f.URLRewrite.Path.Type == gwapiv1.PrefixMatchHTTPPathModifier && f.URLRewrite.Path.ReplacePrefixMatch != nil {
							fi.RewritePathPrefix = *f.URLRewrite.Path.ReplacePrefixMatch
						}
						if f.URLRewrite.Path.Type == gwapiv1.FullPathHTTPPathModifier && f.URLRewrite.Path.ReplaceFullPath != nil {
							fi.RewriteFullPath = *f.URLRewrite.Path.ReplaceFullPath
						}
					}
					if f.URLRewrite.Hostname != nil {
						fi.RewriteHostname = string(*f.URLRewrite.Hostname)
					}
				}
			case gwapiv1.HTTPRouteFilterRequestHeaderModifier:
				if f.RequestHeaderModifier != nil {
					for _, h := range f.RequestHeaderModifier.Add {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: true,
						})
					}
					for _, h := range f.RequestHeaderModifier.Set {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: false,
						})
					}
					for _, name := range f.RequestHeaderModifier.Remove {
						fi.HeadersToRemove = append(fi.HeadersToRemove, name)
					}
				}
			case gwapiv1.HTTPRouteFilterResponseHeaderModifier:
				if f.ResponseHeaderModifier != nil {
					for _, h := range f.ResponseHeaderModifier.Add {
						fi.ResponseHeadersToAdd = append(fi.ResponseHeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: true,
						})
					}
					for _, h := range f.ResponseHeaderModifier.Set {
						fi.ResponseHeadersToAdd = append(fi.ResponseHeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value, Append: false,
						})
					}
					for _, name := range f.ResponseHeaderModifier.Remove {
						fi.ResponseHeadersToRemove = append(fi.ResponseHeadersToRemove, name)
					}
				}
			case gwapiv1.HTTPRouteFilterRequestMirror:
				if f.RequestMirror != nil {
					fi.MirrorServiceName = string(f.RequestMirror.BackendRef.Name)
					ns := namespace
					if f.RequestMirror.BackendRef.Namespace != nil {
						ns = string(*f.RequestMirror.BackendRef.Namespace)
					}
					fi.MirrorServiceNamespace = ns
					if f.RequestMirror.BackendRef.Port != nil {
						fi.MirrorPort = uint32(*f.RequestMirror.BackendRef.Port)
					}
					if f.RequestMirror.Percent != nil {
						fi.MirrorPercent = uint32(*f.RequestMirror.Percent)
					} else if f.RequestMirror.Fraction != nil && f.RequestMirror.Fraction.Denominator != nil && *f.RequestMirror.Fraction.Denominator > 0 {
						fi.MirrorPercent = uint32(float64(f.RequestMirror.Fraction.Numerator) / float64(*f.RequestMirror.Fraction.Denominator) * 100)
					}
				}
			case gwapiv1.HTTPRouteFilterExtensionRef:
				if f.ExtensionRef != nil {
					fi.ExtensionGroup = string(f.ExtensionRef.Group)
					fi.ExtensionKind = string(f.ExtensionRef.Kind)
					fi.ExtensionName = string(f.ExtensionRef.Name)
				}
			}
			ri.Filters = append(ri.Filters, fi)
		}

		if rule.Timeouts != nil && rule.Timeouts.Request != nil {
			ri.Timeouts = &mapper.RuleTimeouts{
				Request: string(*rule.Timeouts.Request),
			}
		}

		if rule.Retry != nil {
			ri.Retry = &mapper.RuleRetry{}
			if rule.Retry.Attempts != nil {
				ri.Retry.Attempts = uint32(*rule.Retry.Attempts)
			}
			if rule.Retry.Backoff != nil {
				ri.Retry.PerAttemptTimeout = string(*rule.Retry.Backoff) // Note: mapping backoff to perAttemptTimeout as an approximation, although they differ semantically.
			}
		}

		if rule.SessionPersistence != nil && rule.SessionPersistence.SessionName != nil {
			ri.SessionPersistence = &mapper.RuleSessionPersistence{
				SessionName: *rule.SessionPersistence.SessionName,
			}
			if rule.SessionPersistence.AbsoluteTimeout != nil {
				ri.SessionPersistence.AbsoluteTimeout = string(*rule.SessionPersistence.AbsoluteTimeout)
			}
		}

		input.Rules = append(input.Rules, ri)
	}

	return input
}

// buildLogger creates an slog.Logger based on the config.
func buildLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// buildK8sConfig creates a Kubernetes REST config from in-cluster or kubeconfig.
func buildK8sConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	kubeconfig := ""
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	if kubeconfig == "" {
		return nil, fmt.Errorf("no kubeconfig found")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// slogLogr converts an *slog.Logger into a logr.Logger so that
// controller-runtime internal components share the same structured logger.
func slogLogr(l *slog.Logger) logr.Logger {
	return logr.FromSlogHandler(l.Handler())
}

// buildVrataHTTPClient creates an *http.Client with TLS configured for
// connecting to the Vrata control plane. Cert, Key, and CA values can be
// inline PEM or file paths.
func buildVrataHTTPClient(tc *config.TLSConfig) (*http.Client, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if tc.CA != "" {
		caPEM, err := resolvePEM(tc.CA)
		if err != nil {
			return nil, fmt.Errorf("resolving CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parsing CA bundle: no valid certificates found")
		}
		tlsCfg.RootCAs = pool
	}

	if tc.Cert != "" && tc.Key != "" {
		certPEM, err := resolvePEM(tc.Cert)
		if err != nil {
			return nil, fmt.Errorf("resolving client cert: %w", err)
		}
		keyPEM, err := resolvePEM(tc.Key)
		if err != nil {
			return nil, fmt.Errorf("resolving client key: %w", err)
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("parsing client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}, nil
}

// resolvePEM returns PEM content from a value that is either inline PEM
// or a file path. Values starting with "-----BEGIN" are inline PEM.
func resolvePEM(value string) ([]byte, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "-----BEGIN") {
		return []byte(value), nil
	}
	data, err := os.ReadFile(value)
	if err != nil {
		return nil, fmt.Errorf("reading PEM from %q: %w", value, err)
	}
	return data, nil
}
