// Package k8s watches Kubernetes EndpointSlices for Destinations whose
// discovery type is "kubernetes" and maintains an up-to-date map of
// resolved endpoints (pod IPs) keyed by Destination ID.
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

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kcache "k8s.io/client-go/tools/cache"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/store"
)

// Endpoint represents a single resolved pod IP + port.
type Endpoint struct {
	Address string
	Port    uint32
}

// Dependencies holds the collaborators required by the Watcher.
type Dependencies struct {
	Store    store.Store
	Client   kubernetes.Interface
	Logger   *slog.Logger
	OnChange func(ctx context.Context) error
}

// Watcher observes Kubernetes EndpointSlices for EDS-backed Destinations.
type Watcher struct {
	deps      Dependencies
	mu        sync.RWMutex
	endpoints map[string][]Endpoint // keyed by Destination ID
	cancels   map[string]context.CancelFunc
}

// New creates a new Watcher.
func New(deps Dependencies) *Watcher {
	return &Watcher{
		deps:      deps,
		endpoints: make(map[string][]Endpoint),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// SetOnChange sets the callback invoked when endpoints change.
func (w *Watcher) SetOnChange(fn func(ctx context.Context) error) {
	w.deps.OnChange = fn
}

// Endpoints returns a snapshot of resolved endpoints keyed by Destination ID.
func (w *Watcher) Endpoints() map[string][]Endpoint {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make(map[string][]Endpoint, len(w.endpoints))
	for k, v := range w.endpoints {
		cp := make([]Endpoint, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// Run subscribes to the store and reconciles EndpointSlice watches.
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
		go w.watchEndpointSlices(watchCtx, id, d.Port, ns, svc)
	}

	return nil
}

func (w *Watcher) watchEndpointSlices(ctx context.Context, destID string, destPort uint32, namespace, service string) {
	w.deps.Logger.Info("k8s watcher: starting EndpointSlice watch",
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

		if err := w.deps.OnChange(ctx); err != nil {
			w.deps.Logger.Error("k8s watcher: OnChange failed",
				slog.String("destID", destID),
				slog.String("error", err.Error()),
			)
		}
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

func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, cancel := range w.cancels {
		cancel()
		delete(w.cancels, id)
		delete(w.endpoints, id)
	}
}

func buildEndpoints(destPort uint32, objs []interface{}) []Endpoint {
	var eps []Endpoint
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
				eps = append(eps, Endpoint{Address: addr, Port: port})
			}
		}
	}
	return eps
}

func isEDS(d model.Destination) bool {
	return d.Options != nil &&
		d.Options.Discovery != nil &&
		d.Options.Discovery.Type == model.DiscoveryTypeKubernetes
}

func parseFQDN(host string) (namespace, service string, err error) {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("expected at least <service>.<namespace> in %q", host)
	}
	return parts[1], parts[0], nil
}
