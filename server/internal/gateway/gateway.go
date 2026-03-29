// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package gateway orchestrates the bridge between the Vrata store and the
// Envoy xDS control plane. It subscribes to store events, rebuilds the xDS
// snapshot on every change, and pushes it to connected Envoy instances.
package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/store"
	"github.com/achetronic/vrata/internal/xds"
)

// EndpointProvider supplies dynamically resolved endpoints keyed by Destination ID.
// The k8s watcher implements this interface.
type EndpointProvider interface {
	Endpoints() map[string][]model.Endpoint
}

// Dependencies holds the external collaborators required by the Gateway.
type Dependencies struct {
	Store            store.Store
	XDS              *xds.Server
	EndpointProvider EndpointProvider
	Logger           *slog.Logger
}

// Gateway listens for store change events and keeps the Envoy xDS snapshot
// up to date.
type Gateway struct {
	deps Dependencies
}

// New creates a new Gateway.
func New(deps Dependencies) *Gateway {
	return &Gateway{deps: deps}
}

// Rebuild is a public wrapper around rebuild, allowing external components
// (e.g. the k8s watcher) to trigger a full xDS snapshot rebuild.
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

	// Push an initial snapshot so Envoy nodes that connect before any change
	// still get a valid config.
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

// rebuild fetches all resources from the store and pushes a new xDS snapshot
// to all connected Envoy nodes.
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

	// Merge dynamically discovered endpoints into destinations.
	if gw.deps.EndpointProvider != nil {
		discovered := gw.deps.EndpointProvider.Endpoints()
		for i, d := range destinations {
			if eps, ok := discovered[d.ID]; ok && len(eps) > 0 {
				destinations[i].Endpoints = eps
			}
		}
	}

	if err := gw.deps.XDS.PushSnapshot(ctx, listeners, groups, routes, destinations, middlewares); err != nil {
		return fmt.Errorf("pushing xds snapshot: %w", err)
	}

	gw.deps.Logger.Info("gateway: xds snapshot pushed",
		slog.Int("listeners", len(listeners)),
		slog.Int("routes", len(routes)),
		slog.Int("groups", len(groups)),
		slog.Int("destinations", len(destinations)),
		slog.Int("middlewares", len(middlewares)),
	)

	return nil
}
