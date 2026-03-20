// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Controller synchronises Gateway API resources (HTTPRoute, Gateway) from
// Kubernetes to Vrata via its REST API. Changes are batched and published
// as versioned snapshots.
//
// Usage:
//
//	vrata-controller --config /path/to/config.yaml
package main

import (
	"context"
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
	vrataClient := vrata.NewClient(cfg.ControlPlaneURL)
	rec := reconciler.NewReconciler(vrataClient, logger)
	debounce, _ := time.ParseDuration(cfg.Snapshot.Debounce)
	if debounce == 0 {
		debounce = 5 * time.Second
	}
	bat := batcher.New(vrataClient, debounce, cfg.Snapshot.MaxBatch, logger)
	statusWriter := status.NewWriter(k8sClient)

	dupMode := cfg.DuplicatesMode()
	var detector *dedup.Detector
	if dupMode != config.DuplicateModeOff {
		detector = dedup.NewDetector(logger)
	}

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
				w.Write([]byte("ok"))
			} else {
				w.WriteHeader(503)
				w.Write([]byte("not ready"))
			}
		})
		srv := &http.Server{Addr: ":8081", Handler: mux}
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server failed", slog.String("error", err.Error()))
		}
	}()

	// Metrics server.
	var m *kcmetrics.Metrics
	if cfg.Metrics.Enabled {
		m = kcmetrics.New()
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", m.Handler())
			srv := &http.Server{Addr: cfg.Metrics.Address, Handler: mux}
			go func() { <-ctx.Done(); srv.Shutdown(context.Background()) }()
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
		slog.Bool("superHttpRoutes", cfg.WatchSuperHTTPRoutes()),
		slog.Bool("gateways", cfg.WatchGateways()),
	)

	// Build the reconcile loop as a function — called directly or via leader election.
	reconcileLoop := func(ctx context.Context) {
		syncCycle := func() {
			start := time.Now()

			// Reset dedup detector before each cycle to clear stale entries.
			if detector != nil {
				detector.Reset()
			}

			// Collect desired group names from all route sources for GC.
			desiredGroups := make(map[string]bool)

			if cfg.WatchGateways() {
				if err := syncAllGateways(ctx, informerCache, rec, bat, logger); err != nil {
					logger.Error("Gateway sync failed", slog.String("error", err.Error()))
					if m != nil {
						m.ReconcileErrors.WithLabelValues("gateway").Inc()
					}
				} else if m != nil {
					m.ReconcileTotal.WithLabelValues("gateway", "success").Inc()
				}
			}
			if cfg.WatchHTTPRoutes() {
				groups, err := syncAllHTTPRoutes(ctx, informerCache, rec, bat, statusWriter, detector, dupMode, refGrantChecker, m, logger)
				if err != nil {
					logger.Error("HTTPRoute sync failed", slog.String("error", err.Error()))
					if m != nil {
						m.ReconcileErrors.WithLabelValues("httproute").Inc()
					}
				} else if m != nil {
					m.ReconcileTotal.WithLabelValues("httproute", "success").Inc()
				}
				for _, g := range groups {
					desiredGroups[g] = true
				}
			}
			if cfg.WatchSuperHTTPRoutes() {
				groups, err := syncAllSuperHTTPRoutes(ctx, informerCache, rec, bat, detector, dupMode, refGrantChecker, m, logger)
				if err != nil {
					logger.Error("SuperHTTPRoute sync failed", slog.String("error", err.Error()))
					if m != nil {
						m.ReconcileErrors.WithLabelValues("superhttproute").Inc()
					}
				} else if m != nil {
					m.ReconcileTotal.WithLabelValues("superhttproute", "success").Inc()
				}
				for _, g := range groups {
					desiredGroups[g] = true
				}
			}

			// Garbage collect orphaned groups (and their routes, middlewares, destinations).
			gcOrphanedGroups(ctx, rec, bat, desiredGroups, logger)

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
		hostname, _ := os.Hostname()
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

// syncAllGateways lists all Gateways, reconciles their Listeners, and removes
// orphaned listeners that no longer correspond to any Gateway in Kubernetes.
func syncAllGateways(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, logger *slog.Logger) error {
	var gateways gwapiv1.GatewayList
	if err := c.List(ctx, &gateways); err != nil {
		return fmt.Errorf("listing Gateways: %w", err)
	}

	vrataClient := rec.Client()
	desiredNames := make(map[string]bool)

	for _, gw := range gateways.Items {
		input := gatewayToInput(&gw)
		listeners := mapper.MapGateway(input)

		for _, l := range listeners {
			desiredNames[l.Name] = true
			existing, err := findListenerByName(ctx, vrataClient, l.Name)
			if err != nil {
				logger.Error("checking listener", slog.String("name", l.Name), slog.String("error", err.Error()))
				continue
			}
			if existing == nil {
				if _, err := vrataClient.CreateListener(ctx, l); err != nil {
					logger.Error("creating listener", slog.String("name", l.Name), slog.String("error", err.Error()))
					continue
				}
				bat.Signal(ctx)
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

// syncAllHTTPRoutes lists all HTTPRoutes and reconciles each one.
// Returns the list of desired k8s: group names for garbage collection.
func syncAllHTTPRoutes(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, sw *status.Writer, detector *dedup.Detector, dupMode config.DuplicateMode, rgc *refgrant.Checker, m *kcmetrics.Metrics, logger *slog.Logger) ([]string, error) {
	var routes gwapiv1.HTTPRouteList
	if err := c.List(ctx, &routes); err != nil {
		return nil, fmt.Errorf("listing HTTPRoutes: %w", err)
	}

	var desiredGroups []string

	for i := range routes.Items {
		hr := &routes.Items[i]
		input := gatewayHTTPRouteToInput(hr)
		groupName := fmt.Sprintf("k8s:%s/%s", hr.Namespace, hr.Name)
		desiredGroups = append(desiredGroups, groupName)

		// Check cross-namespace backendRefs via ReferenceGrant.
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
				continue
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
					logger.Warn("rejecting HTTPRoute due to overlapping paths",
						slog.String("namespace", hr.Namespace),
						slog.String("name", hr.Name),
						slog.String("overlaps", msg),
					)
					if sw != nil {
						sw.SetAccepted(ctx, hr, false, "OverlappingRoute", msg)
					}
					continue
				}
			}
		}

		mapped := mapper.MapHTTPRoute(input)
		changes, err := rec.ApplyHTTPRoute(ctx, mapped)
		if err != nil {
			logger.Error("sync HTTPRoute failed",
				slog.String("namespace", hr.Namespace),
				slog.String("name", hr.Name),
				slog.String("error", err.Error()),
			)
			if sw != nil {
				sw.SetAccepted(ctx, hr, false, "SyncFailed", err.Error())
			}
			continue
		}
		if changes > 0 {
			for j := 0; j < changes; j++ {
				bat.Signal(ctx)
			}
			if sw != nil {
				sw.SetAccepted(ctx, hr, true, "Synced", "Successfully synced to Vrata")
			}
		}
	}

	return desiredGroups, nil
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

// syncAllSuperHTTPRoutes lists all SuperHTTPRoutes and reconciles each one.
// Uses the same mapper as HTTPRoute since the spec is identical.
// Garbage collection is handled by gcOrphanedGroups which covers all k8s: groups.
func syncAllSuperHTTPRoutes(ctx context.Context, c cache.Cache, rec *reconciler.Reconciler, bat *batcher.Batcher, detector *dedup.Detector, dupMode config.DuplicateMode, rgc *refgrant.Checker, m *kcmetrics.Metrics, logger *slog.Logger) ([]string, error) {
	var routes vrataapiv1.SuperHTTPRouteList
	if err := c.List(ctx, &routes); err != nil {
		return nil, fmt.Errorf("listing SuperHTTPRoutes: %w", err)
	}

	var desiredGroups []string

	for i := range routes.Items {
		shr := &routes.Items[i]
		input := superHTTPRouteToInput(shr)
		groupName := fmt.Sprintf("k8s:%s/%s", shr.Namespace, shr.Name)
		desiredGroups = append(desiredGroups, groupName)

		if detector != nil {
			ols := detector.Check(input)
			if len(ols) > 0 && dupMode == config.DuplicateModeReject {
				msg := formatOverlapMessage(ols)
				logger.Warn("rejecting SuperHTTPRoute due to overlapping paths",
					slog.String("namespace", shr.Namespace),
					slog.String("name", shr.Name),
					slog.String("overlaps", msg),
				)
				continue
			}
		}

		mapped := mapper.MapHTTPRoute(input)
		changes, err := rec.ApplyHTTPRoute(ctx, mapped)
		if err != nil {
			logger.Error("sync SuperHTTPRoute failed",
				slog.String("namespace", shr.Namespace),
				slog.String("name", shr.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		if changes > 0 {
			for j := 0; j < changes; j++ {
				bat.Signal(ctx)
			}
		}
	}
	return desiredGroups, nil
}

// superHTTPRouteToInput converts a SuperHTTPRoute to the mapper's input type.
// Since the spec is identical to HTTPRoute, the conversion is the same.
func superHTTPRouteToInput(shr *vrataapiv1.SuperHTTPRoute) mapper.HTTPRouteInput {
	input := mapper.HTTPRouteInput{
		Name:      shr.Name,
		Namespace: shr.Namespace,
	}

	for _, h := range shr.Spec.Hostnames {
		input.Hostnames = append(input.Hostnames, string(h))
	}

	for _, rule := range shr.Spec.Rules {
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
				hi := mapper.HeaderMatchInput{Name: string(hm.Name), Value: hm.Value}
				if hm.Type != nil {
					hi.Type = string(*hm.Type)
				}
				mi.Headers = append(mi.Headers, hi)
			}
			ri.Matches = append(ri.Matches, mi)
		}
		for _, br := range rule.BackendRefs {
			ns := shr.Namespace
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
				ServiceName: string(br.Name), ServiceNamespace: ns, Port: port, Weight: weight,
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
				}
			case gwapiv1.HTTPRouteFilterURLRewrite:
				if f.URLRewrite != nil {
					if f.URLRewrite.Path != nil && f.URLRewrite.Path.ReplacePrefixMatch != nil {
						fi.RewritePathPrefix = *f.URLRewrite.Path.ReplacePrefixMatch
					}
					if f.URLRewrite.Hostname != nil {
						fi.RewriteHostname = string(*f.URLRewrite.Hostname)
					}
				}
			case gwapiv1.HTTPRouteFilterRequestHeaderModifier:
				if f.RequestHeaderModifier != nil {
					for _, h := range f.RequestHeaderModifier.Add {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{Name: string(h.Name), Value: h.Value})
					}
					for _, h := range f.RequestHeaderModifier.Set {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{Name: string(h.Name), Value: h.Value})
					}
					for _, name := range f.RequestHeaderModifier.Remove {
						fi.HeadersToRemove = append(fi.HeadersToRemove, name)
					}
				}
			}
			ri.Filters = append(ri.Filters, fi)
		}
		input.Rules = append(input.Rules, ri)
	}

	return input
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
// from Vrata that no longer correspond to any HTTPRoute or SuperHTTPRoute in Kubernetes.
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
		changes, err := rec.DeleteHTTPRoute(ctx, ns, name)
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
		input.Listeners = append(input.Listeners, gl)
	}
	return input
}

// gatewayHTTPRouteToInput converts a Gateway API HTTPRoute to the mapper's input type.
func gatewayHTTPRouteToInput(hr *gwapiv1.HTTPRoute) mapper.HTTPRouteInput {
	input := mapper.HTTPRouteInput{
		Name:      hr.Name,
		Namespace: hr.Namespace,
	}

	for _, h := range hr.Spec.Hostnames {
		input.Hostnames = append(input.Hostnames, string(h))
	}

	for _, rule := range hr.Spec.Rules {
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
			ri.Matches = append(ri.Matches, mi)
		}

		for _, br := range rule.BackendRefs {
			ns := hr.Namespace
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
					if f.RequestRedirect.Path != nil && f.RequestRedirect.Path.ReplaceFullPath != nil {
						fi.RedirectPath = *f.RequestRedirect.Path.ReplaceFullPath
					}
				}
			case gwapiv1.HTTPRouteFilterURLRewrite:
				if f.URLRewrite != nil {
					if f.URLRewrite.Path != nil && f.URLRewrite.Path.ReplacePrefixMatch != nil {
						fi.RewritePathPrefix = *f.URLRewrite.Path.ReplacePrefixMatch
					}
					if f.URLRewrite.Hostname != nil {
						fi.RewriteHostname = string(*f.URLRewrite.Hostname)
					}
				}
			case gwapiv1.HTTPRouteFilterRequestHeaderModifier:
				if f.RequestHeaderModifier != nil {
					for _, h := range f.RequestHeaderModifier.Add {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value,
						})
					}
					for _, h := range f.RequestHeaderModifier.Set {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{
							Name: string(h.Name), Value: h.Value,
						})
					}
					for _, name := range f.RequestHeaderModifier.Remove {
						fi.HeadersToRemove = append(fi.HeadersToRemove, name)
					}
				}
			}
			ri.Filters = append(ri.Filters, fi)
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
