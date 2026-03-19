// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package model

// Route is an independent first-class entity. It describes how a specific set
// of HTTP requests should be matched and what to do with the traffic.
// Routes are not owned by a single group; they can be referenced by any number
// of RouteGroups via RouteGroup.RouteIDs.
//
// A route operates in exactly one mode, determined by which action field is set:
//
//   - Forward        → forward traffic to one or more upstream Destinations.
//   - Redirect       → return an HTTP redirect to the client (3xx).
//   - DirectResponse → return a fixed HTTP response without contacting any upstream.
//
// These three fields are mutually exclusive. Setting more than one is a
// validation error.
type Route struct {
	// ID is the unique identifier of the route.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the route.
	Name string `json:"name" yaml:"name"`

	// Match defines the conditions that a request must satisfy.
	Match MatchRule `json:"match" yaml:"match"`

	// Forward proxies the request to one or more upstream
	// Destinations. Contains all forwarding behaviour: destinations, timeouts,
	// retries, URL rewriting, and traffic mirroring.
	// Mutually exclusive with Redirect and DirectResponse.
	Forward *ForwardAction `json:"forward,omitempty" yaml:"forward,omitempty"`

	// Redirect returns an HTTP redirect to the client
	// instead of forwarding to an upstream.
	// Mutually exclusive with Forward and DirectResponse.
	Redirect *RouteRedirect `json:"redirect,omitempty" yaml:"redirect,omitempty"`

	// DirectResponse returns a fixed HTTP response without
	// contacting any upstream. Useful for health-check endpoints, maintenance
	// pages, or returning a static 404.
	// Mutually exclusive with Forward and Redirect.
	DirectResponse *RouteDirectResponse `json:"directResponse,omitempty" yaml:"directResponse,omitempty"`

	// MiddlewareIDs lists the IDs of Middleware entities active on this route.
	// The builder activates them
	// only for this route (other routes where the middleware is not listed
	// are not active)..
	MiddlewareIDs []string `json:"middlewareIds,omitempty" yaml:"middlewareIds,omitempty"`

	// MiddlewareOverrides carries per-route overrides for active middlewares.
	// The map key is the Middleware ID. When both the route's group and the
	// route itself carry an override for the same middleware, the route
	// override wins entirely (more specific takes precedence).
	MiddlewareOverrides map[string]MiddlewareOverride `json:"middlewareOverrides,omitempty" yaml:"middlewareOverrides,omitempty"`

	// OnError defines fallback actions when the forward action fails.
	// Rules are evaluated in order; the first rule whose On list matches
	// the error type is executed. If no rule matches, Vrata returns a
	// default JSON error response. Only meaningful when Forward is set.
	OnError []OnErrorRule `json:"onError,omitempty" yaml:"onError,omitempty"`
}

// ProxyErrorType classifies an error that occurred during forwarding.
type ProxyErrorType string

const (
	// ProxyErrConnectionRefused — TCP connect was refused by the endpoint.
	ProxyErrConnectionRefused ProxyErrorType = "connection_refused"

	// ProxyErrConnectionReset — connection established but reset by the endpoint.
	ProxyErrConnectionReset ProxyErrorType = "connection_reset"

	// ProxyErrDNSFailure — hostname could not be resolved.
	ProxyErrDNSFailure ProxyErrorType = "dns_failure"

	// ProxyErrTimeout — request or per-attempt timeout expired.
	ProxyErrTimeout ProxyErrorType = "timeout"

	// ProxyErrTLSHandshakeFailure — TLS handshake with the upstream failed.
	ProxyErrTLSHandshakeFailure ProxyErrorType = "tls_handshake_failure"

	// ProxyErrCircuitOpen — circuit breaker prevented the attempt.
	ProxyErrCircuitOpen ProxyErrorType = "circuit_open"

	// ProxyErrNoDestination — no destination has healthy endpoints.
	ProxyErrNoDestination ProxyErrorType = "no_destination"

	// ProxyErrNoEndpoint — destination exists but all endpoints are down.
	ProxyErrNoEndpoint ProxyErrorType = "no_endpoint"

	// ProxyErrInfrastructure is a wildcard that matches all infrastructure
	// errors.
	ProxyErrInfrastructure ProxyErrorType = "infrastructure"

	// ProxyErrAll is a wildcard that matches every error type.
	ProxyErrAll ProxyErrorType = "all"
)

// OnErrorRule defines a fallback action for a specific set of proxy errors.
// Exactly one of Forward, Redirect, or DirectResponse must be set.
type OnErrorRule struct {
	// On lists the error types that trigger this rule. Evaluated as OR:
	// if the actual error matches any entry, the rule fires.
	// Supports individual types and wildcards ("infrastructure", "all").
	On []ProxyErrorType `json:"on" yaml:"on"`

	// Forward proxies the original request to fallback destinations.
	// Vrata injects X-Vrata-Error-* headers with the error context.
	// Mutually exclusive with Redirect and DirectResponse.
	Forward *ForwardAction `json:"forward,omitempty" yaml:"forward,omitempty"`

	// Redirect returns an HTTP redirect to the client.
	// Mutually exclusive with Forward and DirectResponse.
	Redirect *RouteRedirect `json:"redirect,omitempty" yaml:"redirect,omitempty"`

	// DirectResponse returns a fixed HTTP response.
	// Mutually exclusive with Forward and Redirect.
	DirectResponse *RouteDirectResponse `json:"directResponse,omitempty" yaml:"directResponse,omitempty"`
}
// receives each request (level 1 — before endpoint selection).
type DestinationLBPolicy string

const (
	// DestinationLBWeightedRandom picks a destination by weighted random.
	// This is the default when DestinationBalancing is nil.
	DestinationLBWeightedRandom DestinationLBPolicy = "WEIGHTED_RANDOM"

	// DestinationLBWeightedConsistentHash uses a weighted consistent hash ring
	// with a session cookie to pin clients to the same destination.
	// Disruption is minimal and proportional to weight changes.
	DestinationLBWeightedConsistentHash DestinationLBPolicy = "WEIGHTED_CONSISTENT_HASH"

	// DestinationLBSticky uses a session cookie and an external session store
	// (e.g. Redis) to guarantee zero disruption when weights change.
	// New clients are assigned via weighted random; existing clients always
	// return to the same destination until the cookie expires or the
	// destination is removed.
	DestinationLBSticky DestinationLBPolicy = "STICKY"
)

// ForwardAction groups all configuration that controls how Vrata forwards
// a matched request to upstream Destinations.
type ForwardAction struct {
	// Destinations lists the upstream Destinations for this route.
	// Each entry references a Destination by ID and carries a traffic weight.
	// Weights across all destinations must sum to 100 when more than one
	// is defined. If only one destination is provided its weight is ignored.
	Destinations []DestinationRef `json:"destinations" yaml:"destinations"`

	// DestinationBalancing controls how Vrata selects which Destination
	// receives each request (level 1). When nil, WEIGHTED_RANDOM is used.
	DestinationBalancing *DestinationBalancing `json:"destinationBalancing,omitempty" yaml:"destinationBalancing,omitempty"`

	// Timeouts controls how long the request is allowed to take.
	Timeouts *RouteTimeouts `json:"timeouts,omitempty" yaml:"timeouts,omitempty"`

	// Retry controls automatic retry behaviour when the upstream fails.
	Retry *RouteRetry `json:"retry,omitempty" yaml:"retry,omitempty"`

	// Rewrite transforms the URL before sending the request upstream.
	Rewrite *RouteRewrite `json:"rewrite,omitempty" yaml:"rewrite,omitempty"`

	// Mirror sends a copy of the traffic to an additional Destination for
	// observability or testing. The mirrored request is fire-and-forget;
	// its response is discarded and never affects the client.
	Mirror *RouteMirror `json:"mirror,omitempty" yaml:"mirror,omitempty"`

	// MaxGRPCTimeout caps the timeout that a gRPC client can request via
	// the grpc-timeout header. If the client asks for more, Vrata clamps
	// it to this value. Accepts Go duration strings (e.g. "30s").
	MaxGRPCTimeout string `json:"maxGrpcTimeout,omitempty" yaml:"maxGrpcTimeout,omitempty"`
}

// DestinationBalancing controls the algorithm for selecting which Destination
// receives each request. The algorithm field selects the strategy;
// algorithm-specific parameters live in the corresponding nested struct.
type DestinationBalancing struct {
	// Algorithm selects the destination selection policy.
	// Default: WEIGHTED_RANDOM.
	Algorithm DestinationLBPolicy `json:"algorithm" yaml:"algorithm"`

	// WeightedConsistentHash holds parameters for WEIGHTED_CONSISTENT_HASH.
	// Only used when Algorithm is WEIGHTED_CONSISTENT_HASH.
	WeightedConsistentHash *WeightedConsistentHashOptions `json:"weightedConsistentHash,omitempty" yaml:"weightedConsistentHash,omitempty"`

	// Sticky holds parameters for STICKY.
	// Only used when Algorithm is STICKY.
	Sticky *StickyOptions `json:"sticky,omitempty" yaml:"sticky,omitempty"`
}

// WeightedConsistentHashOptions configures sticky destination selection.
// A session cookie identifies the client; the hash of
// (sessionID + routeID) determines the destination.
type WeightedConsistentHashOptions struct {
	// Cookie configures the session cookie used for client identification.
	Cookie *DestinationPinCookie `json:"cookie,omitempty" yaml:"cookie,omitempty"`
}

// DestinationPinCookie configures the session cookie for destination pinning.
type DestinationPinCookie struct {
	// Name is the cookie name. Default: "_vrata_destination_pin".
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// TTL is the lifetime of the session cookie. Accepts Go duration strings
	// (e.g. "1h", "24h"). Default: "1h".
	TTL string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

// StickyOptions configures zero-disruption destination pinning backed by
// an external session store (e.g. Redis). New clients are assigned via
// weighted random; existing clients always return to the same destination.
type StickyOptions struct {
	// Cookie configures the session cookie used for client identification.
	Cookie *DestinationPinCookie `json:"cookie,omitempty" yaml:"cookie,omitempty"`
}

// RouteTimeouts controls how long a request is allowed to take at the
// route level. This is the outermost watchdog — if the total time exceeds
// the limit, Vrata cuts the request regardless of what the destination
// timeouts allow.
type RouteTimeouts struct {
	// Request is the total time the entire request may take from the moment
	// Vrata receives the first byte from the client until the response is
	// fully sent. Accepts Go duration strings (e.g. "30s", "1m").
	Request string `json:"request,omitempty" yaml:"request,omitempty"`
}

// RetryCondition is a semantic name for a class of upstream failures that
// should trigger a retry. These are translated internally into
// retry_on values internally.
type RetryCondition string

const (
	// RetryOnServerError retries when the upstream returns a 5xx status.
	RetryOnServerError RetryCondition = "server-error"

	// RetryOnConnectionFailure retries when the connection to the upstream
	// fails or is reset before a response is received.
	RetryOnConnectionFailure RetryCondition = "connection-failure"

	// RetryOnGatewayError retries on 502, 503, or 504 specifically.
	RetryOnGatewayError RetryCondition = "gateway-error"

	// RetryOnRetriableCodes retries when the upstream returns one of the
	// status codes listed in RetriableCodes.
	RetryOnRetriableCodes RetryCondition = "retriable-codes"
)

// RouteRetry controls automatic retry behaviour when the upstream fails.
type RouteRetry struct {
	// Attempts is the maximum number of times the request is retried.
	// The original request does not count — setting 3 means up to 3 retries
	// after the first failure (4 total attempts).
	Attempts uint32 `json:"attempts" yaml:"attempts"`

	// PerAttemptTimeout is the deadline for each individual attempt.
	// Accepts Go duration strings (e.g. "5s").
	PerAttemptTimeout string `json:"perAttemptTimeout,omitempty" yaml:"perAttemptTimeout,omitempty"`

	// On lists the conditions that trigger a retry. When empty, defaults
	// to ["server-error", "connection-failure"].
	On []RetryCondition `json:"on,omitempty" yaml:"on,omitempty"`

	// RetriableCodes is the explicit list of HTTP status codes that trigger
	// a retry. Only evaluated when On contains "retriable-codes".
	RetriableCodes []uint32 `json:"retriableCodes,omitempty" yaml:"retriableCodes,omitempty"`

	// Backoff controls the delay between retry attempts.
	Backoff *RetryBackoff `json:"backoff,omitempty" yaml:"backoff,omitempty"`
}

// RetryBackoff controls the exponential backoff between retry attempts.
type RetryBackoff struct {
	// Base is the initial delay before the first retry.
	// Accepts Go duration strings (e.g. "100ms").
	Base string `json:"base,omitempty" yaml:"base,omitempty"`

	// Max is the upper bound on the backoff delay.
	// Accepts Go duration strings (e.g. "1s").
	Max string `json:"max,omitempty" yaml:"max,omitempty"`
}

// RouteRewrite transforms the request URL before forwarding to the upstream.
// At most one of Path and PathRegex should be set.
type RouteRewrite struct {
	// Path replaces the matched path prefix with the given value.
	// For example, if the route matches "/api/v1" and Path is "/internal",
	// a request to "/api/v1/users" arrives at the destination as "/internal/users".
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// PathRegex rewrites the path using a regular expression substitution.
	PathRegex *RewriteRegex `json:"pathRegex,omitempty" yaml:"pathRegex,omitempty"`

	// Host overrides the Host header sent to the upstream with a fixed value.
	Host string `json:"host,omitempty" yaml:"host,omitempty"`

	// HostFromHeader takes the value of the named request header and uses it
	// as the Host header sent to the upstream.
	HostFromHeader string `json:"hostFromHeader,omitempty" yaml:"hostFromHeader,omitempty"`

	// AutoHost sets the Host header to the hostname of the upstream
	// Destination automatically. Useful when the destination requires its own
	// hostname (e.g. an external SaaS API).
	AutoHost bool `json:"autoHost,omitempty" yaml:"autoHost,omitempty"`
}

// RewriteRegex defines a regular expression path rewrite.
type RewriteRegex struct {
	// Pattern is the RE2 regular expression matched against the request path.
	Pattern string `json:"pattern" yaml:"pattern"`

	// Substitution is the replacement string. Capture groups from Pattern
	// can be referenced as \1, \2, etc.
	Substitution string `json:"substitution" yaml:"substitution"`
}

// RouteRedirect returns an HTTP redirect to the client.
// Fields are combined: if both Scheme and Host are set, both are applied.
type RouteRedirect struct {
	// URL is the complete target URL. When set, Scheme, Host, Path, and
	// StripQuery are ignored — the client is sent directly to this URL.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// Scheme overrides only the scheme (e.g. "http" → "https").
	Scheme string `json:"scheme,omitempty" yaml:"scheme,omitempty"`

	// Host overrides only the hostname in the redirect target.
	Host string `json:"host,omitempty" yaml:"host,omitempty"`

	// Path replaces the path component of the redirect target.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// StripQuery removes the query string from the redirect target.
	StripQuery bool `json:"stripQuery,omitempty" yaml:"stripQuery,omitempty"`

	// Code is the HTTP status code returned to the client.
	// Accepted values: 301, 302, 303, 307, 308. Default: 301.
	Code uint32 `json:"code,omitempty" yaml:"code,omitempty"`
}

// RouteDirectResponse returns a fixed HTTP response
// without forwarding the request to any upstream.
type RouteDirectResponse struct {
	// Status is the HTTP status code to return. Required.
	Status uint32 `json:"status" yaml:"status"`

	// Body is the response body returned to the client. Optional.
	Body string `json:"body,omitempty" yaml:"body,omitempty"`
}

// RouteMirror sends a copy of the matched traffic to an additional
// Destination for observability or testing. The mirrored request is
// fire-and-forget; its response is discarded and never affects the client.
type RouteMirror struct {
	// DestinationID is the ID of the Destination that receives the
	// mirrored traffic.
	DestinationID string `json:"destinationId" yaml:"destinationId"`

	// Percentage is the fraction of requests to mirror, from 0 to 100.
	// Default: 100 (mirror all matched traffic).
	Percentage uint32 `json:"percentage,omitempty" yaml:"percentage,omitempty"`
}

