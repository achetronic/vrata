// Package gateway orchestrates the bridge between the Rutoso store and the
// native proxy. It subscribes to store events, rebuilds the routing table on
// every change, and applies it to the proxy atomically.
package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/achetronic/rutoso/internal/proxy"
	"github.com/achetronic/rutoso/internal/store"
)

// Dependencies holds the external collaborators required by the Gateway.
type Dependencies struct {
	Store           store.Store
	Router          *proxy.Router
	ListenerManager *proxy.ListenerManager
	HealthChecker   *proxy.HealthChecker
	OutlierDetector *proxy.OutlierDetector
	Logger          *slog.Logger
}

// Gateway listens for store change events and keeps the proxy config up to date.
type Gateway struct {
	deps Dependencies
}

// New creates a new Gateway.
func New(deps Dependencies) *Gateway {
	return &Gateway{deps: deps}
}

// Rebuild is a public wrapper around rebuild, allowing external components
// to trigger a full proxy config rebuild.
func (gw *Gateway) Rebuild(ctx context.Context) error {
	return gw.rebuild(ctx)
}

// Run starts the event loop. It blocks until ctx is cancelled, then returns nil.
func (gw *Gateway) Run(ctx context.Context) error {
	events, err := gw.deps.Store.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("gateway: subscribing to store: %w", err)
	}

	gw.deps.Logger.Info("gateway started: watching store events")

	// Push an initial config.
	if err := gw.rebuild(ctx); err != nil {
		gw.deps.Logger.Warn("gateway: initial rebuild failed",
			slog.String("error", err.Error()),
		)
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
				gw.deps.Logger.Error("gateway: rebuild failed",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// rebuild fetches all resources from the store and rebuilds the proxy config.
func (gw *Gateway) rebuild(ctx context.Context) error {
	listeners, err := gw.deps.Store.ListListeners(ctx)
	if err != nil {
		return fmt.Errorf("listing listeners: %w", err)
	}

	middlewares, err := gw.deps.Store.ListMiddlewares(ctx)
	if err != nil {
		return fmt.Errorf("listing middlewares: %w", err)
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

	// Build new routing table.
	table, err := proxy.BuildTable(routes, groups, destinations, middlewares)
	if err != nil {
		return fmt.Errorf("building routing table: %w", err)
	}

	// Atomic swap.
	gw.deps.Router.SwapTable(table)

	// Update health checker with new upstreams.
	if gw.deps.HealthChecker != nil {
		gw.deps.HealthChecker.Update(table.Upstreams())
	}
	if gw.deps.OutlierDetector != nil {
		gw.deps.OutlierDetector.Update(table.Upstreams())
		od := gw.deps.OutlierDetector
		for _, u := range table.Upstreams() {
			u.OnResponse = od.RecordResponse
		}
	}

	// Reconcile listeners.
	gw.deps.ListenerManager.Reconcile(listeners)

	gw.deps.Logger.Info("gateway: config applied",
		slog.Int("listeners", len(listeners)),
		slog.Int("routes", len(routes)),
		slog.Int("groups", len(groups)),
		slog.Int("destinations", len(destinations)),
		slog.Int("middlewares", len(middlewares)),
	)

	return nil
}
