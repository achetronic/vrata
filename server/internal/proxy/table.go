// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

// BuildTable compiles a routing table from model entities.
func BuildTable(
	routes []model.Route,
	groups []model.RouteGroup,
	destinations []model.Destination,
	middlewares []model.Middleware,
	sessStore SessionStore,
	celBodyMaxSize int,
) (*RoutingTable, error) {
	// Build lookup maps.
	routeByID := make(map[string]model.Route, len(routes))
	for _, r := range routes {
		routeByID[r.ID] = r
	}

	destByID := make(map[string]model.Destination, len(destinations))
	for _, d := range destinations {
		destByID[d.ID] = d
	}

	mwByID := make(map[string]model.Middleware, len(middlewares))
	for _, m := range middlewares {
		mwByID[m.ID] = m
	}

	// Build destination pools (one pool per destination, N endpoints each).
	pools := make(map[string]*DestinationPool, len(destinations))
	for _, d := range destinations {
		pool, err := NewDestinationPool(d, sessStore)
		if err != nil {
			return nil, err
		}
		pools[d.ID] = pool
	}

	var compiled []compiledRoute

	table := &RoutingTable{
		pools:        pools,
		middlewares:  mwByID,
		sessionStore: sessStore,
	}

	// Track which routes are in groups.
	routesInGroups := make(map[string]bool)

	// Routes from groups.
	for _, g := range groups {
		for _, routeID := range g.RouteIDs {
			r, ok := routeByID[routeID]
			if !ok {
				continue
			}
			routesInGroups[routeID] = true
			gCopy := g
			cr, err := compileRoute(r, &gCopy, pools, mwByID, table.AddCleanup, sessStore, celBodyMaxSize)
			if err != nil {
				slog.Error("proxy: skipping route with compile error",
					slog.String("route", r.Name),
					slog.String("id", r.ID),
					slog.String("error", err.Error()),
				)
				continue
			}
			compiled = append(compiled, cr)
		}
	}

	// Standalone routes.
	for _, r := range routes {
		if routesInGroups[r.ID] {
			continue
		}
		cr, err := compileRoute(r, nil, pools, mwByID, table.AddCleanup, sessStore, celBodyMaxSize)
		if err != nil {
			slog.Error("proxy: skipping route with compile error",
				slog.String("route", r.Name),
				slog.String("id", r.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		compiled = append(compiled, cr)
	}

	// Build balancer rings/tables for destination pools with consistent hashing.
	for _, pool := range pools {
		if pool.Balancer == nil {
			continue
		}
		if builder, ok := pool.Balancer.(interface{ Build([]model.DestinationRef) }); ok {
			refs := pool.endpointRefs(pool.Endpoints)
			builder.Build(refs)
		}
	}

	table.routes = compiled
	return table, nil
}

// Pools returns the destination pool map from the routing table.
func (t *RoutingTable) Pools() map[string]*DestinationPool {
	return t.pools
}

// compileRoute pre-compiles a route for fast matching.
func compileRoute(
	r model.Route,
	g *model.RouteGroup,
	pools map[string]*DestinationPool,
	allMw map[string]model.Middleware,
	onCleanup func(func()),
	sessStore SessionStore,
	celBodyMaxSize int,
) (compiledRoute, error) {
	cr := compiledRoute{
		model:          r,
		group:          g,
		grpcOnly:       r.Match.GRPC,
		celBodyMaxSize: celBodyMaxSize,
	}

	// Compose path from group + route.
	groupPrefix := ""
	groupRegex := ""
	if g != nil {
		groupPrefix = g.PathPrefix
		groupRegex = g.PathRegex
	}

	switch {
	case groupRegex != "":
		// Group is regex — compose.
		var pattern string
		switch {
		case r.Match.PathRegex != "":
			pattern = "(?:" + groupRegex + ")(?:" + r.Match.PathRegex + ")"
		case r.Match.Path != "":
			pattern = "(?:" + groupRegex + ")(?:" + regexp.QuoteMeta(r.Match.Path) + ")"
		case r.Match.PathPrefix != "":
			pattern = "(?:" + groupRegex + ")(?:" + regexp.QuoteMeta(r.Match.PathPrefix) + ")"
		default:
			pattern = groupRegex
		}
		re, err := regexp.Compile("^" + pattern)
		if err != nil {
			return cr, err
		}
		cr.pathRegex = re

	default:
		prefix := groupPrefix
		switch {
		case r.Match.Path != "":
			cr.pathExact = prefix + r.Match.Path
		case r.Match.PathPrefix != "":
			cr.pathPrefix = prefix + r.Match.PathPrefix
		case r.Match.PathRegex != "":
			re, err := regexp.Compile("^" + prefix + r.Match.PathRegex)
			if err != nil {
				return cr, err
			}
			cr.pathRegex = re
		default:
			if prefix == "" {
				prefix = "/"
			}
			cr.pathPrefix = prefix
		}
	}

	// Methods.
	if len(r.Match.Methods) > 0 {
		cr.methods = make(map[string]bool, len(r.Match.Methods))
		for _, m := range r.Match.Methods {
			cr.methods[strings.ToUpper(m)] = true
		}
	}

	// Headers from route + group — pre-compile regexes.
	var rawHeaders []model.HeaderMatcher
	rawHeaders = append(rawHeaders, r.Match.Headers...)
	if g != nil {
		rawHeaders = append(rawHeaders, g.Headers...)
	}
	for _, hm := range rawHeaders {
		chm := compiledHeaderMatcher{name: hm.Name, value: hm.Value}
		if hm.Regex && hm.Value != "" {
			re, err := regexp.Compile(hm.Value)
			if err != nil {
				return cr, fmt.Errorf("compiling header regex %q: %w", hm.Value, err)
			}
			chm.regex = re
		}
		cr.headers = append(cr.headers, chm)
	}

	// Query params — pre-compile regexes.
	for _, qp := range r.Match.QueryParams {
		cqp := compiledQueryParamMatcher{name: qp.Name, value: qp.Value}
		if qp.Regex && qp.Value != "" {
			re, err := regexp.Compile(qp.Value)
			if err != nil {
				return cr, fmt.Errorf("compiling query param regex %q: %w", qp.Value, err)
			}
			cqp.regex = re
		}
		cr.queryParams = append(cr.queryParams, cqp)
	}

	// Hostnames: merge group + route.
	if g != nil {
		cr.hostnames = append(cr.hostnames, g.Hostnames...)
	}
	for _, h := range r.Match.Hostnames {
		found := false
		for _, existing := range cr.hostnames {
			if existing == h {
				found = true
				break
			}
		}
		if !found {
			cr.hostnames = append(cr.hostnames, h)
		}
	}

	// CEL expression.
	if r.Match.CEL != "" {
		prg, err := celeval.Compile(r.Match.CEL)
		if err != nil {
			return cr, err
		}
		cr.celProgram = prg
		if prg.NeedsBody() {
			cr.needsBody = true
		}
	}

	// Build handler with middleware chain.
	cr.handler = buildRouteHandler(r, g, pools, allMw, onCleanup, sessStore, celBodyMaxSize)

	return cr, nil
}
