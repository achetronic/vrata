// Package gateway orchestrates the bridge between the Rutoso store and the Envoy
// xDS control plane. It subscribes to store events, rebuilds xDS snapshots on
// every change, and pushes them to all connected Envoy nodes via the snapshot cache.
package gateway

import (
	"context"
	"fmt"
	"log/slog"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"

	"github.com/achetronic/rutoso/internal/store"
	"github.com/achetronic/rutoso/internal/xds"
)

// EndpointProvider supplies current EDS ClusterLoadAssignments keyed by
// Destination ID. The gateway queries this on every rebuild.
type EndpointProvider interface {
	Endpoints() map[string]*endpointv3.ClusterLoadAssignment
}

// Dependencies holds the external collaborators required by the Gateway.
type Dependencies struct {
	Store             store.Store
	Cache             cachev3.SnapshotCache
	XDSServer         *xds.Server
	Logger            *slog.Logger
	EndpointProvider  EndpointProvider

	// NextVersion is called to obtain a monotonically increasing version string
	// for each new snapshot. Typically backed by xds.Server.NextVersion.
	NextVersion func() string
}

// Gateway listens for store change events and keeps the xDS snapshot cache up to date.
type Gateway struct {
	deps Dependencies
}

// New creates a new Gateway.
func New(deps Dependencies) *Gateway {
	return &Gateway{deps: deps}
}

// Rebuild is a public wrapper around rebuild, allowing external components
// (e.g. the k8s EndpointSlice watcher) to trigger a full xDS snapshot rebuild.
func (gw *Gateway) Rebuild(ctx context.Context) error {
	return gw.rebuild(ctx)
}

// Run starts the event loop. It blocks until ctx is cancelled, then returns nil.
// Any error encountered while rebuilding a snapshot is logged but does not stop the loop.
func (gw *Gateway) Run(ctx context.Context) error {
	events, err := gw.deps.Store.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("gateway: subscribing to store: %w", err)
	}

	gw.deps.Logger.Info("gateway started: watching store events")

	// Push an initial snapshot so Envoys that connect before any API call
	// get an empty but valid configuration.
	if err := gw.rebuild(ctx); err != nil {
		gw.deps.Logger.Warn("gateway: initial snapshot failed", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			gw.deps.Logger.Info("gateway stopped")
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			gw.deps.Logger.Debug("gateway: store event received",
				slog.String("type", string(ev.Type)),
				slog.String("resource", string(ev.Resource)),
				slog.String("id", ev.ID),
			)
			if err := gw.rebuild(ctx); err != nil {
				gw.deps.Logger.Error("gateway: snapshot rebuild failed",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// rebuild fetches all resources from the store, builds a fresh xDS snapshot,
// and pushes it to every Envoy node ID currently tracked in the cache.
func (gw *Gateway) rebuild(ctx context.Context) error {
	listeners, err := gw.deps.Store.ListListeners(ctx)
	if err != nil {
		return fmt.Errorf("listing listeners: %w", err)
	}

	filters, err := gw.deps.Store.ListFilters(ctx)
	if err != nil {
		return fmt.Errorf("listing filters: %w", err)
	}

	groups, err := gw.deps.Store.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("listing groups: %w", err)
	}

	routes, err := gw.deps.Store.ListRoutes(ctx)
	if err != nil {
		return fmt.Errorf("listing routes: %w", err)
	}

	destinations, err := gw.deps.Store.ListDestinations(ctx)
	if err != nil {
		return fmt.Errorf("listing destinations: %w", err)
	}

	version := gw.deps.NextVersion()

	var edsCLAs map[string]*endpointv3.ClusterLoadAssignment
	if gw.deps.EndpointProvider != nil {
		edsCLAs = gw.deps.EndpointProvider.Endpoints()
	}

	snap, err := xds.BuildSnapshot(version, listeners, filters, groups, routes, destinations, edsCLAs)
	if err != nil {
		return fmt.Errorf("building snapshot: %w", err)
	}

	// Store the snapshot for debug retrieval regardless of connected nodes.
	gw.deps.XDSServer.SetLastSnapshot(snap)

	// Push to all known node IDs. A node is "known" once it sends its first
	// discovery request; until then there is nothing to update.
	for _, nodeID := range gw.deps.Cache.GetStatusKeys() {
		if err := gw.deps.Cache.SetSnapshot(ctx, nodeID, snap); err != nil {
			gw.deps.Logger.Error("gateway: set snapshot failed",
				slog.String("nodeId", nodeID),
				slog.String("error", err.Error()),
			)
		}
	}

	gw.deps.Logger.Info("gateway: snapshot pushed",
		slog.String("version", version),
		slog.Int("listeners", len(listeners)),
		slog.Int("filters", len(filters)),
		slog.Int("groups", len(groups)),
		slog.Int("routes", len(routes)),
		slog.Int("destinations", len(destinations)),
		slog.Int("nodes", len(gw.deps.Cache.GetStatusKeys())),
	)
	return nil
}
