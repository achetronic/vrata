// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package xds implements the xDS control plane server for Envoy.
// It translates Vrata model entities into Envoy xDS resources and pushes
// them to connected Envoy instances via ADS.
package xds

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/achetronic/vrata/internal/model"
)

// Server is the xDS control plane server.
type Server struct {
	cache   cachev3.SnapshotCache
	version atomic.Int64
	logger  *slog.Logger
}

// New creates a new xDS Server.
func New(logger *slog.Logger) *Server {
	return &Server{
		cache:  cachev3.NewSnapshotCache(false, cachev3.IDHash{}, nil),
		logger: logger,
	}
}

// Run starts the gRPC server on the given address. Blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("xds: listening on %s: %w", address, err)
	}

	grpcSrv := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(grpcSrv, serverv3.NewServer(ctx, s.cache, nil))

	s.logger.Info("xds server listening", slog.String("address", address))

	errCh := make(chan error, 1)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			errCh <- fmt.Errorf("xds: grpc server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		grpcSrv.GracefulStop()
		s.logger.Info("xds server stopped")
		return nil
	case err := <-errCh:
		return err
	}
}

// PushSnapshot translates Vrata model state into Envoy xDS and pushes to all
// connected nodes.
func (s *Server) PushSnapshot(
	ctx context.Context,
	listeners []model.Listener,
	groups []model.RouteGroup,
	routes []model.Route,
	destinations []model.Destination,
	middlewares []model.Middleware,
) error {
	version := strconv.FormatInt(s.version.Add(1), 10)

	clusters, eps := buildClusters(destinations)
	envoyListeners, rcs := buildListenersAndRoutes(listeners, groups, routes)

	resources := map[string][]cachetypes.Resource{
		resourcev3.ClusterType:  toResourceSlice(clusters),
		resourcev3.EndpointType: toResourceSlice(eps),
		resourcev3.ListenerType: toResourceSlice(envoyListeners),
		resourcev3.RouteType:    toResourceSlice(rcs),
	}

	snap, err := cachev3.NewSnapshot(version, resources)
	if err != nil {
		return fmt.Errorf("xds: building snapshot: %w", err)
	}

	if err := s.cache.SetSnapshot(ctx, "", snap); err != nil {
		return fmt.Errorf("xds: setting snapshot: %w", err)
	}

	s.logger.Info("xds: snapshot pushed",
		slog.String("version", version),
		slog.Int("clusters", len(clusters)),
		slog.Int("listeners", len(envoyListeners)),
	)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Clusters + Endpoints
// ─────────────────────────────────────────────────────────────────────────────

func buildClusters(destinations []model.Destination) ([]*clusterv3.Cluster, []*endpointv3.ClusterLoadAssignment) {
	clusters := make([]*clusterv3.Cluster, 0, len(destinations))
	eps := make([]*endpointv3.ClusterLoadAssignment, 0, len(destinations))

	for _, d := range destinations {
		cname := clusterName(d.ID)

		lbeps := make([]*endpointv3.LbEndpoint, 0, len(d.Endpoints))
		for _, ep := range d.Endpoints {
			lbeps = append(lbeps, &endpointv3.LbEndpoint{
				HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
					Endpoint: &endpointv3.Endpoint{
						Address: &corev3.Address{
							Address: &corev3.Address_SocketAddress{
								SocketAddress: &corev3.SocketAddress{
									Address:       ep.Host,
									PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: uint32(ep.Port)},
								},
							},
						},
					},
				},
			})
		}

		eps = append(eps, &endpointv3.ClusterLoadAssignment{
			ClusterName: cname,
			Endpoints:   []*endpointv3.LocalityLbEndpoints{{LbEndpoints: lbeps}},
		})

		clusters = append(clusters, &clusterv3.Cluster{
			Name:                 cname,
			ConnectTimeout:       durationpb(2),
			ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
			EdsClusterConfig: &clusterv3.Cluster_EdsClusterConfig{
				EdsConfig: &corev3.ConfigSource{
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
				},
			},
			LbPolicy: clusterv3.Cluster_ROUND_ROBIN,
		})
	}

	return clusters, eps
}

// ─────────────────────────────────────────────────────────────────────────────
// Listeners + RouteConfigurations
// ─────────────────────────────────────────────────────────────────────────────

func buildListenersAndRoutes(
	listeners []model.Listener,
	groups []model.RouteGroup,
	routes []model.Route,
) ([]*listenerv3.Listener, []*routev3.RouteConfiguration) {
	// Index routes by ID for O(1) lookup.
	routeByID := make(map[string]model.Route, len(routes))
	for _, r := range routes {
		routeByID[r.ID] = r
	}

	// Index groups by ID.
	groupByID := make(map[string]model.RouteGroup, len(groups))
	for _, g := range groups {
		groupByID[g.ID] = g
	}

	envoyListeners := make([]*listenerv3.Listener, 0, len(listeners))
	rcs := make([]*routev3.RouteConfiguration, 0, len(listeners))

	for _, l := range listeners {
		rcName := routeConfigName(l.ID)

		// Determine which groups belong to this listener.
		// If GroupIDs is empty, attach all groups (catch-all).
		var targetGroups []model.RouteGroup
		if len(l.GroupIDs) == 0 {
			targetGroups = groups
		} else {
			for _, gid := range l.GroupIDs {
				if g, ok := groupByID[gid]; ok {
					targetGroups = append(targetGroups, g)
				}
			}
		}

		// Build one VirtualHost per group.
		var vhosts []*routev3.VirtualHost
		for _, g := range targetGroups {
			var envoyRoutes []*routev3.Route
			for _, rid := range g.RouteIDs {
				r, ok := routeByID[rid]
				if !ok {
					continue
				}
				if er := buildRoute(r, g); er != nil {
					envoyRoutes = append(envoyRoutes, er)
				}
			}
			if len(envoyRoutes) == 0 {
				continue
			}

			// Domains: use group hostnames if set, else wildcard.
			domains := g.Hostnames
			if len(domains) == 0 {
				domains = []string{"*"}
			}

			vhosts = append(vhosts, &routev3.VirtualHost{
				Name:    g.ID,
				Domains: domains,
				Routes:  envoyRoutes,
			})
		}

		rcs = append(rcs, &routev3.RouteConfiguration{
			Name:         rcName,
			VirtualHosts: vhosts,
		})

		envoyListeners = append(envoyListeners, buildEnvoyListener(l, rcName))
	}

	return envoyListeners, rcs
}

// buildRoute translates a Vrata Route into an Envoy Route.
func buildRoute(r model.Route, g model.RouteGroup) *routev3.Route {
	match := buildRouteMatch(r, g)

	// No forward action → skip (redirect and direct response not implemented yet).
	if r.Forward == nil || len(r.Forward.Destinations) == 0 {
		return nil
	}

	var action *routev3.Route_Route
	if len(r.Forward.Destinations) == 1 {
		action = &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_Cluster{
					Cluster: clusterName(r.Forward.Destinations[0].DestinationID),
				},
			},
		}
	} else {
		// Weighted clusters.
		wcs := make([]*routev3.WeightedCluster_ClusterWeight, 0, len(r.Forward.Destinations))
		for _, d := range r.Forward.Destinations {
			wcs = append(wcs, &routev3.WeightedCluster_ClusterWeight{
				Name:   clusterName(d.DestinationID),
				Weight: wrapperspb.UInt32(d.Weight),
			})
		}
		action = &routev3.Route_Route{
			Route: &routev3.RouteAction{
				ClusterSpecifier: &routev3.RouteAction_WeightedClusters{
					WeightedClusters: &routev3.WeightedCluster{Clusters: wcs},
				},
			},
		}
	}

	// Timeout.
	if r.Forward.Timeouts != nil && r.Forward.Timeouts.Request != "" {
		if d, err := parseDuration(r.Forward.Timeouts.Request); err == nil {
			action.Route.Timeout = d
		}
	}

	return &routev3.Route{Match: match, Action: action}
}

// buildRouteMatch constructs the Envoy RouteMatch, composing group and route
// path prefixes the same way the native proxy does.
func buildRouteMatch(r model.Route, g model.RouteGroup) *routev3.RouteMatch {
	match := &routev3.RouteMatch{}

	// Compose group path prefix with route path.
	prefix := g.PathPrefix

	switch {
	case r.Match.Path != "":
		match.PathSpecifier = &routev3.RouteMatch_Path{Path: prefix + r.Match.Path}
	case r.Match.PathPrefix != "":
		match.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: prefix + r.Match.PathPrefix}
	case r.Match.PathRegex != "":
		match.PathSpecifier = &routev3.RouteMatch_SafeRegex{
			SafeRegex: &matcherv3.RegexMatcher{
				EngineType: &matcherv3.RegexMatcher_GoogleRe2{GoogleRe2: &matcherv3.RegexMatcher_GoogleRE2{}},
				Regex:      r.Match.PathRegex,
			},
		}
	default:
		if prefix != "" {
			match.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: prefix}
		} else {
			match.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: "/"}
		}
	}

	// Header matchers (group headers appended to route headers).
	for _, hm := range append(g.Headers, r.Match.Headers...) {
		match.Headers = append(match.Headers, &routev3.HeaderMatcher{
			Name:                 hm.Name,
			HeaderMatchSpecifier: &routev3.HeaderMatcher_ExactMatch{ExactMatch: hm.Value},
		})
	}

	// Method matcher.
	if len(r.Match.Methods) > 0 {
		match.Headers = append(match.Headers, &routev3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &routev3.HeaderMatcher_ExactMatch{
				ExactMatch: r.Match.Methods[0],
			},
		})
	}

	return match
}

// buildEnvoyListener constructs an Envoy Listener with HCM.
func buildEnvoyListener(l model.Listener, rcName string) *listenerv3.Listener {
	addr := l.Address
	if addr == "" {
		addr = "0.0.0.0"
	}

	return &listenerv3.Listener{
		Name: l.ID,
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address:       addr,
					PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: l.Port},
				},
			},
		},
		FilterChains: []*listenerv3.FilterChain{
			{Filters: []*listenerv3.Filter{buildHCM(rcName)}},
		},
	}
}

// toResourceSlice converts a typed proto slice to []cachetypes.Resource.
func toResourceSlice[T cachetypes.Resource](in []T) []cachetypes.Resource {
	out := make([]cachetypes.Resource, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
