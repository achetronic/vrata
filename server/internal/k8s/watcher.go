// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package k8s watches Kubernetes EndpointSlices and Services for Destinations
// whose discovery type is "kubernetes" and maintains an up-to-date map of
// resolved endpoints keyed by Destination ID.
//
// For regular Services (ClusterIP, NodePort, LoadBalancer), the watcher
// observes EndpointSlices and resolves individual pod IPs. For ExternalName
// Services, the watcher reads spec.externalName and uses it as the sole
// endpoint, re-checking whenever the Service object changes.
//
// The Watcher is a pure endpoint provider: it does not touch the proxy
// config directly. It calls OnChange() whenever the endpoint set changes
// so the gateway can trigger a rebuild.
package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kcache "k8s.io/client-go/tools/cache"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/store"
)

// Dependencies holds the collaborators required by the Watcher.
type Dependencies struct {
	Store  store.Store
	Client kubernetes.Interface
	Logger *slog.Logger
}

// Watcher observes Kubernetes EndpointSlices and Services for EDS-backed
// Destinations.
type Watcher struct {
	deps      Dependencies
	mu        sync.RWMutex
	endpoints map[string][]model.Endpoint
	cancels   map[string]context.CancelFunc
	onChangeMu sync.RWMutex
	onChange   func(ctx context.Context) error
}

// New creates a new Watcher.
func New(deps Dependencies) *Watcher {
	return &Watcher{
		deps:      deps,
		endpoints: make(map[string][]model.Endpoint),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// SetOnChange sets the callback invoked when endpoints change.
// Safe for concurrent use with notifyChange.
func (w *Watcher) SetOnChange(fn func(ctx context.Context) error) {
	w.onChangeMu.Lock()
	w.onChange = fn
	w.onChangeMu.Unlock()
}

// Endpoints returns a snapshot of resolved endpoints keyed by Destination ID.
func (w *Watcher) Endpoints() map[string][]model.Endpoint {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make(map[string][]model.Endpoint, len(w.endpoints))
	for k, v := range w.endpoints {
		cp := make([]model.Endpoint, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// Run subscribes to the store and reconciles watches on every Destination change.
func (w *Watcher) Run(ctx context.Context) error {
	events, err := w.deps.Store.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("k8s watcher: subscribing to store: %w", err)
	}

	w.deps.Logger.Info("k8s watcher started")

	if err := w.reconcile(ctx); err != nil {
		w.deps.Logger.Warn("k8s watcher: initial reconcile failed",
			slog.String("error", err.Error()),
		)
	}

	for {
		select {
		case <-ctx.Done():
			w.stopAll()
			w.deps.Logger.Info("k8s watcher stopped")
			return nil
		case ev, ok := <-events:
			if !ok {
				w.stopAll()
				return nil
			}
			if ev.Resource != store.ResourceDestination {
				continue
			}
			if err := w.reconcile(ctx); err != nil {
				w.deps.Logger.Error("k8s watcher: reconcile failed",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// reconcile diffs the current watches against the desired state from the store
// and starts/stops watches as needed.
func (w *Watcher) reconcile(ctx context.Context) error {
	destinations, err := w.deps.Store.ListDestinations(ctx)
	if err != nil {
		return fmt.Errorf("listing destinations: %w", err)
	}

	desired := make(map[string]model.Destination)
	for _, d := range destinations {
		if isEDS(d) {
			desired[d.ID] = d
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Stop watches for removed destinations.
	for id, cancel := range w.cancels {
		if _, ok := desired[id]; !ok {
			cancel()
			delete(w.cancels, id)
			delete(w.endpoints, id)
		}
	}

	// Start watches for new destinations.
	for id, d := range desired {
		if _, ok := w.cancels[id]; ok {
			continue
		}
		ns, svc, err := parseFQDN(d.Host)
		if err != nil {
			w.deps.Logger.Warn("k8s watcher: cannot parse FQDN",
				slog.String("destID", id),
				slog.String("host", d.Host),
				slog.String("error", err.Error()),
			)
			continue
		}

		watchCtx, cancel := context.WithCancel(ctx)
		w.cancels[id] = cancel

		// Check the Service type to decide how to resolve endpoints.
		svcObj, err := w.deps.Client.CoreV1().Services(ns).Get(ctx, svc, metav1.GetOptions{})
		if err != nil {
			w.deps.Logger.Warn("k8s watcher: cannot get Service",
				slog.String("destID", id),
				slog.String("namespace", ns),
				slog.String("service", svc),
				slog.String("error", err.Error()),
			)
			cancel()
			delete(w.cancels, id)
			continue
		}

		if svcObj.Spec.Type == corev1.ServiceTypeExternalName {
			go w.watchExternalNameService(watchCtx, id, d.Port, ns, svc)
		} else {
			go w.watchEndpointSlices(watchCtx, id, d.Port, ns, svc)
		}
	}

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// EndpointSlice watcher (ClusterIP / NodePort / LoadBalancer)
// ─────────────────────────────────────────────────────────────────────────────

// watchEndpointSlices observes EndpointSlices for a regular Kubernetes Service
// and updates the endpoint map on every change.
func (w *Watcher) watchEndpointSlices(ctx context.Context, destID string, destPort uint32, namespace, service string) {
	w.deps.Logger.Info("k8s watcher: watching EndpointSlices",
		slog.String("destID", destID),
		slog.String("namespace", namespace),
		slog.String("service", service),
	)

	factory := informers.NewSharedInformerFactoryWithOptions(
		w.deps.Client,
		0,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = labels.Set{
				discoveryv1.LabelServiceName: service,
			}.String()
		}),
	)

	informer := factory.Discovery().V1().EndpointSlices().Informer()

	onEvent := func(_ interface{}) {
		objs := informer.GetStore().List()
		eps := buildEndpoints(destPort, objs)

		w.mu.Lock()
		w.endpoints[destID] = eps
		w.mu.Unlock()

		w.deps.Logger.Info("k8s watcher: endpoints updated",
			slog.String("destID", destID),
			slog.Int("count", len(eps)),
		)

		w.notifyChange(ctx, destID)
	}

	informer.AddEventHandler(kcache.ResourceEventHandlerFuncs{
		AddFunc:    onEvent,
		UpdateFunc: func(_, newObj interface{}) { onEvent(newObj) },
		DeleteFunc: onEvent,
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	<-ctx.Done()
}

// buildEndpoints extracts ready pod IPs from a list of EndpointSlice objects.
func buildEndpoints(destPort uint32, objs []interface{}) []model.Endpoint {
	var eps []model.Endpoint
	for _, obj := range objs {
		slice, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			continue
		}
		port := destPort
		for _, p := range slice.Ports {
			if p.Port != nil {
				port = uint32(*p.Port)
				break
			}
		}
		for _, ep := range slice.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}
			for _, addr := range ep.Addresses {
				eps = append(eps, model.Endpoint{Host: addr, Port: port})
			}
		}
	}
	return eps
}

// ─────────────────────────────────────────────────────────────────────────────
// ExternalName watcher
// ─────────────────────────────────────────────────────────────────────────────

// watchExternalNameService observes a Kubernetes Service of type ExternalName
// and uses spec.externalName as the sole endpoint. The watcher reacts to
// Service updates so that changes to the externalName are picked up.
func (w *Watcher) watchExternalNameService(ctx context.Context, destID string, destPort uint32, namespace, service string) {
	w.deps.Logger.Info("k8s watcher: watching ExternalName Service",
		slog.String("destID", destID),
		slog.String("namespace", namespace),
		slog.String("service", service),
	)

	factory := informers.NewSharedInformerFactoryWithOptions(
		w.deps.Client,
		0,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = "metadata.name=" + service
		}),
	)

	informer := factory.Core().V1().Services().Informer()

	onEvent := func(obj interface{}) {
		svcObj, ok := obj.(*corev1.Service)
		if !ok {
			return
		}
		if svcObj.Spec.Type != corev1.ServiceTypeExternalName || svcObj.Spec.ExternalName == "" {
			w.mu.Lock()
			w.endpoints[destID] = nil
			w.mu.Unlock()

			w.deps.Logger.Warn("k8s watcher: ExternalName Service has no externalName or changed type",
				slog.String("destID", destID),
				slog.String("service", service),
			)
			return
		}

		eps := []model.Endpoint{{Host: svcObj.Spec.ExternalName, Port: destPort}}

		w.mu.Lock()
		w.endpoints[destID] = eps
		w.mu.Unlock()

		w.deps.Logger.Info("k8s watcher: ExternalName resolved",
			slog.String("destID", destID),
			slog.String("externalName", svcObj.Spec.ExternalName),
			slog.Uint64("port", uint64(destPort)),
		)

		w.notifyChange(ctx, destID)
	}

	informer.AddEventHandler(kcache.ResourceEventHandlerFuncs{
		AddFunc:    onEvent,
		UpdateFunc: func(_, newObj interface{}) { onEvent(newObj) },
		DeleteFunc: func(_ interface{}) {
			w.mu.Lock()
			w.endpoints[destID] = nil
			w.mu.Unlock()

			w.deps.Logger.Warn("k8s watcher: ExternalName Service deleted",
				slog.String("destID", destID),
			)

			w.notifyChange(ctx, destID)
		},
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	<-ctx.Done()
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, cancel := range w.cancels {
		cancel()
		delete(w.cancels, id)
		delete(w.endpoints, id)
	}
}

func isEDS(d model.Destination) bool {
	return d.Options != nil &&
		d.Options.Discovery != nil &&
		d.Options.Discovery.Type == model.DiscoveryTypeKubernetes
}

// notifyChange safely calls OnChange if set.
func (w *Watcher) notifyChange(ctx context.Context, destID string) {
	w.onChangeMu.RLock()
	fn := w.onChange
	w.onChangeMu.RUnlock()
	if fn == nil {
		return
	}
	if err := fn(ctx); err != nil {
		w.deps.Logger.Error("k8s watcher: OnChange failed",
			slog.String("destID", destID),
			slog.String("error", err.Error()),
		)
	}
}

func parseFQDN(host string) (namespace, service string, err error) {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("expected at least <service>.<namespace> in %q", host)
	}
	return parts[1], parts[0], nil
}
