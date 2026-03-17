// Package model defines the core domain types used throughout Rutoso.
// These are pure data structures with no I/O or business logic.
package model

// RouteGroup is a logical grouping of routes referenced by their IDs.
//
// A group acts as a first-level context layer applied on top of every
// referenced route. Path matching is composed according to these rules:
//
//   - PathPrefix (group) + any path specifier (route) → literal concatenation.
//     The group prefix is prepended to the route's exact path, prefix, or
//     regex pattern as-is. Use this when the group namespace is a simple
//     path segment (e.g. "/api/v1").
//
//   - PathRegex (group) + PathRegex (route) → pattern composition.
//     The two patterns are combined as (?:group_regex)(?:route_regex).
//     Both must be valid RE2 expressions. Use this when the group itself
//     defines a variable namespace (e.g. "/api/v[0-9]+").
//
//   - PathRegex (group) + Path or PathPrefix (route) → safe composition.
//     The route's literal value is escaped with regexp.QuoteMeta before
//     being appended to the group regex: (?:group_regex)(?:escaped_literal).
//     This lets you mix a flexible group namespace with fixed route paths
//     without accidentally breaking the regex.
//
//   - PathRegex (group) + no path specifier (route) → group regex is the
//     full match. The route contributes only backends and other matchers.
//
// In all cases, Hostnames are merged (union) and Headers are appended.
type RouteGroup struct {
	// ID is the unique identifier of the group.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the group.
	Name string `json:"name" yaml:"name"`

	// RouteIDs lists the IDs of the routes that belong to this group.
	// Routes are independent entities and may be referenced by multiple groups.
	RouteIDs []string `json:"routeIds" yaml:"routeIds"`

	// PathPrefix is prepended literally to the path/pathPrefix/pathRegex of
	// every referenced route. Mutually exclusive with PathRegex.
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`

	// PathRegex is a RE2 regular expression that defines the group's path
	// namespace. It is composed with the route's own path specifier according
	// to the rules described above. Mutually exclusive with PathPrefix.
	PathRegex string `json:"pathRegex,omitempty" yaml:"pathRegex,omitempty"`

	// Hostnames are merged (union) with each referenced route's own hostnames.
	// An empty slice means only the route's own hostnames apply.
	Hostnames []string `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`

	// Headers are appended to each referenced route's own header matchers.
	Headers []HeaderMatcher `json:"headers,omitempty" yaml:"headers,omitempty"`

	// MiddlewareIDs lists the IDs of Middleware entities active on all routes
	// in this group. The builder activates them
	// and enables them for every route in the group.
	MiddlewareIDs []string `json:"middlewareIds,omitempty" yaml:"middlewareIds,omitempty"`

	// MiddlewareOverrides carries per-group overrides for active middlewares.
	// The map key is the Middleware ID. These overrides apply to all routes
	// in the group. If a route also carries an override for the same
	// middleware, the route override wins entirely.
	MiddlewareOverrides map[string]MiddlewareOverride `json:"middlewareOverrides,omitempty" yaml:"middlewareOverrides,omitempty"`

	// RetryDefault is the default retry policy applied to all routes in this
	// group. Individual routes can override this by setting their own
	// ForwardAction.Retry. Maps to VirtualHost.retry_policy.
	RetryDefault *RouteRetry `json:"retryDefault,omitempty" yaml:"retryDefault,omitempty"`

	// IncludeAttemptCount makes Rutoso add the X-Request-Attempt-Count header
	// to upstream requests, indicating how many times the request has been
	// attempted (including the original). Maps to
	// VirtualHost.include_request_attempt_count.
	IncludeAttemptCount bool `json:"includeAttemptCount,omitempty" yaml:"includeAttemptCount,omitempty"`
}

// HeaderMatcher describes a condition on a single HTTP request header.
type HeaderMatcher struct {
	// Name is the header name (case-insensitive).
	Name string `json:"name" yaml:"name"`

	// Value is the exact value the header must have.
	// If empty, only the presence of the header is checked.
	Value string `json:"value,omitempty" yaml:"value,omitempty"`

	// Regex indicates that Value should be treated as a regular expression.
	Regex bool `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// QueryParamMatcher describes a condition on a single HTTP query parameter.
type QueryParamMatcher struct {
	// Name is the query parameter name.
	Name string `json:"name" yaml:"name"`

	// Value is the exact value the parameter must have.
	Value string `json:"value,omitempty" yaml:"value,omitempty"`

	// Regex indicates that Value should be treated as a regular expression.
	Regex bool `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// MatchRule defines all the conditions a request must satisfy to be matched by a Route.
// At most one of Path, PathPrefix, or PathRegex should be set.
type MatchRule struct {
	// Path is the exact path that must match.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// PathPrefix is a prefix that the request path must start with.
	// Mutually exclusive with Path and PathRegex.
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`

	// PathRegex is a regular expression the request path must match.
	// Mutually exclusive with Path and PathPrefix.
	PathRegex string `json:"pathRegex,omitempty" yaml:"pathRegex,omitempty"`

	// Methods lists the HTTP methods this rule applies to.
	// An empty slice matches all methods.
	Methods []string `json:"methods,omitempty" yaml:"methods,omitempty"`

	// Hostnames restricts the match to specific virtual host names.
	// An empty slice matches all virtual hosts.
	Hostnames []string `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`

	// Headers are request header matchers that must all match.
	Headers []HeaderMatcher `json:"headers,omitempty" yaml:"headers,omitempty"`

	// QueryParams are query parameter matchers that must all match.
	QueryParams []QueryParamMatcher `json:"queryParams,omitempty" yaml:"queryParams,omitempty"`

	// GRPC restricts this match to gRPC requests only (content-type
	// application/grpc). Maps to RouteMatch.grpc.
	GRPC bool `json:"grpc,omitempty" yaml:"grpc,omitempty"`
}
