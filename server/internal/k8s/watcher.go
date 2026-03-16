// Package k8s watches Kubernetes EndpointSlices for Destinations whose
// discovery type is "kubernetes" and pushes ClusterLoadAssignment updates
// to the xDS snapshot cache whenever the set of ready endpoints changes.
package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	"github.com/achetronic/rutoso/internal/xds"

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
	Store     store.Store
	Client    kubernetes.Interface
	Cache     cachev3.SnapshotCache
	XDSServer *xds.Server
	Logger    *slog.Logger
	Rebuild   func(ctx context.Context) error
}

// Watcher observes Kubernetes EndpointSlices for EDS-backed Destinations and
// pushes ClusterLoadAssignment updates to the xDS snapshot cache.
type Watcher struct {
	deps    Dependencies
	mu      sync.Mutex
	cancels map[string]context.CancelFunc // keyed by Destination ID
}

// New creates a new Watcher. Call Run to start it.
func New(deps Dependencies) *Watcher {
	return &Watcher{
		deps:    deps,
		cancels: make(map[string]context.CancelFunc),
	}
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

// watchEndpointSlices starts a k8s informer for EndpointSlices belonging to
// the given service. On every event it rebuilds the full xDS snapshot (so
// cluster/route/listener resources stay consistent) and then overwrites the
// EDS endpoint resource for this destination in all known node snapshots.
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

	push := func(_ interface{}) {
		// First do a full snapshot rebuild so cluster/route/listener are current.
		if err := w.deps.Rebuild(ctx); err != nil {
			w.deps.Logger.Error("k8s watcher: rebuild failed",
				slog.String("destID", destID),
				slog.String("error", err.Error()),
			)
		}

		objs := informer.GetStore().List()
		cla := buildCLA(destID, destPort, objs)

		w.deps.Logger.Info("k8s watcher: pushing EDS endpoints",
			slog.String("destID", destID),
			slog.Int("endpoints", len(cla.Endpoints)),
		)

		// Build an updated snapshot merging the CLA into lastSnapshot.
		// This is used both for connected nodes and for nodes that connect later.
		var snapWithEDS *cachev3.Snapshot
		if last := w.deps.XDSServer.Snapshot(); last != nil {
			existing, ok := last.(*cachev3.Snapshot)
			if ok {
				resources := existing.GetResources(resourcev3.EndpointType)
				updated := make(map[string]types.Resource, len(resources)+1)
				for k, v := range resources {
					updated[k] = v
				}
				updated[destID] = cla
				endpointList := resourcesToSlice(updated)
				newSnap, err := cachev3.NewSnapshot(
					existing.GetVersion(resourcev3.EndpointType)+"e",
					map[resourcev3.Type][]types.Resource{
						resourcev3.ClusterType:  resourcesToSlice(existing.GetResources(resourcev3.ClusterType)),
						resourcev3.EndpointType: endpointList,
						resourcev3.RouteType:    resourcesToSlice(existing.GetResources(resourcev3.RouteType)),
						resourcev3.ListenerType: resourcesToSlice(existing.GetResources(resourcev3.ListenerType)),
					},
				)
				if err == nil {
					snapWithEDS = newSnap
				}
			}
		}

		if snapWithEDS == nil {
			return
		}

		// Always update lastSnapshot so new nodes get endpoints on connect.
		w.deps.XDSServer.SetLastSnapshot(snapWithEDS)

		// Push to already-connected nodes.
		for _, nodeID := range w.deps.Cache.GetStatusKeys() {
			if err := w.deps.Cache.SetSnapshot(ctx, nodeID, snapWithEDS); err != nil {
				w.deps.Logger.Error("k8s watcher: set snapshot failed",
					slog.String("nodeId", nodeID),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	informer.AddEventHandler(kcache.ResourceEventHandlerFuncs{ //nolint:errcheck
		AddFunc:    push,
		UpdateFunc: func(_, newObj interface{}) { push(newObj) },
		DeleteFunc: push,
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	<-ctx.Done()
	w.deps.Logger.Info("k8s watcher: EndpointSlice watch stopped",
		slog.String("destID", destID),
	)
}

// buildCLA builds a ClusterLoadAssignment from the EndpointSlice objects
// currently held by the informer store. Only Ready endpoints are included.
func buildCLA(destID string, destPort uint32, objs []interface{}) *endpointv3.ClusterLoadAssignment {
	var lbEndpoints []*endpointv3.LbEndpoint

	for _, obj := range objs {
		slice, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			continue
		}

		// Determine which port to use from the slice's port list.
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
		Endpoints: []*endpointv3.LocalityLbEndpoints{
			{LbEndpoints: lbEndpoints},
		},
	}
}

// stopAll cancels all active watches.
func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, cancel := range w.cancels {
		cancel()
		delete(w.cancels, id)
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

// resourcesToSlice converts a map[string]types.Resource to a []types.Resource.
func resourcesToSlice(m map[string]types.Resource) []types.Resource {
	s := make([]types.Resource, 0, len(m))
	for _, v := range m {
		s = append(s, v)
	}
	return s
}
