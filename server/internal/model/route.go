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
	// Destinations. Contains all forwarding behaviour: backends, timeouts,
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
}

// ForwardAction groups all configuration that controls how Rutoso forwards
// a matched request to upstream Destinations.
type ForwardAction struct {
	// Backends lists the upstream Destinations for this route.
	// Each entry references a Destination by ID and carries a traffic weight.
	// Weights across all backends must sum to 100 when more than one backend
	// is defined. If only one backend is provided its weight is ignored.
	Backends []BackendRef `json:"backends" yaml:"backends"`

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

	// HashPolicy controls how Rutoso computes the consistent-hash key for
	// sticky sessions. Only relevant when the Destination uses RING_HASH or
	// MAGLEV balancing (configured in Destination.Options.Balancing).
	//
	// This field lives on the route — not on the Destination — because Rutoso
	// evaluates hash_policy at routing time using request attributes (headers,
	// cookies, client IP) that are only available in the RouteAction context.
	// The Destination defines *which algorithm* and ring parameters to use;
	// the route defines *what request data* feeds the hash function.
	// Both sides must be configured for sticky sessions to work.
	//
	// Entries are evaluated in order; the first one that produces a value wins.
	// Evaluated at routing time before selecting an endpoint.
	HashPolicy []HashPolicy `json:"hashPolicy,omitempty" yaml:"hashPolicy,omitempty"`


	// MaxGRPCTimeout caps the timeout that a gRPC client can request via
	// the grpc-timeout header. If the client asks for more, Rutoso clamps
	// it to this value. Accepts Go duration strings (e.g. "30s").
	MaxGRPCTimeout string `json:"maxGrpcTimeout,omitempty" yaml:"maxGrpcTimeout,omitempty"`

	// DestinationPinning enables sticky destination selection for canary
	// deployments. When enabled, the first request uses weighted selection
	// to pick a destination; subsequent requests from the same client are
	// pinned to that destination via a session cookie and a weighted
	// consistent hash. All proxies compute the same result deterministically
	// from the snapshot — no shared state required.
	DestinationPinning *DestinationPinning `json:"destinationPinning,omitempty" yaml:"destinationPinning,omitempty"`
}

// DestinationPinning configures sticky destination selection. Once a client
// is assigned to a destination, it stays there until the cookie expires or
// the destination is removed from the backends list.
type DestinationPinning struct {
	// CookieName is the name of the session cookie that identifies the client.
	// All routes sharing the same cookie name share the same session ID.
	// Default: "_rutoso_pin".
	CookieName string `json:"cookieName,omitempty" yaml:"cookieName,omitempty"`

	// TTL is the lifetime of the session cookie. Accepts Go duration strings
	// (e.g. "1h", "24h"). When the cookie expires, the client goes through
	// weighted selection again. Default: "1h".
	TTL string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

// RouteTimeouts controls how long a request is allowed to take.
type RouteTimeouts struct {
	// Request is the total time the entire request may take from the moment
	// Rutoso receives the first byte from the client until the response is
	// fully sent. Accepts Go duration strings (e.g. "30s", "1m").
	Request string `json:"request,omitempty" yaml:"request,omitempty"`

	// Idle is the maximum time a connection may remain open with no data
	// flowing. Accepts Go duration strings. Useful for long-lived streaming
	// connections that may stall.
	Idle string `json:"idle,omitempty" yaml:"idle,omitempty"`
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
	// a request to "/api/v1/users" arrives at the backend as "/internal/users".
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// PathRegex rewrites the path using a regular expression substitution.
	PathRegex *RewriteRegex `json:"pathRegex,omitempty" yaml:"pathRegex,omitempty"`

	// Host overrides the Host header sent to the upstream with a fixed value.
	Host string `json:"host,omitempty" yaml:"host,omitempty"`

	// HostFromHeader takes the value of the named request header and uses it
	// as the Host header sent to the upstream.
	HostFromHeader string `json:"hostFromHeader,omitempty" yaml:"hostFromHeader,omitempty"`

	// AutoHost sets the Host header to the hostname of the upstream
	// Destination automatically. Useful when the backend requires its own
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

