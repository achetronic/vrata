// Package xds implements the Envoy xDS snapshot builder for Rutoso.
package xds

import (
	"fmt"
	"regexp"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/achetronic/rutoso/internal/model"
)

// BuildSnapshot converts listeners, filters, groups and routes into a complete
// Envoy xDS Snapshot.
//
// Each model.Listener produces one Envoy Listener. Filters referenced by
// FilterIDs are resolved and their configs are wired into the HCM filter chain
// in declaration order (router filter is always appended last).
//
// When no listeners are stored, a default listener on 0.0.0.0:80 is generated
// automatically so the xDS server always emits a valid snapshot.
//
// One Cluster + ClusterLoadAssignment is generated per unique Backend name.
// All virtual hosts share a single RouteConfiguration.
func BuildSnapshot(version string, modelListeners []model.Listener, modelFilters []model.Filter, groups []model.RouteGroup, routes []model.Route) (*cachev3.Snapshot, error) {
	// Build a lookup map so we can resolve route IDs in O(1).
	routeByID := make(map[string]model.Route, len(routes))
	for _, r := range routes {
		routeByID[r.ID] = r
	}

	// Build a lookup map for filters so the listener builder can resolve them.
	filterByID := make(map[string]model.Filter, len(modelFilters))
	for _, f := range modelFilters {
		filterByID[f.ID] = f
	}

	var (
		clusters  []types.Resource
		endpoints []types.Resource
		vhosts    []*routev3.VirtualHost
		seen      = make(map[string]bool) // tracks already-created cluster names
	)

	for _, g := range groups {
		for _, routeID := range g.RouteIDs {
			r, ok := routeByID[routeID]
			if !ok {
				// Route referenced by group does not exist; skip gracefully.
				continue
			}

			vhost := buildVirtualHost(g, r)
			vhosts = append(vhosts, vhost)

			for _, b := range r.Backends {
				if seen[b.Name] {
					continue
				}
				seen[b.Name] = true
				clusters = append(clusters, buildCluster(b))
				endpoints = append(endpoints, buildEndpoint(b))
			}
		}
	}

	// Single RouteConfiguration with all virtual hosts.
	routeConfig := &routev3.RouteConfiguration{
		Name:         "rutoso_routes",
		VirtualHosts: vhosts,
	}

	// Build one Envoy Listener per model.Listener stored in the database.
	// If none are stored yet, fall back to a default listener on port 80 so
	// the dev environment keeps working out of the box.
	var envoyListeners []types.Resource
	if len(modelListeners) == 0 {
		defaultListener, err := buildListenerFromModel(model.Listener{
			ID:      "rutoso_default",
			Name:    "default",
			Address: "0.0.0.0",
			Port:    80,
		}, filterByID, routeConfig.Name)
		if err != nil {
			return nil, fmt.Errorf("building default listener: %w", err)
		}
		envoyListeners = append(envoyListeners, defaultListener)
	} else {
		for _, ml := range modelListeners {
			el, err := buildListenerFromModel(ml, filterByID, routeConfig.Name)
			if err != nil {
				return nil, fmt.Errorf("building listener %q: %w", ml.ID, err)
			}
			envoyListeners = append(envoyListeners, el)
		}
	}

	snap, err := cachev3.NewSnapshot(version, map[resourcev3.Type][]types.Resource{
		resourcev3.ClusterType:  clusters,
		resourcev3.EndpointType: endpoints,
		resourcev3.RouteType:    {routeConfig},
		resourcev3.ListenerType: envoyListeners,
	})
	if err != nil {
		return nil, fmt.Errorf("creating xds snapshot: %w", err)
	}

	return snap, nil
}

// buildVirtualHost creates an Envoy VirtualHost for a single Route within its group.
// Group-level hostnames are merged (union) with the route's own hostnames.
func buildVirtualHost(g model.RouteGroup, r model.Route) *routev3.VirtualHost {
	// Merge hostnames: start with the route's own, then add any from the group
	// that are not already present.
	domains := append([]string{}, r.Match.Hostnames...)
	for _, h := range g.Hostnames {
		if !contains(domains, h) {
			domains = append(domains, h)
		}
	}
	if len(domains) == 0 {
		domains = []string{"*"}
	}

	route := buildRouteAction(g, r)

	return &routev3.VirtualHost{
		Name:    fmt.Sprintf("%s__%s", g.ID, r.ID),
		Domains: domains,
		Routes:  []*routev3.Route{route},
	}
}

// buildRouteAction builds the Envoy Route message for a single Rutoso Route,
// including the match conditions and the weighted cluster action.
func buildRouteAction(g model.RouteGroup, r model.Route) *routev3.Route {
	match := buildRouteMatch(g, r)
	action := buildWeightedClusterAction(r)

	return &routev3.Route{
		Match:  match,
		Action: action,
	}
}

// buildRouteMatch converts a MatchRule (plus group-level matchers) into an
// Envoy RouteMatch. Path composition follows these rules:
//
// Group has PathPrefix:
//
//	+---------------------+-----------------------------------------+
//	| Route path specifier | Result                                  |
//	+---------------------+-----------------------------------------+
//	| Path (exact)        | Prefix + Path  (exact match)            |
//	| PathPrefix          | Prefix + PathPrefix  (prefix match)     |
//	| PathRegex           | Prefix + PathRegex  (safe_regex match)  |
//	| (none)              | Prefix  (prefix match)                  |
//	+---------------------+-----------------------------------------+
//
// Group has PathRegex:
//
//	+---------------------+-------------------------------------------------+
//	| Route path specifier | Result                                          |
//	+---------------------+-------------------------------------------------+
//	| PathRegex           | (?:GroupRegex)(?:RouteRegex)  (composed regex)  |
//	| Path (exact)        | (?:GroupRegex)(?:QuoteMeta(Path))               |
//	| PathPrefix          | (?:GroupRegex)(?:QuoteMeta(PathPrefix))         |
//	| (none)              | GroupRegex  (group regex is the full match)     |
//	+---------------------+-------------------------------------------------+
//
// In the regex+literal cases the route's literal is escaped with
// regexp.QuoteMeta so that special characters in a plain path segment
// (e.g. dots in "/v1.0/") are never misinterpreted as regex metacharacters.
//
// Group headers are appended to the route's own header matchers.
func buildRouteMatch(g model.RouteGroup, r model.Route) *routev3.RouteMatch {
	rm := &routev3.RouteMatch{}

	switch {
	case g.PathRegex != "":
		// Group defines a regex namespace — compose with the route's path specifier.
		var finalRegex string
		switch {
		case r.Match.PathRegex != "":
			// Both are regex: compose as (?:group)(?:route).
			finalRegex = "(?:" + g.PathRegex + ")(?:" + r.Match.PathRegex + ")"
		case r.Match.Path != "":
			// Route has a literal exact path: escape it before composing.
			finalRegex = "(?:" + g.PathRegex + ")(?:" + regexp.QuoteMeta(r.Match.Path) + ")"
		case r.Match.PathPrefix != "":
			// Route has a literal prefix: escape it before composing.
			finalRegex = "(?:" + g.PathRegex + ")(?:" + regexp.QuoteMeta(r.Match.PathPrefix) + ")"
		default:
			// No route path specifier: the group regex is the full match.
			finalRegex = g.PathRegex
		}
		rm.PathSpecifier = &routev3.RouteMatch_SafeRegex{
			SafeRegex: &matcherv3.RegexMatcher{Regex: finalRegex},
		}

	default:
		// Group defines a literal prefix (possibly empty).
		prefix := g.PathPrefix
		switch {
		case r.Match.Path != "":
			rm.PathSpecifier = &routev3.RouteMatch_Path{Path: prefix + r.Match.Path}
		case r.Match.PathPrefix != "":
			rm.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: prefix + r.Match.PathPrefix}
		case r.Match.PathRegex != "":
			rm.PathSpecifier = &routev3.RouteMatch_SafeRegex{
				SafeRegex: &matcherv3.RegexMatcher{Regex: prefix + r.Match.PathRegex},
			}
		default:
			// Match everything under the group prefix (or "/" if no prefix).
			p := prefix
			if p == "" {
				p = "/"
			}
			rm.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: p}
		}
	}

	// Route headers first, then group headers on top.
	allHeaders := append(r.Match.Headers, g.Headers...)
	for _, h := range allHeaders {
		rm.Headers = append(rm.Headers, buildHeaderMatcher(h))
	}

	return rm
}

// buildWeightedClusterAction creates a weighted cluster route action.
// If only one backend is defined, a simple cluster action is used instead.
func buildWeightedClusterAction(r model.Route) *routev3.Route_Route {
	if len(r.Backends) == 1 {
		return &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_Cluster{
					Cluster: r.Backends[0].Name,
				},
			},
		}
	}

	var wcs []*routev3.WeightedCluster_ClusterWeight
	for _, b := range r.Backends {
		wcs = append(wcs, &routev3.WeightedCluster_ClusterWeight{
			Name:   b.Name,
			Weight: wrapperspb.UInt32(b.Weight),
		})
	}

	return &routev3.Route_Route{
		Route: &routev3.RouteAction{
			ClusterSpecifier: &routev3.RouteAction_WeightedClusters{
				WeightedClusters: &routev3.WeightedCluster{
					Clusters: wcs,
				},
			},
		},
	}
}

// buildHeaderMatcher converts a model.HeaderMatcher into an Envoy HeaderMatcher.
func buildHeaderMatcher(h model.HeaderMatcher) *routev3.HeaderMatcher {
	hm := &routev3.HeaderMatcher{Name: h.Name}
	if h.Regex {
		hm.HeaderMatchSpecifier = &routev3.HeaderMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_SafeRegex{
					SafeRegex: &matcherv3.RegexMatcher{Regex: h.Value},
				},
			},
		}
	} else if h.Value != "" {
		hm.HeaderMatchSpecifier = &routev3.HeaderMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_Exact{Exact: h.Value},
			},
		}
	}
	return hm
}

// buildCluster creates an Envoy Cluster for a Backend using EDS discovery.
func buildCluster(b model.Backend) *clusterv3.Cluster {
	return &clusterv3.Cluster{
		Name:                 b.Name,
		ConnectTimeout:       durationpb.New(5 * time.Second),
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
		EdsClusterConfig: &clusterv3.Cluster_EdsClusterConfig{
			EdsConfig: &corev3.ConfigSource{
				ConfigSourceSpecifier: &corev3.ConfigSource_Ads{},
			},
		},
	}
}

// buildEndpoint creates the ClusterLoadAssignment (EDS resource) for a Backend.
func buildEndpoint(b model.Backend) *endpointv3.ClusterLoadAssignment {
	return &endpointv3.ClusterLoadAssignment{
		ClusterName: b.Name,
		Endpoints: []*endpointv3.LocalityLbEndpoints{
			{
				LbEndpoints: []*endpointv3.LbEndpoint{
					{
						HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
							Endpoint: &endpointv3.Endpoint{
								Address: &corev3.Address{
									Address: &corev3.Address_SocketAddress{
										SocketAddress: &corev3.SocketAddress{
											Address: b.Host,
											PortSpecifier: &corev3.SocketAddress_PortValue{
												PortValue: b.Port,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildListenerFromModel creates an Envoy Listener from a model.Listener.
// It resolves the filter IDs against the provided map and wires each filter
// into the HCM pipeline in declaration order. The router filter is always
// appended as the last HTTP filter.
//
// TLS config is stored in model.Listener.TLS but is NOT yet applied here;
// DownstreamTlsContext generation is deferred to a future iteration.
func buildListenerFromModel(ml model.Listener, filterByID map[string]model.Filter, routeConfigName string) (*listenerv3.Listener, error) {
	router, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, fmt.Errorf("marshalling router filter: %w", err)
	}

	// Start with the router filter; prepend resolved HTTP filters before it.
	httpFilters := []*hcmv3.HttpFilter{
		{
			Name:       "envoy.filters.http.router",
			ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: router},
		},
	}

	// Resolve filter IDs in reverse so we can prepend each resolved filter
	// in declaration order using a simple append-then-reverse approach.
	var resolved []*hcmv3.HttpFilter
	for _, fid := range ml.FilterIDs {
		f, ok := filterByID[fid]
		if !ok {
			// Unknown filter ID — skip gracefully; do not crash the snapshot.
			continue
		}
		hf, err := buildHTTPFilter(f)
		if err != nil {
			return nil, fmt.Errorf("building filter %q: %w", fid, err)
		}
		if hf != nil {
			resolved = append(resolved, hf)
		}
	}
	// Prepend resolved filters before the router.
	httpFilters = append(resolved, httpFilters...)

	addr := ml.Address
	if addr == "" {
		addr = "0.0.0.0"
	}

	hcm := &hcmv3.HttpConnectionManager{
		StatPrefix: "rutoso_ingress",
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{
			Rds: &hcmv3.Rds{
				ConfigSource: &corev3.ConfigSource{
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{},
				},
				RouteConfigName: routeConfigName,
			},
		},
		HttpFilters: httpFilters,
	}

	hcmAny, err := anypb.New(hcm)
	if err != nil {
		return nil, fmt.Errorf("marshalling HCM to Any: %w", err)
	}

	return &listenerv3.Listener{
		Name: ml.ID,
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address: addr,
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: ml.Port,
					},
				},
			},
		},
		FilterChains: []*listenerv3.FilterChain{
			{
				Filters: []*listenerv3.Filter{
					{
						Name:       "envoy.filters.network.http_connection_manager",
						ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcmAny},
					},
				},
			},
		},
	}, nil
}

// buildHTTPFilter converts a model.Filter into an Envoy HttpFilter proto.
// Returns nil when the filter type is recognised but has no config (no-op).
// Returns an error only when marshalling fails.
//
// NOTE: CORS, JWT, ExtAuthz, and ExtProc full config generation is stubbed here.
// The filter is registered in the HCM pipeline with an empty typed config so
// Envoy knows it exists. Per-route overrides will reference these filter names.
// Full config generation will be wired in a follow-up iteration.
func buildHTTPFilter(f model.Filter) (*hcmv3.HttpFilter, error) {
	switch f.Type {
	case model.FilterTypeCORS:
		// CORS filter with empty config — Envoy applies global defaults.
		// Full CORSConfig mapping is deferred.
		return &hcmv3.HttpFilter{
			Name: "envoy.filters.http.cors",
		}, nil

	case model.FilterTypeJWT:
		// JWT authn filter stub — registered so per-route overrides are valid.
		// Full JWTConfig mapping is deferred.
		return &hcmv3.HttpFilter{
			Name: "envoy.filters.http.jwt_authn",
		}, nil

	case model.FilterTypeExtAuthz:
		// ext_authz filter stub.
		return &hcmv3.HttpFilter{
			Name: "envoy.filters.http.ext_authz",
		}, nil

	case model.FilterTypeExtProc:
		// ext_proc filter stub.
		return &hcmv3.HttpFilter{
			Name: "envoy.filters.http.ext_proc",
		}, nil

	default:
		// Unknown filter type — skip without error.
		return nil, nil
	}
}

// contains reports whether s appears in the slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
