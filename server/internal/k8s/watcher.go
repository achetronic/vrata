// Package k8s watches Kubernetes EndpointSlices for Destinations whose
// discovery type is "kubernetes" and maintains an up-to-date map of
// ClusterLoadAssignments keyed by Destination ID.
//
// The Watcher is a pure endpoint provider: it does not touch the xDS cache,
// does not build snapshots, and does not know about Envoy. It exposes
// Endpoints() so the gateway can query the current state during rebuild,
// and calls deps.OnChange() whenever the endpoint set changes so the gateway
// knows to trigger a rebuild.
package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kcache "k8s.io/client-go/tools/cache"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/store"
)

// Dependencies holds the collaborators required by the Watcher.
type Dependencies struct {
	// Store is used to list Destinations and subscribe to changes.
	Store store.Store

	// Client is the Kubernetes API client used to create informers.
	Client kubernetes.Interface

	// Logger receives structured log output.
	Logger *slog.Logger

	// OnChange is called whenever the endpoint set for any destination changes.
	// Typically this triggers a gateway rebuild.
	OnChange func(ctx context.Context) error
}

// Watcher observes Kubernetes EndpointSlices for EDS-backed Destinations
// and maintains a current map of ClusterLoadAssignments.
type Watcher struct {
	deps Dependencies

	mu        sync.RWMutex
	endpoints map[string]*endpointv3.ClusterLoadAssignment // keyed by Destination ID
	cancels   map[string]context.CancelFunc                // keyed by Destination ID
}

// New creates a new Watcher. Call Run to start it.
func New(deps Dependencies) *Watcher {
	return &Watcher{
		deps:      deps,
		endpoints: make(map[string]*endpointv3.ClusterLoadAssignment),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// SetOnChange sets the callback invoked when endpoints change.
// Must be called before Run.
func (w *Watcher) SetOnChange(fn func(ctx context.Context) error) {
	w.deps.OnChange = fn
}

// Endpoints returns a snapshot of the current ClusterLoadAssignments,
// keyed by Destination ID. Safe for concurrent use.
func (w *Watcher) Endpoints() map[string]*endpointv3.ClusterLoadAssignment {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make(map[string]*endpointv3.ClusterLoadAssignment, len(w.endpoints))
	for k, v := range w.endpoints {
		out[k] = v
	}
	return out
}

// Run subscribes to the store and reconciles EndpointSlice watches until ctx
// is cancelled.
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
			w.deps.Logger.Debug("k8s watcher: destination event, reconciling",
				slog.String("type", string(ev.Type)),
				slog.String("id", ev.ID),
			)
			if err := w.reconcile(ctx); err != nil {
				w.deps.Logger.Error("k8s watcher: reconcile failed",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// reconcile diffs EDS Destinations vs active watches.
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

	for id, cancel := range w.cancels {
		if _, ok := desired[id]; !ok {
			w.deps.Logger.Info("k8s watcher: stopping watch", slog.String("destID", id))
			cancel()
			delete(w.cancels, id)
			delete(w.endpoints, id)
		}
	}

	for id, d := range desired {
		if _, ok := w.cancels[id]; ok {
			continue
		}
		ns, svc, err := parseFQDN(d.Host)
		if err != nil {
			w.deps.Logger.Warn("k8s watcher: cannot parse FQDN, skipping",
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

// watchEndpointSlices runs an informer for the given service's EndpointSlices.
// On every event it updates the internal CLA map and calls OnChange.
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
		cla := buildCLA(destID, destPort, objs)

		w.mu.Lock()
		w.endpoints[destID] = cla
		w.mu.Unlock()

		w.deps.Logger.Info("k8s watcher: endpoints updated",
			slog.String("destID", destID),
			slog.String("service", service),
			slog.Int("lb_endpoints", len(cla.Endpoints[0].LbEndpoints)),
		)

		if err := w.deps.OnChange(ctx); err != nil {
			w.deps.Logger.Error("k8s watcher: OnChange failed",
				slog.String("destID", destID),
				slog.String("error", err.Error()),
			)
		}
	}

	informer.AddEventHandler(kcache.ResourceEventHandlerFuncs{ //nolint:errcheck
		AddFunc:    onEvent,
		UpdateFunc: func(_, newObj interface{}) { onEvent(newObj) },
		DeleteFunc: onEvent,
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	<-ctx.Done()
	w.deps.Logger.Info("k8s watcher: EndpointSlice watch stopped",
		slog.String("destID", destID),
	)
}

// stopAll cancels all active watches and clears endpoint state.
func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, cancel := range w.cancels {
		cancel()
		delete(w.cancels, id)
		delete(w.endpoints, id)
	}
}

// buildCLA builds a ClusterLoadAssignment from EndpointSlice objects.
// Only Ready endpoints are included.
func buildCLA(destID string, destPort uint32, objs []interface{}) *endpointv3.ClusterLoadAssignment {
	var lbEndpoints []*endpointv3.LbEndpoint

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
				lbEndpoints = append(lbEndpoints, &endpointv3.LbEndpoint{
					HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
						Endpoint: &endpointv3.Endpoint{
							Address: &corev3.Address{
								Address: &corev3.Address_SocketAddress{
									SocketAddress: &corev3.SocketAddress{
										Address: addr,
										PortSpecifier: &corev3.SocketAddress_PortValue{
											PortValue: port,
										},
									},
								},
							},
						},
					},
				})
			}
		}
	}

	return &endpointv3.ClusterLoadAssignment{
		ClusterName: destID,
		Endpoints:   []*endpointv3.LocalityLbEndpoints{{LbEndpoints: lbEndpoints}},
	}
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
