// Package model defines the core domain types used throughout Rutoso.
// These are pure data structures with no I/O or business logic.
package model

// RouteGroup is a logical grouping of routes referenced by their IDs.
// It can optionally add extra matching constraints on top of all referenced routes:
//   - PathPrefix is prepended to each route's path/pathPrefix.
//   - Hostnames are merged (union) with each route's own hostnames.
//   - Headers are appended to each route's own header matchers.
type RouteGroup struct {
	// ID is the unique identifier of the group.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the group.
	Name string `json:"name" yaml:"name"`

	// RouteIDs lists the IDs of the routes that belong to this group.
	// Routes are independent entities and may be referenced by multiple groups.
	RouteIDs []string `json:"routeIds" yaml:"routeIds"`

	// PathPrefix is prepended to the path/pathPrefix of every referenced route.
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`

	// Hostnames are merged (union) with each referenced route's own hostnames.
	// An empty slice means only the route's own hostnames apply.
	Hostnames []string `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`

	// Headers are appended to each referenced route's own header matchers.
	Headers []HeaderMatcher `json:"headers,omitempty" yaml:"headers,omitempty"`
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

	// Ports restricts the match to specific listener ports.
	Ports []uint32 `json:"ports,omitempty" yaml:"ports,omitempty"`

	// QueryParams are query parameter matchers that must all match.
	QueryParams []QueryParamMatcher `json:"queryParams,omitempty" yaml:"queryParams,omitempty"`
}
