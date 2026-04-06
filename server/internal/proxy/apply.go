// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"fmt"

	"github.com/achetronic/vrata/internal/model"
)

// ApplyParams holds the shared dependencies for applying a config snapshot.
type ApplyParams struct {
	Router          *Router
	ListenerManager *ListenerManager
	HealthChecker   *HealthChecker
	OutlierDetector *OutlierDetector
	SessionStore    SessionStore
	CELBodyMaxSize  int
}

// ApplySnapshot builds a routing table from the given resources, swaps it
// into the running proxy atomically, and reconciles all supporting
// infrastructure (health checks, outlier detection, listeners, metrics).
func ApplySnapshot(
	p ApplyParams,
	listeners []model.Listener,
	routes []model.Route,
	groups []model.RouteGroup,
	destinations []model.Destination,
	middlewares []model.Middleware,
) error {
	table, err := BuildTable(routes, groups, destinations, middlewares, p.SessionStore, p.CELBodyMaxSize)
	if err != nil {
		return fmt.Errorf("building routing table: %w", err)
	}

	p.Router.SwapTable(table)

	if p.HealthChecker != nil {
		p.HealthChecker.Update(table.Pools())
	}
	if p.OutlierDetector != nil {
		p.OutlierDetector.Update(table.Pools())
		od := p.OutlierDetector
		for _, pool := range table.Pools() {
			for _, ep := range pool.Endpoints {
				ep.OnResponse = od.RecordResponse
			}
		}
	}

	p.ListenerManager.Reconcile(listeners)

	mcs := p.ListenerManager.MetricsCollectors()
	for _, mc := range mcs {
		mc.UpdatePools(table.Pools())
	}
	p.Router.SetMetricsCollectors(mcs)

	return nil
}
