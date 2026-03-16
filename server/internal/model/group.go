// Package model defines the core domain types used throughout Rutoso.
// These are pure data structures with no I/O or business logic.
package model

// RouteGroup represents a named collection of routes that share common
// attributes such as a path prefix, hostname matchers, and headers.
// These shared attributes are automatically inherited by all child routes.
type RouteGroup struct {
	// ID is the unique identifier of the group.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the group.
	Name string `json:"name" yaml:"name"`

	// Description is an optional explanation of the group's purpose.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Prefix is a path prefix applied to all routes in this group.
	// Example: "/api/v1" will make a route with path "/users" match "/api/v1/users".
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`

	// Hostnames restricts this group to requests targeting specific virtual hosts.
	// An empty slice means the group matches all hostnames.
	Hostnames []string `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`

	// Headers are additional request header matchers applied to all routes in
	// this group. Routes can define their own headers on top of these.
	Headers []HeaderMatcher `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Routes holds all routes that belong to this group.
	Routes []Route `json:"routes,omitempty" yaml:"routes,omitempty"`
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

// MatchRule uniquely identifies a route within its group.
// Two routes with identical MatchRules within the same group are considered duplicates.
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

	// Hostnames overrides the group-level hostname matchers for this specific route.
	// An empty slice inherits the group's hostnames.
	Hostnames []string `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`

	// Headers are additional header matchers specific to this route.
	// These are evaluated in addition to the group-level headers.
	Headers []HeaderMatcher `json:"headers,omitempty" yaml:"headers,omitempty"`
}
