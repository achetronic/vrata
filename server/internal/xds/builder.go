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
	accesslogcorev3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	upstreamhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
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
func BuildSnapshot(version string, modelListeners []model.Listener, modelMiddlewares []model.Middleware, groups []model.RouteGroup, routes []model.Route, destinations []model.Destination, edsCLAs map[string]*endpointv3.ClusterLoadAssignment) (*cachev3.Snapshot, error) {
	// Build lookup maps for O(1) resolution.
	routeByID := make(map[string]model.Route, len(routes))
	for _, r := range routes {
		routeByID[r.ID] = r
	}

	mwByID := make(map[string]model.Middleware, len(modelMiddlewares))
	for _, f := range modelMiddlewares {
		mwByID[f.ID] = f
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

	// Collect all middleware IDs referenced by routes and groups so the
	// builder can register them in every listener's HCM pipeline and
	// disable them per-route where not active.
	usedMwIDs := make(map[string]bool)
	routesInGroups := make(map[string]bool)
	for _, g := range groups {
		for _, id := range g.MiddlewareIDs {
			usedMwIDs[id] = true
		}
		for id := range g.MiddlewareOverrides {
			usedMwIDs[id] = true
		}
		for _, routeID := range g.RouteIDs {
			routesInGroups[routeID] = true
			if r, ok := routeByID[routeID]; ok {
				for _, id := range r.MiddlewareIDs {
					usedMwIDs[id] = true
				}
				for id := range r.MiddlewareOverrides {
					usedMwIDs[id] = true
				}
			}
		}
	}
	// Also collect middlewares from standalone routes (not in any group).
	for _, r := range routes {
		if routesInGroups[r.ID] {
			continue
		}
		for _, id := range r.MiddlewareIDs {
			usedMwIDs[id] = true
		}
		for id := range r.MiddlewareOverrides {
			usedMwIDs[id] = true
		}
	}

	for _, g := range groups {
		for _, routeID := range g.RouteIDs {
			r, ok := routeByID[routeID]
			if !ok {
				continue
			}
			vhosts = append(vhosts, buildVirtualHost(g, r, mwByID, usedMwIDs))
		}
	}

	// Standalone routes: not referenced by any group. Each gets its own
	// VirtualHost using the route's own hostnames (or "*" if none).
	for _, r := range routes {
		if routesInGroups[r.ID] {
			continue
		}
		vhosts = append(vhosts, buildStandaloneVirtualHost(r, mwByID, usedMwIDs))
	}

	// Single RouteConfiguration with all virtual hosts.
	routeConfig := &routev3.RouteConfiguration{
		Name:         "rutoso_routes",
		VirtualHosts: vhosts,
	}

	var envoyListeners []types.Resource
	for _, ml := range modelListeners {
		el, err := buildListenerFromModel(ml, mwByID, usedMwIDs, routeConfig.Name)
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
func buildVirtualHost(g model.RouteGroup, r model.Route, mwByID map[string]model.Middleware, usedMwIDs map[string]bool) *routev3.VirtualHost {
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

	route := buildRouteAction(g, r, mwByID, usedMwIDs)

	vh := &routev3.VirtualHost{
		Name:    fmt.Sprintf("%s__%s", g.ID, r.ID),
		Domains: domains,
		Routes:  []*routev3.Route{route},
	}

	// Group-level default retry policy.
	if g.RetryDefault != nil {
		vh.RetryPolicy = buildRetryPolicy(g.RetryDefault)
	}

	// Include x-envoy-attempt-count header.
	if g.IncludeAttemptCount {
		vh.IncludeRequestAttemptCount = true
	}

	return vh
}

// buildStandaloneVirtualHost creates an Envoy VirtualHost for a route that
// is not referenced by any group. Uses the route's own hostnames as domains
// (or "*" if none are defined). No group-level matchers are applied.
func buildStandaloneVirtualHost(r model.Route, mwByID map[string]model.Middleware, usedMwIDs map[string]bool) *routev3.VirtualHost {
	domains := r.Match.Hostnames
	if len(domains) == 0 {
		domains = []string{"*"}
	}

	// Use an empty group for the route action builder. The empty group
	// contributes no path prefix, no headers, no middlewares.
	emptyGroup := model.RouteGroup{}
	route := buildRouteAction(emptyGroup, r, mwByID, usedMwIDs)

	return &routev3.VirtualHost{
		Name:    fmt.Sprintf("standalone__%s", r.ID),
		Domains: domains,
		Routes:  []*routev3.Route{route},
	}
}

// buildRouteAction builds the Envoy Route message for a single Rutoso Route,
// including the match conditions and the appropriate action (forward to
// backends, redirect, or direct response).
func buildRouteAction(g model.RouteGroup, r model.Route, mwByID map[string]model.Middleware, usedMwIDs map[string]bool) *routev3.Route {
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

	// Wire per_filter_config from merged MiddlewareOverrides.
	if pfc := buildPerFilterConfig(g.MiddlewareIDs, r.MiddlewareIDs, g.MiddlewareOverrides, r.MiddlewareOverrides, usedMwIDs, mwByID); pfc != nil {
		envoyRoute.TypedPerFilterConfig = pfc
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

	// Method matching: Envoy uses a ":method" pseudo-header matcher.
	// Multiple methods are combined into a single regex so Envoy ORs them
	// (multiple HeaderMatchers on the same name would be ANDed, which is
	// impossible for methods).
	if len(r.Match.Methods) == 1 {
		rm.Headers = append(rm.Headers, &routev3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
				StringMatch: &matcherv3.StringMatcher{
					MatchPattern: &matcherv3.StringMatcher_Exact{Exact: r.Match.Methods[0]},
				},
			},
		})
	} else if len(r.Match.Methods) > 1 {
		pattern := strings.Join(r.Match.Methods, "|")
		rm.Headers = append(rm.Headers, &routev3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
				StringMatch: &matcherv3.StringMatcher{
					MatchPattern: &matcherv3.StringMatcher_SafeRegex{
						SafeRegex: &matcherv3.RegexMatcher{Regex: pattern},
					},
				},
			},
		})
	}

	// Query parameter matching.
	for _, qp := range r.Match.QueryParams {
		rm.QueryParameters = append(rm.QueryParameters, buildQueryParamMatcher(qp))
	}

	// gRPC-only matching.
	if r.Match.GRPC {
		rm.Grpc = &routev3.RouteMatch_GrpcRouteMatchOptions{}
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
	applyHashPolicy(ra, f.HashPolicy)

	// WebSocket upgrade.
	if f.Websocket {
		ra.UpgradeConfigs = append(ra.UpgradeConfigs, &routev3.RouteAction_UpgradeConfig{
			UpgradeType: "websocket",
		})
	}

	// Max gRPC timeout.
	if f.MaxGRPCTimeout != "" {
		if dur, err := time.ParseDuration(f.MaxGRPCTimeout); err == nil {
			ra.MaxGrpcTimeout = durationpb.New(dur)
		}
	}

	// Internal redirect policy.
	if ir := f.InternalRedirect; ir != nil {
		irp := &routev3.InternalRedirectPolicy{}
		if ir.MaxRedirects > 0 {
			irp.MaxInternalRedirects = wrapperspb.UInt32(ir.MaxRedirects)
		}
		if ir.AllowCrossScheme {
			irp.AllowCrossSchemeRedirect = true
		}
		for _, code := range ir.RedirectCodes {
			irp.RedirectResponseCodes = append(irp.RedirectResponseCodes, code)
		}
		ra.InternalRedirectPolicy = irp
	}

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
	ra.RetryPolicy = buildRetryPolicy(retry)
}

// buildRetryPolicy converts a model.RouteRetry into an Envoy RetryPolicy.
// Used by both per-route (RouteAction) and per-group (VirtualHost) retry config.
func buildRetryPolicy(retry *model.RouteRetry) *routev3.RetryPolicy {
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

	return rp
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

// applyHashPolicy sets the hash_policy entries on the RouteAction.
// Each model.HashPolicy is converted to an Envoy RouteAction_HashPolicy.
// Envoy evaluates entries in order and uses the first one that produces a value.
func applyHashPolicy(ra *routev3.RouteAction, policies []model.HashPolicy) {
	for _, hp := range policies {
		switch {
		case hp.Header != "":
			ra.HashPolicy = append(ra.HashPolicy, &routev3.RouteAction_HashPolicy{
				PolicySpecifier: &routev3.RouteAction_HashPolicy_Header_{
					Header: &routev3.RouteAction_HashPolicy_Header{
						HeaderName: hp.Header,
					},
				},
			})
		case hp.Cookie != "":
			cookie := &routev3.RouteAction_HashPolicy_Cookie{
				Name: hp.Cookie,
			}
			if hp.CookieTTL != "" {
				if dur, err := time.ParseDuration(hp.CookieTTL); err == nil {
					cookie.Ttl = durationpb.New(dur)
				}
			}
			ra.HashPolicy = append(ra.HashPolicy, &routev3.RouteAction_HashPolicy{
				PolicySpecifier: &routev3.RouteAction_HashPolicy_Cookie_{
					Cookie: cookie,
				},
			})
		case hp.SourceIP:
			ra.HashPolicy = append(ra.HashPolicy, &routev3.RouteAction_HashPolicy{
				PolicySpecifier: &routev3.RouteAction_HashPolicy_ConnectionProperties_{
					ConnectionProperties: &routev3.RouteAction_HashPolicy_ConnectionProperties{
						SourceIp: true,
					},
				},
			})
		}
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

// buildQueryParamMatcher converts a model.QueryParamMatcher into an Envoy
// QueryParameterMatcher.
func buildQueryParamMatcher(qp model.QueryParamMatcher) *routev3.QueryParameterMatcher {
	qpm := &routev3.QueryParameterMatcher{Name: qp.Name}
	if qp.Regex {
		qpm.QueryParameterMatchSpecifier = &routev3.QueryParameterMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_SafeRegex{
					SafeRegex: &matcherv3.RegexMatcher{Regex: qp.Value},
				},
			},
		}
	} else if qp.Value != "" {
		qpm.QueryParameterMatchSpecifier = &routev3.QueryParameterMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_Exact{Exact: qp.Value},
			},
		}
	}
	// When Value is empty and Regex is false, the matcher checks for parameter
	// presence only (any value).
	return qpm
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

	// HTTP/2 upstream protocol.
	if d.Options.HTTP2 {
		upstreamOpts := &upstreamhttpv3.HttpProtocolOptions{
			UpstreamProtocolOptions: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
						Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
					},
				},
			},
		}
		if upAny, err := anypb.New(upstreamOpts); err == nil {
			c.TypedExtensionProtocolOptions = map[string]*anypb.Any{
				"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": upAny,
			}
		}
	}

	// DNS settings (STRICT_DNS only).
	if d.Options.DNSRefreshRate != "" {
		if dur, err := time.ParseDuration(d.Options.DNSRefreshRate); err == nil {
			c.DnsRefreshRate = durationpb.New(dur)
		}
	}
	switch d.Options.DNSLookupFamily {
	case "V4_ONLY":
		c.DnsLookupFamily = clusterv3.Cluster_V4_ONLY
	case "V6_ONLY":
		c.DnsLookupFamily = clusterv3.Cluster_V6_ONLY
	case "AUTO", "":
		c.DnsLookupFamily = clusterv3.Cluster_AUTO
	}

	// Max requests per connection.
	if d.Options.MaxRequestsPerConnection > 0 {
		c.MaxRequestsPerConnection = wrapperspb.UInt32(d.Options.MaxRequestsPerConnection)
	}

	// Slow start.
	if ss := d.Options.SlowStart; ss != nil {
		ssc := &clusterv3.Cluster_SlowStartConfig{}
		if ss.Window != "" {
			if dur, err := time.ParseDuration(ss.Window); err == nil {
				ssc.SlowStartWindow = durationpb.New(dur)
			}
		}
		if ss.Aggression > 0 {
			ssc.Aggression = &corev3.RuntimeDouble{DefaultValue: ss.Aggression}
		}
		c.LbConfig = &clusterv3.Cluster_RoundRobinLbConfig_{
			RoundRobinLbConfig: &clusterv3.Cluster_RoundRobinLbConfig{
				SlowStartConfig: ssc,
			},
		}
	}

	// TLS upstream (UpstreamTlsContext as transport_socket).
	if tls := d.Options.TLS; tls != nil && tls.Mode != model.TLSModeNone && tls.Mode != "" {
		upstreamTLS := buildUpstreamTLSContext(tls, d.Host)
		if tlsAny, err := anypb.New(upstreamTLS); err == nil {
			c.TransportSocket = &corev3.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: tlsAny},
			}
		}
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
// Middlewares referenced by routes and groups (usedMwIDs) are resolved from
// mwByID and wired into the HCM pipeline. The router filter is always
// appended as the last HTTP filter.
// buildUpstreamTLSContext creates an UpstreamTlsContext from model.TLSOptions.
// Supports TLS (server cert verification) and mTLS (client cert presentation).
func buildUpstreamTLSContext(tls *model.TLSOptions, host string) *tlsv3.UpstreamTlsContext {
	ctx := &tlsv3.UpstreamTlsContext{}
	common := &tlsv3.CommonTlsContext{}

	// SNI: use explicit value or fall back to the upstream host.
	if tls.SNI != "" {
		ctx.Sni = tls.SNI
	} else {
		ctx.Sni = host
	}

	// TLS version constraints.
	if tls.MinVersion != "" || tls.MaxVersion != "" {
		params := &tlsv3.TlsParameters{}
		params.TlsMinimumProtocolVersion = parseTLSVersion(tls.MinVersion)
		params.TlsMaximumProtocolVersion = parseTLSVersion(tls.MaxVersion)
		common.TlsParams = params
	}

	// CA for server certificate validation.
	// When no CA file is specified in TLS mode, default to the system CA bundle
	// so connections to services with public certificates (Let's Encrypt, etc.)
	// work out of the box without extra configuration.
	caFile := tls.CAFile
	if caFile == "" {
		caFile = "/etc/ssl/certs/ca-certificates.crt"
	}
	common.ValidationContextType = &tlsv3.CommonTlsContext_ValidationContext{
		ValidationContext: &tlsv3.CertificateValidationContext{
			TrustedCa: &corev3.DataSource{
				Specifier: &corev3.DataSource_Filename{Filename: caFile},
			},
		},
	}

	// Client certificate for mTLS.
	if tls.Mode == model.TLSModeMTLS && tls.CertFile != "" && tls.KeyFile != "" {
		common.TlsCertificates = []*tlsv3.TlsCertificate{
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

	ctx.CommonTlsContext = common
	return ctx
}

// parseTLSVersion converts a user-facing TLS version string to the Envoy enum.
func parseTLSVersion(v string) tlsv3.TlsParameters_TlsProtocol {
	switch v {
	case "TLSv1_0":
		return tlsv3.TlsParameters_TLSv1_0
	case "TLSv1_1":
		return tlsv3.TlsParameters_TLSv1_1
	case "TLSv1_2":
		return tlsv3.TlsParameters_TLSv1_2
	case "TLSv1_3":
		return tlsv3.TlsParameters_TLSv1_3
	default:
		return tlsv3.TlsParameters_TLS_AUTO
	}
}

func buildListenerFromModel(ml model.Listener, mwByID map[string]model.Middleware, usedMwIDs map[string]bool, routeConfigName string) (*listenerv3.Listener, error) {
	router, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, fmt.Errorf("marshalling router filter: %w", err)
	}

	// Build HTTP filters from all middlewares used across routes/groups.
	var resolved []*hcmv3.HttpFilter
	for mwID := range usedMwIDs {
		mw, ok := mwByID[mwID]
		if !ok {
			continue
		}
		hf, err := buildHTTPFilter(mw)
		if err != nil {
			return nil, fmt.Errorf("building middleware %q: %w", mwID, err)
		}
		if hf != nil {
			resolved = append(resolved, hf)
		}
	}
	// Append router filter last.
	resolved = append(resolved, &hcmv3.HttpFilter{
		Name:       "envoy.filters.http.router",
		ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: router},
	})

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
		HttpFilters: resolved,
	}

	// HTTP/2 support (required for gRPC clients).
	if ml.HTTP2 {
		hcm.CodecType = hcmv3.HttpConnectionManager_AUTO
	}

	// Server name header.
	if ml.ServerName != "" {
		hcm.ServerName = ml.ServerName
	}

	// Max request headers size.
	if ml.MaxRequestHeadersKB > 0 {
		hcm.MaxRequestHeadersKb = wrapperspb.UInt32(ml.MaxRequestHeadersKB)
	}

	// Access log.
	if al := ml.AccessLog; al != nil {
		accessLogConfig := &accesslogv3.FileAccessLog{
			Path: al.Path,
		}
		// Format: JSON or text template.
		if al.JSON {
			if al.Format != "" {
				accessLogConfig.AccessLogFormat = &accesslogv3.FileAccessLog_LogFormat{
					LogFormat: &corev3.SubstitutionFormatString{
						Format: &corev3.SubstitutionFormatString_TextFormat{
							TextFormat: al.Format,
						},
					},
				}
			}
		} else if al.Format != "" {
			accessLogConfig.AccessLogFormat = &accesslogv3.FileAccessLog_LogFormat{
				LogFormat: &corev3.SubstitutionFormatString{
					Format: &corev3.SubstitutionFormatString_TextFormat{
						TextFormat: al.Format,
					},
				},
			}
		}
		alAny, err := anypb.New(accessLogConfig)
		if err == nil {
			hcm.AccessLog = []*accesslogcorev3.AccessLog{
				{
					Name:       "envoy.access_loggers.file",
					ConfigType: &accesslogcorev3.AccessLog_TypedConfig{TypedConfig: alAny},
				},
			}
		}
	}

	hcmAny, err := anypb.New(hcm)
	if err != nil {
		return nil, fmt.Errorf("marshalling HCM to Any: %w", err)
	}

	l := &listenerv3.Listener{
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
	}

	// TCP-level listener filters.
	for _, lf := range ml.ListenerFilters {
		switch lf.Type {
		case model.ListenerFilterTLSInspector:
			l.ListenerFilters = append(l.ListenerFilters, &listenerv3.ListenerFilter{
				Name: "envoy.filters.listener.tls_inspector",
			})
		case model.ListenerFilterProxyProtocol:
			l.ListenerFilters = append(l.ListenerFilters, &listenerv3.ListenerFilter{
				Name: "envoy.filters.listener.proxy_protocol",
			})
		case model.ListenerFilterOriginalDst:
			l.ListenerFilters = append(l.ListenerFilters, &listenerv3.ListenerFilter{
				Name: "envoy.filters.listener.original_dst",
			})
		}
	}

	return l, nil
}

// buildHTTPFilter converts a model.Middleware into an Envoy HttpFilter proto.
// Delegates to buildHTTPFilterReal in filters.go for the actual typed config.
func buildHTTPFilter(f model.Middleware) (*hcmv3.HttpFilter, error) {
	return buildHTTPFilterReal(f)
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
