// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package mapper translates Gateway API resources into Vrata API entities.
// All functions are pure (no I/O, no side effects) and fully testable.
package mapper

import (
	"fmt"
	"strings"

	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

// HTTPRouteInput holds the fields extracted from a Gateway API HTTPRoute
// that the mapper needs. This decouples the mapper from the concrete
// Gateway API types so it can handle both HTTPRoute and SuperHTTPRoute.
type HTTPRouteInput struct {
	Name      string
	Namespace string
	Hostnames []string
	Rules     []RuleInput
}

// RuleInput holds a single rule from an HTTPRoute.
type RuleInput struct {
	Matches    []MatchInput
	BackendRefs []BackendRefInput
	Filters    []FilterInput
}

// MatchInput holds a single match within a rule.
type MatchInput struct {
	PathType  string // "PathPrefix", "Exact", "RegularExpression"
	PathValue string
	Method    string
	Headers   []HeaderMatchInput
}

// HeaderMatchInput holds a single header match.
type HeaderMatchInput struct {
	Name  string
	Value string
	Type  string // "Exact" or "RegularExpression"
}

// BackendRefInput holds a backend reference.
type BackendRefInput struct {
	ServiceName      string
	ServiceNamespace string
	Port             uint32
	Weight           uint32
}

// FilterInput holds a filter from a rule.
type FilterInput struct {
	Type string // "RequestRedirect", "URLRewrite", "RequestHeaderModifier", "ResponseHeaderModifier"

	// RequestRedirect fields.
	RedirectScheme      string
	RedirectHost        string
	RedirectPort        uint32
	RedirectPath        string
	RedirectPathPrefix  string
	RedirectCode        uint32
	RedirectStripQuery  bool

	// URLRewrite fields.
	RewritePathPrefix string
	RewriteFullPath   string
	RewriteHostname   string

	// RequestHeaderModifier / ResponseHeaderModifier fields.
	HeadersToAdd    []HeaderValue
	HeadersToRemove []string

	// ResponseHeaderModifier fields.
	ResponseHeadersToAdd    []HeaderValue
	ResponseHeadersToRemove []string
}

// HeaderValue is a key-value pair for header manipulation.
type HeaderValue struct {
	Name  string
	Value string
}

// GatewayInput holds the fields extracted from a Gateway resource.
type GatewayInput struct {
	Name      string
	Namespace string
	Listeners []GatewayListenerInput
}

// GatewayListenerInput holds a single listener from a Gateway.
type GatewayListenerInput struct {
	Name     string
	Port     uint32
	Protocol string // "HTTP", "HTTPS", "GRPC", "GRPCS"
	Hostname string
}

// GRPCRouteInput holds the fields extracted from a Gateway API GRPCRoute.
type GRPCRouteInput struct {
	Name      string
	Namespace string
	Hostnames []string
	Rules     []GRPCRuleInput
}

// GRPCRuleInput holds a single rule from a GRPCRoute.
type GRPCRuleInput struct {
	Matches     []GRPCMatchInput
	BackendRefs []BackendRefInput
	Filters     []FilterInput
}

// GRPCMatchInput holds a single match within a GRPCRoute rule.
type GRPCMatchInput struct {
	ServiceName string // e.g. "mypackage.MyService"
	MethodName  string // e.g. "GetItem"
	MatchType   string // "Exact" or "RegularExpression"
	Headers     []HeaderMatchInput
}

// MappedEntities holds all Vrata entities produced from a single HTTPRoute.
type MappedEntities struct {
	Group        vrata.RouteGroup
	Routes       []vrata.Route
	Destinations []DestinationKey
	Middlewares  []vrata.Middleware
}

// DestinationKey uniquely identifies a Destination by Service coordinates.
// Used for deduplication across HTTPRoutes.
type DestinationKey struct {
	Name      string
	Namespace string
	Port      uint32
}

// DestinationName returns the ownership-tagged name for a Destination.
func (dk DestinationKey) DestinationName() string {
	return fmt.Sprintf("k8s:%s/%s:%d", dk.Namespace, dk.Name, dk.Port)
}

// FQDN returns the Kubernetes Service FQDN for this destination.
func (dk DestinationKey) FQDN() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", dk.Name, dk.Namespace)
}

// MapHTTPRoute translates an HTTPRoute into Vrata entities.
func MapHTTPRoute(input HTTPRouteInput) MappedEntities {
	prefix := fmt.Sprintf("k8s:%s/%s", input.Namespace, input.Name)

	var routes []vrata.Route
	var allDests []DestinationKey
	var allMiddlewares []vrata.Middleware

	for ri, rule := range input.Rules {
		// Collect destinations from backendRefs.
		var destRefs []map[string]any
		for _, br := range rule.BackendRefs {
			dk := DestinationKey{Name: br.ServiceName, Namespace: br.ServiceNamespace, Port: br.Port}
			allDests = append(allDests, dk)

			weight := br.Weight
			if weight == 0 {
				weight = 1
			}
			destRefs = append(destRefs, map[string]any{
				"destinationId": dk.DestinationName(),
				"weight":        weight,
			})
		}

		// Check for redirect filter (overrides forward).
		redirectFilter := findFilter(rule.Filters, "RequestRedirect")
		rewriteFilter := findFilter(rule.Filters, "URLRewrite")
		headerFilter := findFilter(rule.Filters, "RequestHeaderModifier")
		respHeaderFilter := findFilter(rule.Filters, "ResponseHeaderModifier")

		var middlewareIDs []string
		if headerFilter != nil {
			mwName := fmt.Sprintf("%s/rule-%d/headers", prefix, ri)
			mw := mapHeaderModifierFilter(mwName, headerFilter)
			allMiddlewares = append(allMiddlewares, mw)
			middlewareIDs = append(middlewareIDs, mwName)
		}
		if respHeaderFilter != nil {
			mwName := fmt.Sprintf("%s/rule-%d/resp-headers", prefix, ri)
			mw := mapResponseHeaderModifierFilter(mwName, respHeaderFilter)
			allMiddlewares = append(allMiddlewares, mw)
			middlewareIDs = append(middlewareIDs, mwName)
		}

		// One Route per match in the rule.
		matches := rule.Matches
		if len(matches) == 0 {
			matches = []MatchInput{{PathType: "PathPrefix", PathValue: "/"}}
		}

		for mi, m := range matches {
			routeName := fmt.Sprintf("%s/rule-%d/match-%d", prefix, ri, mi)
			route := vrata.Route{
				Name:          routeName,
				Match:         mapMatch(m),
				MiddlewareIDs: middlewareIDs,
			}

			if redirectFilter != nil {
				route.Redirect = mapRedirectFilter(redirectFilter)
			} else if len(destRefs) > 0 {
				fwd := map[string]any{
					"destinations": destRefs,
				}
				if rewriteFilter != nil {
					fwd["rewrite"] = mapRewriteFilter(rewriteFilter)
				}
				route.Forward = fwd
			}

			routes = append(routes, route)
		}
	}

	// Deduplicate destinations.
	allDests = deduplicateDests(allDests)

	// Build the group.
	routeNames := make([]string, len(routes))
	for i, r := range routes {
		routeNames[i] = r.Name
	}

	group := vrata.RouteGroup{
		Name:      prefix,
		RouteIDs:  routeNames,
		Hostnames: input.Hostnames,
	}

	return MappedEntities{
		Group:        group,
		Routes:       routes,
		Destinations: allDests,
		Middlewares:  allMiddlewares,
	}
}

// MapGateway translates a Gateway into Vrata Listeners.
func MapGateway(input GatewayInput) []vrata.Listener {
	var listeners []vrata.Listener
	for _, l := range input.Listeners {
		name := fmt.Sprintf("k8s:%s/%s/%s", input.Namespace, input.Name, l.Name)
		listener := vrata.Listener{
			Name:    name,
			Address: "0.0.0.0",
			Port:    l.Port,
		}
		switch l.Protocol {
		case "HTTPS", "GRPCS", "TLS":
			listener.TLS = map[string]any{"enabled": true}
		}
		listeners = append(listeners, listener)
	}
	return listeners
}

// GatewayListenerProtocolSupported returns true if the protocol is one the
// controller can handle.
func GatewayListenerProtocolSupported(protocol string) bool {
	switch protocol {
	case "HTTP", "HTTPS", "GRPC", "GRPCS":
		return true
	}
	return false
}

// MapGRPCRoute translates a GRPCRoute into Vrata entities.
// gRPC service/method matches are converted to path-based matchers with the
// grpc flag set, since gRPC always uses /{service}/{method} URL paths.
func MapGRPCRoute(input GRPCRouteInput) MappedEntities {
	prefix := fmt.Sprintf("k8s:%s/%s", input.Namespace, input.Name)

	var routes []vrata.Route
	var allDests []DestinationKey
	var allMiddlewares []vrata.Middleware

	for ri, rule := range input.Rules {
		var destRefs []map[string]any
		for _, br := range rule.BackendRefs {
			dk := DestinationKey{Name: br.ServiceName, Namespace: br.ServiceNamespace, Port: br.Port}
			allDests = append(allDests, dk)

			weight := br.Weight
			if weight == 0 {
				weight = 1
			}
			destRefs = append(destRefs, map[string]any{
				"destinationId": dk.DestinationName(),
				"weight":        weight,
			})
		}

		headerFilter := findFilter(rule.Filters, "RequestHeaderModifier")
		respHeaderFilter := findFilter(rule.Filters, "ResponseHeaderModifier")
		var middlewareIDs []string
		if headerFilter != nil {
			mwName := fmt.Sprintf("%s/rule-%d/headers", prefix, ri)
			mw := mapHeaderModifierFilter(mwName, headerFilter)
			allMiddlewares = append(allMiddlewares, mw)
			middlewareIDs = append(middlewareIDs, mwName)
		}
		if respHeaderFilter != nil {
			mwName := fmt.Sprintf("%s/rule-%d/resp-headers", prefix, ri)
			mw := mapResponseHeaderModifierFilter(mwName, respHeaderFilter)
			allMiddlewares = append(allMiddlewares, mw)
			middlewareIDs = append(middlewareIDs, mwName)
		}

		matches := rule.Matches
		if len(matches) == 0 {
			matches = []GRPCMatchInput{{}}
		}

		for mi, m := range matches {
			routeName := fmt.Sprintf("%s/rule-%d/match-%d", prefix, ri, mi)
			route := vrata.Route{
				Name:          routeName,
				Match:         mapGRPCMatch(m),
				MiddlewareIDs: middlewareIDs,
			}

			if len(destRefs) > 0 {
				route.Forward = map[string]any{
					"destinations": destRefs,
				}
			}

			routes = append(routes, route)
		}
	}

	allDests = deduplicateDests(allDests)

	routeNames := make([]string, len(routes))
	for i, r := range routes {
		routeNames[i] = r.Name
	}

	group := vrata.RouteGroup{
		Name:      prefix,
		RouteIDs:  routeNames,
		Hostnames: input.Hostnames,
	}

	return MappedEntities{
		Group:        group,
		Routes:       routes,
		Destinations: allDests,
		Middlewares:  allMiddlewares,
	}
}

// mapGRPCMatch converts a GRPCMatchInput to a Vrata match map.
// gRPC service/method are mapped to HTTP path matching with the grpc flag set.
func mapGRPCMatch(m GRPCMatchInput) map[string]any {
	match := map[string]any{"grpc": true}

	path := grpcMethodPath(m.ServiceName, m.MethodName)

	switch m.MatchType {
	case "RegularExpression":
		match["pathRegex"] = path
	case "Exact":
		if m.ServiceName == "" && m.MethodName == "" {
			match["pathPrefix"] = "/"
		} else if m.MethodName == "" {
			match["pathPrefix"] = "/" + m.ServiceName + "/"
		} else {
			match["path"] = path
		}
	default:
		if m.ServiceName == "" && m.MethodName == "" {
			match["pathPrefix"] = "/"
		} else if m.MethodName == "" {
			match["pathPrefix"] = "/" + m.ServiceName + "/"
		} else {
			match["path"] = path
		}
	}

	if len(m.Headers) > 0 {
		var headers []map[string]any
		for _, h := range m.Headers {
			hm := map[string]any{"name": h.Name, "value": h.Value}
			if h.Type == "RegularExpression" {
				hm["regex"] = true
			}
			headers = append(headers, hm)
		}
		match["headers"] = headers
	}

	match["methods"] = []string{"POST"}

	return match
}

// grpcMethodPath builds the HTTP/2 path for a gRPC service and method.
// If both are empty, returns "/". If only service is set, returns "/{service}/".
// Otherwise returns "/{service}/{method}".
func grpcMethodPath(service, method string) string {
	if service == "" && method == "" {
		return "/"
	}
	if method == "" {
		return "/" + service + "/"
	}
	return "/" + service + "/" + method
}

// mapMatch converts a MatchInput to a Vrata match map.
func mapMatch(m MatchInput) map[string]any {
	match := make(map[string]any)
	switch m.PathType {
	case "Exact":
		match["path"] = m.PathValue
	case "RegularExpression":
		match["pathRegex"] = m.PathValue
	default:
		match["pathPrefix"] = m.PathValue
	}
	if m.Method != "" {
		match["methods"] = []string{m.Method}
	}
	if len(m.Headers) > 0 {
		var headers []map[string]any
		for _, h := range m.Headers {
			hm := map[string]any{"name": h.Name, "value": h.Value}
			if h.Type == "RegularExpression" {
				hm["regex"] = true
			}
			headers = append(headers, hm)
		}
		match["headers"] = headers
	}
	return match
}

// mapRedirectFilter converts a redirect filter to a Vrata redirect map.
func mapRedirectFilter(f *FilterInput) map[string]any {
	rd := make(map[string]any)
	if f.RedirectScheme != "" {
		rd["scheme"] = f.RedirectScheme
	}
	if f.RedirectHost != "" {
		rd["host"] = f.RedirectHost
	}
	if f.RedirectPath != "" {
		rd["path"] = f.RedirectPath
	}
	if f.RedirectPathPrefix != "" {
		rd["prefixPath"] = f.RedirectPathPrefix
	}
	if f.RedirectPort > 0 {
		rd["port"] = f.RedirectPort
	}
	if f.RedirectCode > 0 {
		rd["code"] = f.RedirectCode
	}
	if f.RedirectStripQuery {
		rd["stripQuery"] = true
	}
	return rd
}

// mapRewriteFilter converts a URL rewrite filter to a Vrata rewrite map.
func mapRewriteFilter(f *FilterInput) map[string]any {
	rw := make(map[string]any)
	if f.RewritePathPrefix != "" {
		rw["path"] = f.RewritePathPrefix
	}
	if f.RewriteFullPath != "" {
		rw["fullPath"] = f.RewriteFullPath
	}
	if f.RewriteHostname != "" {
		rw["host"] = f.RewriteHostname
	}
	return rw
}

// mapHeaderModifierFilter converts a header modifier filter to a Vrata Middleware.
func mapHeaderModifierFilter(name string, f *FilterInput) vrata.Middleware {
	headers := make(map[string]any)
	if len(f.HeadersToAdd) > 0 {
		var add []map[string]any
		for _, h := range f.HeadersToAdd {
			add = append(add, map[string]any{"key": h.Name, "value": h.Value})
		}
		headers["requestHeadersToAdd"] = add
	}
	if len(f.HeadersToRemove) > 0 {
		headers["requestHeadersToRemove"] = f.HeadersToRemove
	}
	return vrata.Middleware{
		Name:    name,
		Type:    "headers",
		Headers: headers,
	}
}

// mapResponseHeaderModifierFilter converts a response header modifier filter to a Vrata Middleware.
func mapResponseHeaderModifierFilter(name string, f *FilterInput) vrata.Middleware {
	headers := make(map[string]any)
	if len(f.ResponseHeadersToAdd) > 0 {
		var add []map[string]any
		for _, h := range f.ResponseHeadersToAdd {
			add = append(add, map[string]any{"key": h.Name, "value": h.Value})
		}
		headers["responseHeadersToAdd"] = add
	}
	if len(f.ResponseHeadersToRemove) > 0 {
		headers["responseHeadersToRemove"] = f.ResponseHeadersToRemove
	}
	return vrata.Middleware{
		Name:    name,
		Type:    "headers",
		Headers: headers,
	}
}

// findFilter returns the first filter of the given type, or nil.
func findFilter(filters []FilterInput, filterType string) *FilterInput {
	for i := range filters {
		if filters[i].Type == filterType {
			return &filters[i]
		}
	}
	return nil
}

// deduplicateDests removes duplicate DestinationKeys by name.
func deduplicateDests(dests []DestinationKey) []DestinationKey {
	seen := make(map[string]bool)
	var out []DestinationKey
	for _, d := range dests {
		key := d.DestinationName()
		if !seen[key] {
			seen[key] = true
			out = append(out, d)
		}
	}
	return out
}

// IsOwned returns true if the entity name starts with the controller
// ownership prefix.
func IsOwned(name string) bool {
	return strings.HasPrefix(name, "k8s:")
}
