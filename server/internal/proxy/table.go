package proxy

import (
	"regexp"
	"strings"

	"github.com/achetronic/rutoso/internal/model"
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
			continue
		}
		compiled = append(compiled, cr)
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

	// Build handler with middleware chain.
	cr.handler = buildRouteHandler(r, g, upstreams, allMw)

	return cr, nil
}
