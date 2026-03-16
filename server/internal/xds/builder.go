// Package xds implements the Envoy xDS snapshot builder for Rutoso.
package xds

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/achetronic/rutoso/internal/model"
)

// BuildSnapshot converts listeners, filters, groups, routes, and destinations
// into a complete Envoy xDS Snapshot.
//
// Each model.Listener produces one Envoy Listener. Filters referenced by
// FilterIDs are resolved and their configs are wired into the HCM filter chain
// in declaration order (router filter is always appended last).
//
// When no listeners are stored, a default listener on 0.0.0.0:80 is generated
// automatically so the xDS server always emits a valid snapshot.
//
// One Cluster + ClusterLoadAssignment is generated per Destination. Routes
// reference Destinations by ID via BackendRef.
func BuildSnapshot(version string, modelListeners []model.Listener, modelFilters []model.Filter, groups []model.RouteGroup, routes []model.Route, destinations []model.Destination, edsCLAs map[string]*endpointv3.ClusterLoadAssignment) (*cachev3.Snapshot, error) {
	// Build lookup maps for O(1) resolution.
	routeByID := make(map[string]model.Route, len(routes))
	for _, r := range routes {
		routeByID[r.ID] = r
	}

	filterByID := make(map[string]model.Filter, len(modelFilters))
	for _, f := range modelFilters {
		filterByID[f.ID] = f
	}

	destByID := make(map[string]model.Destination, len(destinations))
	for _, d := range destinations {
		destByID[d.ID] = d
	}

	// Build one Envoy Cluster + ClusterLoadAssignment per Destination.
	// Clusters are keyed by Destination ID so routes can reference them directly.
	var (
		clusters  []types.Resource
		endpoints []types.Resource
		vhosts    []*routev3.VirtualHost
		seen      = make(map[string]bool) // avoids duplicate clusters
	)

	for _, d := range destinations {
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		clusters = append(clusters, buildClusterFromDestination(d))
		if isEDS(d) {
			// Use the CLA provided by the k8s watcher if available.
			if cla, ok := edsCLAs[d.ID]; ok {
				endpoints = append(endpoints, cla)
			}
		} else {
			endpoints = append(endpoints, buildEndpointFromDestination(d))
		}
	}

	for _, g := range groups {
		for _, routeID := range g.RouteIDs {
			r, ok := routeByID[routeID]
			if !ok {
				// Route referenced by group does not exist; skip gracefully.
				continue
			}
			vhosts = append(vhosts, buildVirtualHost(g, r))
		}
	}

	// Single RouteConfiguration with all virtual hosts.
	routeConfig := &routev3.RouteConfiguration{
		Name:         "rutoso_routes",
		VirtualHosts: vhosts,
	}

	var envoyListeners []types.Resource
	for _, ml := range modelListeners {
		el, err := buildListenerFromModel(ml, filterByID, routeConfig.Name)
		if err != nil {
			return nil, fmt.Errorf("building listener %q: %w", ml.ID, err)
		}
		envoyListeners = append(envoyListeners, el)
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
// including the match conditions and the appropriate action (forward to
// backends, redirect, or direct response).
func buildRouteAction(g model.RouteGroup, r model.Route) *routev3.Route {
	match := buildRouteMatch(g, r)

	envoyRoute := &routev3.Route{Match: match}

	switch {
	case r.DirectResponse != nil:
		envoyRoute.Action = buildDirectResponseAction(r)
	case r.Redirect != nil:
		envoyRoute.Action = buildRedirectAction(r)
	case r.Forward != nil:
		envoyRoute.Action = buildForwardAction(r)
	}

	return envoyRoute
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

// buildForwardAction creates a Route_Route action that forwards traffic to
// one or more upstream Destinations. It also wires timeouts, retry policy,
// URL rewriting, and request mirroring when configured on the ForwardAction.
func buildForwardAction(r model.Route) *routev3.Route_Route {
	f := r.Forward
	ra := &routev3.RouteAction{}

	if len(f.Backends) == 1 {
		ra.ClusterSpecifier = &routev3.RouteAction_Cluster{
			Cluster: f.Backends[0].DestinationID,
		}
	} else if len(f.Backends) > 1 {
		var wcs []*routev3.WeightedCluster_ClusterWeight
		for _, b := range f.Backends {
			wcs = append(wcs, &routev3.WeightedCluster_ClusterWeight{
				Name:   b.DestinationID,
				Weight: wrapperspb.UInt32(b.Weight),
			})
		}
		ra.ClusterSpecifier = &routev3.RouteAction_WeightedClusters{
			WeightedClusters: &routev3.WeightedCluster{
				Clusters: wcs,
			},
		}
	}

	applyTimeouts(ra, f.Timeouts)
	applyRetryPolicy(ra, f.Retry)
	applyRewrite(ra, f.Rewrite)
	applyMirror(ra, f.Mirror)

	return &routev3.Route_Route{Route: ra}
}

// buildRedirectAction creates a Route_Redirect action from the Route's
// Redirect config. When URL is set it is parsed and decomposed into its
// scheme, host, and path components; the individual fields are ignored.
func buildRedirectAction(r model.Route) *routev3.Route_Redirect {
	rd := r.Redirect
	ra := &routev3.RedirectAction{
		StripQuery: rd.StripQuery,
	}

	if rd.URL != "" {
		if u, err := url.Parse(rd.URL); err == nil {
			if u.Scheme != "" {
				ra.SchemeRewriteSpecifier = &routev3.RedirectAction_SchemeRedirect{
					SchemeRedirect: u.Scheme,
				}
			}
			if u.Host != "" {
				ra.HostRedirect = u.Host
			}
			if u.Path != "" {
				ra.PathRewriteSpecifier = &routev3.RedirectAction_PathRedirect{
					PathRedirect: u.Path,
				}
			}
		}
	} else {
		if rd.Host != "" {
			ra.HostRedirect = rd.Host
		}
		if rd.Path != "" {
			ra.PathRewriteSpecifier = &routev3.RedirectAction_PathRedirect{
				PathRedirect: rd.Path,
			}
		}
		if rd.Scheme != "" {
			ra.SchemeRewriteSpecifier = &routev3.RedirectAction_SchemeRedirect{
				SchemeRedirect: rd.Scheme,
			}
		}
	}

	switch rd.Code {
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

	return &routev3.Route_Redirect{Redirect: ra}
}

// buildDirectResponseAction creates a Route_DirectResponse action from the
// Route's DirectResponse config.
func buildDirectResponseAction(r model.Route) *routev3.Route_DirectResponse {
	dr := r.DirectResponse
	action := &routev3.DirectResponseAction{
		Status: dr.Status,
	}
	if dr.Body != "" {
		action.Body = &corev3.DataSource{
			Specifier: &corev3.DataSource_InlineString{
				InlineString: dr.Body,
			},
		}
	}
	return &routev3.Route_DirectResponse{DirectResponse: action}
}

// applyTimeouts sets the request and idle timeouts on the RouteAction.
func applyTimeouts(ra *routev3.RouteAction, t *model.RouteTimeouts) {
	if t == nil {
		return
	}
	if t.Request != "" {
		if dur, err := time.ParseDuration(t.Request); err == nil {
			ra.Timeout = durationpb.New(dur)
		}
	}
	if t.Idle != "" {
		if dur, err := time.ParseDuration(t.Idle); err == nil {
			ra.IdleTimeout = durationpb.New(dur)
		}
	}
}

// retryConditionMap translates semantic retry condition names into the
// comma-separated values Envoy expects in RetryPolicy.retry_on.
var retryConditionMap = map[model.RetryCondition]string{
	model.RetryOnServerError:       "5xx",
	model.RetryOnConnectionFailure: "connect-failure,reset",
	model.RetryOnGatewayError:      "gateway-error",
	model.RetryOnRetriableCodes:    "retriable-status-codes",
}

// applyRetryPolicy sets the retry policy on the RouteAction.
func applyRetryPolicy(ra *routev3.RouteAction, retry *model.RouteRetry) {
	if retry == nil {
		return
	}
	rp := &routev3.RetryPolicy{
		NumRetries: wrapperspb.UInt32(retry.Attempts),
	}

	var conditions []string
	for _, c := range retry.On {
		if envoyVal, ok := retryConditionMap[c]; ok {
			conditions = append(conditions, envoyVal)
		}
	}
	if len(conditions) > 0 {
		rp.RetryOn = strings.Join(conditions, ",")
	}

	if retry.PerAttemptTimeout != "" {
		if dur, err := time.ParseDuration(retry.PerAttemptTimeout); err == nil {
			rp.PerTryTimeout = durationpb.New(dur)
		}
	}

	if len(retry.RetriableCodes) > 0 {
		rp.RetriableStatusCodes = retry.RetriableCodes
	}

	if retry.Backoff != nil {
		bo := &routev3.RetryPolicy_RetryBackOff{}
		if retry.Backoff.Base != "" {
			if dur, err := time.ParseDuration(retry.Backoff.Base); err == nil {
				bo.BaseInterval = durationpb.New(dur)
			}
		}
		if retry.Backoff.Max != "" {
			if dur, err := time.ParseDuration(retry.Backoff.Max); err == nil {
				bo.MaxInterval = durationpb.New(dur)
			}
		}
		rp.RetryBackOff = bo
	}

	ra.RetryPolicy = rp
}

// applyRewrite sets prefix rewrite, regex rewrite, and host rewrite on the
// RouteAction.
func applyRewrite(ra *routev3.RouteAction, rw *model.RouteRewrite) {
	if rw == nil {
		return
	}

	switch {
	case rw.PathRegex != nil:
		ra.RegexRewrite = &matcherv3.RegexMatchAndSubstitute{
			Pattern:      &matcherv3.RegexMatcher{Regex: rw.PathRegex.Pattern},
			Substitution: rw.PathRegex.Substitution,
		}
	case rw.Path != "":
		ra.PrefixRewrite = rw.Path
	}

	switch {
	case rw.AutoHost:
		ra.HostRewriteSpecifier = &routev3.RouteAction_AutoHostRewrite{
			AutoHostRewrite: wrapperspb.Bool(true),
		}
	case rw.HostFromHeader != "":
		ra.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteHeader{
			HostRewriteHeader: rw.HostFromHeader,
		}
	case rw.Host != "":
		ra.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteLiteral{
			HostRewriteLiteral: rw.Host,
		}
	}
}

// applyMirror sets the request mirror policy on the RouteAction.
func applyMirror(ra *routev3.RouteAction, m *model.RouteMirror) {
	if m == nil {
		return
	}
	pct := m.Percentage
	if pct == 0 {
		pct = 100
	}
	ra.RequestMirrorPolicies = []*routev3.RouteAction_RequestMirrorPolicy{
		{
			Cluster: m.DestinationID,
			RuntimeFraction: &corev3.RuntimeFractionalPercent{
				DefaultValue: &typev3.FractionalPercent{
					Numerator:   pct,
					Denominator: typev3.FractionalPercent_HUNDRED,
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

// isEDS reports whether a Destination should use EDS (Kubernetes discovery).
func isEDS(d model.Destination) bool {
	return d.Options != nil &&
		d.Options.Discovery != nil &&
		d.Options.Discovery.Type == model.DiscoveryTypeKubernetes
}

// clusterTypeFor derives the Envoy cluster discovery type from the Destination.
// Rules:
//   - Kubernetes discovery  → EDS (endpoints pushed by EndpointSlice watcher)
//   - Host is a bare IP     → STATIC
//   - Host is an FQDN       → STRICT_DNS
func clusterTypeFor(d model.Destination) clusterv3.Cluster_DiscoveryType {
	if isEDS(d) {
		return clusterv3.Cluster_EDS
	}
	if net.ParseIP(d.Host) != nil {
		return clusterv3.Cluster_STATIC
	}
	return clusterv3.Cluster_STRICT_DNS
}

// connectTimeoutFor parses the user-supplied connect timeout string, falling
// back to 5 s when the field is empty or unparseable.
func connectTimeoutFor(d model.Destination) *durationpb.Duration {
	if d.Options != nil && d.Options.ConnectTimeout != "" {
		if dur, err := time.ParseDuration(d.Options.ConnectTimeout); err == nil {
			return durationpb.New(dur)
		}
	}
	return durationpb.New(5 * time.Second)
}

// lbPolicyFor maps the user-supplied algorithm string to an Envoy LbPolicy.
func lbPolicyFor(d model.Destination) clusterv3.Cluster_LbPolicy {
	if d.Options == nil || d.Options.Balancing == nil {
		return clusterv3.Cluster_ROUND_ROBIN
	}
	switch d.Options.Balancing.Algorithm {
	case model.LBPolicyLeastRequest:
		return clusterv3.Cluster_LEAST_REQUEST
	case model.LBPolicyRingHash:
		return clusterv3.Cluster_RING_HASH
	case model.LBPolicyMaglev:
		return clusterv3.Cluster_MAGLEV
	case model.LBPolicyRandom:
		return clusterv3.Cluster_RANDOM
	default:
		return clusterv3.Cluster_ROUND_ROBIN
	}
}

// buildClusterFromDestination converts a model.Destination into an Envoy Cluster.
// Cluster type is derived automatically (EDS / STATIC / STRICT_DNS).
// All optional sub-configs (TLS, circuit breakers, health checks, outlier
// detection, ring-hash / maglev) are applied only when the corresponding
// Options fields are populated.
func buildClusterFromDestination(d model.Destination) *clusterv3.Cluster {
	ctype := clusterTypeFor(d)
	lbPolicy := lbPolicyFor(d)

	c := &clusterv3.Cluster{
		Name:                 d.ID,
		ConnectTimeout:       connectTimeoutFor(d),
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: ctype},
		LbPolicy:             lbPolicy,
	}

	// EDS: delegate endpoint discovery to the xDS server (EndpointSlice watcher).
	if ctype == clusterv3.Cluster_EDS {
		c.EdsClusterConfig = &clusterv3.Cluster_EdsClusterConfig{
			EdsConfig: &corev3.ConfigSource{
				ConfigSourceSpecifier: &corev3.ConfigSource_Ads{},
			},
			ServiceName: d.ID,
		}
	}

	if d.Options == nil {
		return c
	}

	// Ring-hash / Maglev config.
	if d.Options.Balancing != nil {
		switch lbPolicy {
		case clusterv3.Cluster_RING_HASH:
			rhc := &clusterv3.Cluster_RingHashLbConfig{}
			if d.Options.Balancing.RingSize != nil {
				rhc.MinimumRingSize = wrapperspb.UInt64(d.Options.Balancing.RingSize.Min)
				rhc.MaximumRingSize = wrapperspb.UInt64(d.Options.Balancing.RingSize.Max)
				_ = rhc // suppress unused if only size is set
			}
			c.LbConfig = &clusterv3.Cluster_RingHashLbConfig_{RingHashLbConfig: rhc}
		case clusterv3.Cluster_MAGLEV:
			if d.Options.Balancing.MaglevTableSize > 0 {
				c.LbConfig = &clusterv3.Cluster_MaglevLbConfig_{
					MaglevLbConfig: &clusterv3.Cluster_MaglevLbConfig{
						TableSize: wrapperspb.UInt64(d.Options.Balancing.MaglevTableSize),
					},
				}
			}
		}
	}

	// Circuit breakers.
	if cb := d.Options.CircuitBreaker; cb != nil {
		c.CircuitBreakers = &clusterv3.CircuitBreakers{
			Thresholds: []*clusterv3.CircuitBreakers_Thresholds{
				{
					MaxConnections:     wrapperspb.UInt32(cb.MaxConnections),
					MaxPendingRequests: wrapperspb.UInt32(cb.MaxPendingRequests),
					MaxRequests:        wrapperspb.UInt32(cb.MaxRequests),
					MaxRetries:         wrapperspb.UInt32(cb.MaxRetries),
				},
			},
		}
	}

	// Health checks.
	if hc := d.Options.HealthCheck; hc != nil {
		interval := 10 * time.Second
		timeout := 5 * time.Second
		if hc.Interval != "" {
			if dur, err := time.ParseDuration(hc.Interval); err == nil {
				interval = dur
			}
		}
		if hc.Timeout != "" {
			if dur, err := time.ParseDuration(hc.Timeout); err == nil {
				timeout = dur
			}
		}
		c.HealthChecks = []*corev3.HealthCheck{
			{
				Interval: durationpb.New(interval),
				Timeout:  durationpb.New(timeout),
				UnhealthyThreshold: wrapperspb.UInt32(func() uint32 {
					if hc.UnhealthyThreshold > 0 {
						return hc.UnhealthyThreshold
					}
					return 3
				}()),
				HealthyThreshold: wrapperspb.UInt32(func() uint32 {
					if hc.HealthyThreshold > 0 {
						return hc.HealthyThreshold
					}
					return 2
				}()),
				HealthChecker: &corev3.HealthCheck_HttpHealthCheck_{
					HttpHealthCheck: &corev3.HealthCheck_HttpHealthCheck{
						Path: hc.Path,
					},
				},
			},
		}
	}

	// Outlier detection.
	if od := d.Options.OutlierDetection; od != nil {
		odc := &clusterv3.OutlierDetection{}
		if od.Consecutive5xx > 0 {
			odc.Consecutive_5Xx = wrapperspb.UInt32(od.Consecutive5xx)
		}
		if od.ConsecutiveGatewayErrors > 0 {
			odc.ConsecutiveGatewayFailure = wrapperspb.UInt32(od.ConsecutiveGatewayErrors)
		}
		if od.MaxEjectionPercent > 0 {
			odc.MaxEjectionPercent = wrapperspb.UInt32(od.MaxEjectionPercent)
		}
		if od.Interval != "" {
			if dur, err := time.ParseDuration(od.Interval); err == nil {
				odc.Interval = durationpb.New(dur)
			}
		}
		if od.BaseEjectionTime != "" {
			if dur, err := time.ParseDuration(od.BaseEjectionTime); err == nil {
				odc.BaseEjectionTime = durationpb.New(dur)
			}
		}
		c.OutlierDetection = odc
	}

	return c
}

// buildEndpointFromDestination creates a static ClusterLoadAssignment for
// STATIC and STRICT_DNS clusters. EDS clusters must NOT call this function —
// their endpoints are pushed dynamically by the EndpointSlice watcher.
func buildEndpointFromDestination(d model.Destination) *endpointv3.ClusterLoadAssignment {
	return &endpointv3.ClusterLoadAssignment{
		ClusterName: d.ID,
		Endpoints: []*endpointv3.LocalityLbEndpoints{
			{
				LbEndpoints: []*endpointv3.LbEndpoint{
					{
						HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
							Endpoint: &endpointv3.Endpoint{
								Address: &corev3.Address{
									Address: &corev3.Address_SocketAddress{
										SocketAddress: &corev3.SocketAddress{
											Address: d.Host,
											PortSpecifier: &corev3.SocketAddress_PortValue{
												PortValue: d.Port,
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
