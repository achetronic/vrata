package proxy

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/proxy/celeval"
)

// BuildTable compiles a routing table from model entities.
func BuildTable(
	routes []model.Route,
	groups []model.RouteGroup,
	destinations []model.Destination,
	middlewares []model.Middleware,
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

	// Build upstreams.
	upstreams := make(map[string]*Upstream, len(destinations))
	for _, d := range destinations {
		u, err := NewUpstream(d)
		if err != nil {
			return nil, err
		}
		upstreams[d.ID] = u
	}

	var compiled []compiledRoute

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
			cr, err := compileRoute(r, &gCopy, upstreams, mwByID)
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
		cr, err := compileRoute(r, nil, upstreams, mwByID)
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

	// Build balancer rings/tables for upstreams that use consistent hashing.
	// Collect all backend refs that point to each upstream.
	backendsByDest := make(map[string][]model.BackendRef)
	for _, cr := range compiled {
		if cr.model.Forward != nil {
			for _, b := range cr.model.Forward.Backends {
				backendsByDest[b.DestinationID] = append(backendsByDest[b.DestinationID], b)
			}
		}
	}
	for destID, u := range upstreams {
		if u.Balancer == nil {
			continue
		}
		if backends, ok := backendsByDest[destID]; ok {
			if builder, ok := u.Balancer.(interface{ Build([]model.BackendRef) }); ok {
				builder.Build(backends)
			}
		}
	}

	return &RoutingTable{
		routes:       compiled,
		destinations: upstreams,
		middlewares:  mwByID,
	}, nil
}

// Upstreams returns the upstream map from the routing table.
func (t *RoutingTable) Upstreams() map[string]*Upstream {
	return t.destinations
}

// compileRoute pre-compiles a route for fast matching.
func compileRoute(
	r model.Route,
	g *model.RouteGroup,
	upstreams map[string]*Upstream,
	allMw map[string]model.Middleware,
) (compiledRoute, error) {
	cr := compiledRoute{
		model:   r,
		group:   g,
		grpcOnly: r.Match.GRPC,
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

	// Headers from route + group.
	cr.headers = append(cr.headers, r.Match.Headers...)
	if g != nil {
		cr.headers = append(cr.headers, g.Headers...)
	}

	// Query params.
	cr.queryParams = r.Match.QueryParams

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
	}

	// Build handler with middleware chain.
	cr.handler = buildRouteHandler(r, g, upstreams, allMw)

	return cr, nil
}
