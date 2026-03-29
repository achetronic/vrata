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
	"strings"
	"sync/atomic"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	envoytype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
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
	envoyListeners, rcs := buildListenersAndRoutes(listeners, groups, routes, middlewares)

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

		cluster := &clusterv3.Cluster{
			Name:                 cname,
			ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterTypeFor(d)},
			EdsClusterConfig: &clusterv3.Cluster_EdsClusterConfig{
				EdsConfig: &corev3.ConfigSource{
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
				},
			},
			LbPolicy: lbPolicyFor(d),
		}

		// Circuit breaker.
		if d.Options != nil && d.Options.CircuitBreaker != nil {
			cb := d.Options.CircuitBreaker
			threshold := &clusterv3.CircuitBreakers_Thresholds{}
			if cb.MaxConnections > 0 {
				threshold.MaxConnections = wrapperspb.UInt32(cb.MaxConnections)
			}
			if cb.MaxPendingRequests > 0 {
				threshold.MaxPendingRequests = wrapperspb.UInt32(cb.MaxPendingRequests)
			}
			if cb.MaxRequests > 0 {
				threshold.MaxRequests = wrapperspb.UInt32(cb.MaxRequests)
			}
			if cb.MaxRetries > 0 {
				threshold.MaxRetries = wrapperspb.UInt32(cb.MaxRetries)
			}
			cluster.CircuitBreakers = &clusterv3.CircuitBreakers{
				Thresholds: []*clusterv3.CircuitBreakers_Thresholds{threshold},
			}
		}

		// Outlier detection.
		if d.Options != nil && d.Options.OutlierDetection != nil {
			od := d.Options.OutlierDetection
			detection := &clusterv3.OutlierDetection{}
			if od.Consecutive5xx > 0 {
				detection.Consecutive_5Xx = wrapperspb.UInt32(od.Consecutive5xx)
			}
			if od.ConsecutiveGatewayErrors > 0 {
				detection.ConsecutiveGatewayFailure = wrapperspb.UInt32(od.ConsecutiveGatewayErrors)
			}
			if od.MaxEjectionPercent > 0 {
				detection.MaxEjectionPercent = wrapperspb.UInt32(od.MaxEjectionPercent)
			}
			if od.Interval != "" {
				if dur, err := parseDuration(od.Interval); err == nil {
					detection.Interval = dur
				}
			}
			if od.BaseEjectionTime != "" {
				if dur, err := parseDuration(od.BaseEjectionTime); err == nil {
					detection.BaseEjectionTime = dur
				}
			}
			cluster.OutlierDetection = detection
		}

		// Connect timeout.
		connectTimeout := int64(2)
		if d.Options != nil && d.Options.Timeouts != nil && d.Options.Timeouts.Connect != "" {
			if dur, err := parseDuration(d.Options.Timeouts.Connect); err == nil {
				cluster.ConnectTimeout = dur
			} else {
				cluster.ConnectTimeout = durationpb(connectTimeout)
			}
		} else {
			cluster.ConnectTimeout = durationpb(connectTimeout)
		}

		// Upstream TLS.
		if d.Options != nil && d.Options.TLS != nil && d.Options.TLS.Mode != "" && d.Options.TLS.Mode != model.TLSModeNone {
			if tsAny, err := buildUpstreamTLS(d.Options.TLS, d.Host); err == nil {
				cluster.TransportSocket = &corev3.TransportSocket{
					Name:       "envoy.transport_sockets.tls",
					ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: tsAny},
				}
			}
		}

		// HTTP/2 upstream.
		if d.Options != nil && d.Options.HTTP2 {
			cluster.Http2ProtocolOptions = &corev3.Http2ProtocolOptions{}
		}

		// Max requests per connection.
		if d.Options != nil && d.Options.MaxRequestsPerConnection > 0 {
			cluster.MaxRequestsPerConnection = wrapperspb.UInt32(d.Options.MaxRequestsPerConnection)
		}

		// Health check.
		if d.Options != nil && d.Options.HealthCheck != nil {
			cluster.HealthChecks = buildHealthChecks(d.Options.HealthCheck)
		}

		// Ring hash / Maglev config.
		if d.Options != nil && d.Options.EndpointBalancing != nil {
			eb := d.Options.EndpointBalancing
			if eb.Algorithm == model.EndpointLBRingHash && eb.RingHash != nil {
				if eb.RingHash.RingSize != nil {
					cluster.LbConfig = &clusterv3.Cluster_RingHashLbConfig_{
						RingHashLbConfig: &clusterv3.Cluster_RingHashLbConfig{
							MinimumRingSize: wrapperspb.UInt64(eb.RingHash.RingSize.Min),
							MaximumRingSize: wrapperspb.UInt64(eb.RingHash.RingSize.Max),
						},
					}
				}
			}
			if eb.Algorithm == model.EndpointLBMaglev && eb.Maglev != nil && eb.Maglev.TableSize > 0 {
				cluster.LbConfig = &clusterv3.Cluster_MaglevLbConfig_{
					MaglevLbConfig: &clusterv3.Cluster_MaglevLbConfig{
						TableSize: wrapperspb.UInt64(eb.Maglev.TableSize),
					},
				}
			}
		}

		clusters = append(clusters, cluster)
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
	middlewares []model.Middleware,
) ([]*listenerv3.Listener, []*routev3.RouteConfiguration) {
	// Index middlewares by ID.
	mwByID := make(map[string]model.Middleware, len(middlewares))
	for _, m := range middlewares {
		mwByID[m.ID] = m
	}
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

		// Collect middlewares active on any group attached to this listener.
		activeMWIDs := make(map[string]struct{})
		for _, g := range targetGroups {
			for _, mid := range g.MiddlewareIDs {
				activeMWIDs[mid] = struct{}{}
			}
		}
		activeMWs := make([]model.Middleware, 0, len(activeMWIDs))
		for mid := range activeMWIDs {
			if mw, ok := mwByID[mid]; ok {
				activeMWs = append(activeMWs, mw)
			}
		}

		envoyListeners = append(envoyListeners, buildEnvoyListener(l, rcName, activeMWs))
	}

	// Check all routes for STICKY destination balancing to inject Go plugin.
	needsSticky := false
	for _, g := range groups {
		for _, rid := range g.RouteIDs {
			if r, ok := routeByID[rid]; ok && r.Forward != nil && r.Forward.DestinationBalancing != nil {
				if r.Forward.DestinationBalancing.Algorithm == model.DestinationLBSticky {
					needsSticky = true
					break
				}
			}
		}
		if needsSticky {
			break
		}
	}
	_ = needsSticky

	return envoyListeners, rcs
}

// buildRoute translates a Vrata Route into an Envoy Route.
func buildRoute(r model.Route, g model.RouteGroup) *routev3.Route {
	match := buildRouteMatch(r, g)

	var envoyRoute *routev3.Route

	switch {
	case r.Forward != nil && len(r.Forward.Destinations) > 0:
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

		// Retry.
		if r.Forward.Retry != nil {
			action.Route.RetryPolicy = buildRetryPolicy(r.Forward.Retry)
		}

		// Rewrite.
		if r.Forward.Rewrite != nil {
			applyRewrite(action.Route, r.Forward.Rewrite)
		}

		// Mirror.
		if r.Forward.Mirror != nil && r.Forward.Mirror.DestinationID != "" {
			pct := r.Forward.Mirror.Percentage
			if pct == 0 {
				pct = 100
			}
			action.Route.RequestMirrorPolicies = []*routev3.RouteAction_RequestMirrorPolicy{
				{
					Cluster: clusterName(r.Forward.Mirror.DestinationID),
					RuntimeFraction: &corev3.RuntimeFractionalPercent{
						DefaultValue: &envoytype.FractionalPercent{
							Numerator:   pct,
							Denominator: envoytype.FractionalPercent_HUNDRED,
						},
					},
				},
			}
		}

		// Hash policy for consistent hash routing.
		if r.Forward.DestinationBalancing != nil {
			db := r.Forward.DestinationBalancing
			switch db.Algorithm {
			case model.DestinationLBWeightedConsistentHash:
				if db.WeightedConsistentHash != nil && db.WeightedConsistentHash.Cookie != nil {
					cookie := db.WeightedConsistentHash.Cookie
					hp := &routev3.RouteAction_HashPolicy{
						PolicySpecifier: &routev3.RouteAction_HashPolicy_Cookie_{
							Cookie: &routev3.RouteAction_HashPolicy_Cookie{
								Name: cookie.Name,
							},
						},
					}
					if cookie.TTL != "" {
						if d, err := parseDuration(cookie.TTL); err == nil {
							hp.PolicySpecifier.(*routev3.RouteAction_HashPolicy_Cookie_).Cookie.Ttl = d
						}
					}
					action.Route.HashPolicy = []*routev3.RouteAction_HashPolicy{hp}
				}
			case model.DestinationLBSticky:
				if db.Sticky != nil && db.Sticky.Cookie != nil {
					cookie := db.Sticky.Cookie
					hp := &routev3.RouteAction_HashPolicy{
						PolicySpecifier: &routev3.RouteAction_HashPolicy_Cookie_{
							Cookie: &routev3.RouteAction_HashPolicy_Cookie{
								Name: cookie.Name,
							},
						},
					}
					if cookie.TTL != "" {
						if d, err := parseDuration(cookie.TTL); err == nil {
							hp.PolicySpecifier.(*routev3.RouteAction_HashPolicy_Cookie_).Cookie.Ttl = d
						}
					}
					action.Route.HashPolicy = []*routev3.RouteAction_HashPolicy{hp}
				}
			}
		}

		envoyRoute = &routev3.Route{Match: match, Action: action}

	case r.Redirect != nil:
		envoyRoute = &routev3.Route{
			Match:  match,
			Action: &routev3.Route_Redirect{Redirect: buildRedirectAction(r.Redirect)},
		}

	case r.DirectResponse != nil:
		envoyRoute = &routev3.Route{
			Match: match,
			Action: &routev3.Route_DirectResponse{
				DirectResponse: &routev3.DirectResponseAction{
					Status: r.DirectResponse.Status,
					Body: &corev3.DataSource{
						Specifier: &corev3.DataSource_InlineString{InlineString: r.DirectResponse.Body},
					},
				},
			},
		}

	default:
		return nil
	}

	return envoyRoute
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

// buildEnvoyListener constructs an Envoy Listener with HCM, TLS, and middleware filters.
func buildEnvoyListener(l model.Listener, rcName string, activeMWs []model.Middleware) *listenerv3.Listener {
	addr := l.Address
	if addr == "" {
		addr = "0.0.0.0"
	}

	// Build HTTP filters: inject xfcc when mTLS is configured.
	hasMTLS := l.TLS != nil && l.TLS.ClientAuth != nil && l.TLS.ClientAuth.CAFile != ""
	httpFilters := buildHTTPFilters(activeMWs, hasMTLS)
	accessLogs := buildAccessLogs(activeMWs)
	hcm := buildHCM(rcName, httpFilters, accessLogs)

	filterChain := &listenerv3.FilterChain{
		Filters: []*listenerv3.Filter{hcm},
	}

	// TLS termination.
	if l.TLS != nil && l.TLS.CertPath != "" && l.TLS.KeyPath != "" {
		caPath := ""
		requireClient := false
		if l.TLS.ClientAuth != nil {
			caPath = l.TLS.ClientAuth.CAFile
			requireClient = l.TLS.ClientAuth.Mode == "require" || l.TLS.ClientAuth.Mode == "optional"
		}
		if tlsAny, err := buildDownstreamTLS(
			l.TLS.CertPath, l.TLS.KeyPath, caPath,
			l.TLS.MinVersion, l.TLS.MaxVersion,
			requireClient,
		); err == nil {
			filterChain.TransportSocket = &corev3.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: tlsAny},
			}
		}
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
		FilterChains: []*listenerv3.FilterChain{filterChain},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Retry policy
// ─────────────────────────────────────────────────────────────────────────────

func buildRetryPolicy(retry *model.RouteRetry) *routev3.RetryPolicy {
	policy := &routev3.RetryPolicy{
		NumRetries: wrapperspb.UInt32(retry.Attempts),
	}

	if retry.PerAttemptTimeout != "" {
		if d, err := parseDuration(retry.PerAttemptTimeout); err == nil {
			policy.PerTryTimeout = d
		}
	}

	retryOn := translateRetryConditions(retry.On)
	if retryOn != "" {
		policy.RetryOn = retryOn
	}

	if retry.Backoff != nil && retry.Backoff.Base != "" {
		if d, err := parseDuration(retry.Backoff.Base); err == nil {
			policy.RetryBackOff = &routev3.RetryPolicy_RetryBackOff{
				BaseInterval: d,
			}
			if retry.Backoff.Max != "" {
				if maxD, err := parseDuration(retry.Backoff.Max); err == nil {
					policy.RetryBackOff.MaxInterval = maxD
				}
			}
		}
	}

	return policy
}

func translateRetryConditions(conditions []model.RetryCondition) string {
	if len(conditions) == 0 {
		return "5xx,connect-failure"
	}
	mapping := map[model.RetryCondition]string{
		model.RetryOnServerError:       "5xx",
		model.RetryOnConnectionFailure: "connect-failure,reset",
		model.RetryOnGatewayError:      "gateway-error",
		model.RetryOnRetriableCodes:    "retriable-status-codes",
	}
	var parts []string
	for _, c := range conditions {
		if v, ok := mapping[c]; ok {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ",")
}

// ─────────────────────────────────────────────────────────────────────────────
// URL rewrite
// ─────────────────────────────────────────────────────────────────────────────

func applyRewrite(ra *routev3.RouteAction, rw *model.RouteRewrite) {
	if rw.Path != "" {
		ra.PrefixRewrite = rw.Path
	}

	if rw.PathRegex != nil {
		ra.RegexRewrite = &matcherv3.RegexMatchAndSubstitute{
			Pattern: &matcherv3.RegexMatcher{
				EngineType: &matcherv3.RegexMatcher_GoogleRe2{GoogleRe2: &matcherv3.RegexMatcher_GoogleRE2{}},
				Regex:      rw.PathRegex.Pattern,
			},
			Substitution: rw.PathRegex.Substitution,
		}
	}

	if rw.Host != "" {
		ra.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteLiteral{HostRewriteLiteral: rw.Host}
	} else if rw.HostFromHeader != "" {
		ra.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteHeader{HostRewriteHeader: rw.HostFromHeader}
	} else if rw.AutoHost {
		ra.HostRewriteSpecifier = &routev3.RouteAction_AutoHostRewrite{AutoHostRewrite: wrapperspb.Bool(true)}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Redirect
// ─────────────────────────────────────────────────────────────────────────────

func buildRedirectAction(rd *model.RouteRedirect) *routev3.RedirectAction {
	ra := &routev3.RedirectAction{}

	if rd.Scheme != "" {
		ra.SchemeRewriteSpecifier = &routev3.RedirectAction_SchemeRedirect{SchemeRedirect: rd.Scheme}
	}
	if rd.Host != "" {
		ra.HostRedirect = rd.Host
	}
	if rd.Path != "" {
		ra.PathRewriteSpecifier = &routev3.RedirectAction_PathRedirect{PathRedirect: rd.Path}
	}
	if rd.StripQuery {
		ra.StripQuery = true
	}

	switch rd.Code {
	case 301:
		ra.ResponseCode = routev3.RedirectAction_MOVED_PERMANENTLY
	case 302:
		ra.ResponseCode = routev3.RedirectAction_FOUND
	case 303:
		ra.ResponseCode = routev3.RedirectAction_SEE_OTHER
	case 307:
		ra.ResponseCode = routev3.RedirectAction_TEMPORARY_REDIRECT
	case 308:
		ra.ResponseCode = routev3.RedirectAction_PERMANENT_REDIRECT
	default:
		ra.ResponseCode = routev3.RedirectAction_MOVED_PERMANENTLY
	}

	return ra
}

// ─────────────────────────────────────────────────────────────────────────────
// Cluster helpers
// ─────────────────────────────────────────────────────────────────────────────

// clusterTypeFor derives the Envoy cluster type from the Destination fields.
func clusterTypeFor(d model.Destination) clusterv3.Cluster_DiscoveryType {
	if d.Options != nil && d.Options.Discovery != nil && d.Options.Discovery.Type == model.DiscoveryTypeKubernetes {
		return clusterv3.Cluster_EDS
	}
	if isIPAddress(d.Host) {
		return clusterv3.Cluster_STATIC
	}
	return clusterv3.Cluster_STRICT_DNS
}

// lbPolicyFor translates the Vrata EndpointBalancing algorithm to Envoy LB policy.
func lbPolicyFor(d model.Destination) clusterv3.Cluster_LbPolicy {
	if d.Options == nil || d.Options.EndpointBalancing == nil {
		return clusterv3.Cluster_ROUND_ROBIN
	}
	switch d.Options.EndpointBalancing.Algorithm {
	case model.EndpointLBRoundRobin:
		return clusterv3.Cluster_ROUND_ROBIN
	case model.EndpointLBLeastRequest:
		return clusterv3.Cluster_LEAST_REQUEST
	case model.EndpointLBRingHash:
		return clusterv3.Cluster_RING_HASH
	case model.EndpointLBMaglev:
		return clusterv3.Cluster_MAGLEV
	case model.EndpointLBRandom:
		return clusterv3.Cluster_RANDOM
	default:
		return clusterv3.Cluster_ROUND_ROBIN
	}
}

func isIPAddress(host string) bool {
	return net.ParseIP(host) != nil
}

// buildUpstreamTLS builds an Envoy UpstreamTlsContext for connecting to TLS upstreams.
func buildUpstreamTLS(tls *model.TLSOptions, defaultSNI string) (*anypb.Any, error) {
	tlsContext := &tlsv3.UpstreamTlsContext{
		CommonTlsContext: &tlsv3.CommonTlsContext{
			TlsParams: buildTLSParams(tls.MinVersion, tls.MaxVersion),
		},
	}

	sni := tls.SNI
	if sni == "" {
		sni = defaultSNI
	}
	tlsContext.Sni = sni

	if tls.CAFile != "" {
		tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_ValidationContext{
			ValidationContext: &tlsv3.CertificateValidationContext{
				TrustedCa: &corev3.DataSource{
					Specifier: &corev3.DataSource_Filename{Filename: tls.CAFile},
				},
			},
		}
	}

	if tls.Mode == model.TLSModeMTLS && tls.CertFile != "" && tls.KeyFile != "" {
		tlsContext.CommonTlsContext.TlsCertificates = []*tlsv3.TlsCertificate{
			{
				CertificateChain: &corev3.DataSource{
					Specifier: &corev3.DataSource_Filename{Filename: tls.CertFile},
				},
				PrivateKey: &corev3.DataSource{
					Specifier: &corev3.DataSource_Filename{Filename: tls.KeyFile},
				},
			},
		}
	}

	return anypb.New(tlsContext)
}

// buildHealthChecks translates Vrata HealthCheckOptions to Envoy health check config.
func buildHealthChecks(hc *model.HealthCheckOptions) []*corev3.HealthCheck {
	check := &corev3.HealthCheck{
		HealthChecker: &corev3.HealthCheck_HttpHealthCheck_{
			HttpHealthCheck: &corev3.HealthCheck_HttpHealthCheck{
				Path: hc.Path,
			},
		},
	}

	if hc.Interval != "" {
		if d, err := parseDuration(hc.Interval); err == nil {
			check.Interval = d
		}
	} else {
		check.Interval = durationpb(10)
	}

	if hc.Timeout != "" {
		if d, err := parseDuration(hc.Timeout); err == nil {
			check.Timeout = d
		}
	} else {
		check.Timeout = durationpb(5)
	}

	if hc.UnhealthyThreshold > 0 {
		check.UnhealthyThreshold = wrapperspb.UInt32(hc.UnhealthyThreshold)
	} else {
		check.UnhealthyThreshold = wrapperspb.UInt32(3)
	}

	if hc.HealthyThreshold > 0 {
		check.HealthyThreshold = wrapperspb.UInt32(hc.HealthyThreshold)
	} else {
		check.HealthyThreshold = wrapperspb.UInt32(2)
	}

	return []*corev3.HealthCheck{check}
}

// toResourceSlice converts a typed proto slice to []cachetypes.Resource.
func toResourceSlice[T cachetypes.Resource](in []T) []cachetypes.Resource {
	out := make([]cachetypes.Resource, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
