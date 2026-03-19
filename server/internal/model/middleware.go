// Package model defines the core domain types used throughout Vrata.
package model

// MiddlewareType identifies which middleware behaviour a Middleware entity configures.
type MiddlewareType string

const (
	// MiddlewareTypeCORS configures CORS behaviour.
	MiddlewareTypeCORS MiddlewareType = "cors"

	// MiddlewareTypeJWT configures JWT authentication.
	MiddlewareTypeJWT MiddlewareType = "jwt"

	// MiddlewareTypeExtAuthz configures external authorisation
	// (external authorization).
	MiddlewareTypeExtAuthz MiddlewareType = "extAuthz"

	// MiddlewareTypeExtProc configures external processing
	// (external processing).
	MiddlewareTypeExtProc MiddlewareType = "extProc"

	// MiddlewareTypeRateLimit configures rate limiting
	// provides embedded rate limiting.
	MiddlewareTypeRateLimit MiddlewareType = "rateLimit"

	// MiddlewareTypeHeaders configures header manipulation
	// Adds or removes request/response
	// headers as a middleware, consistent with the middleware pattern.
	MiddlewareTypeHeaders MiddlewareType = "headers"

	// MiddlewareTypeAccessLog configures access logging per route/group.
	MiddlewareTypeAccessLog MiddlewareType = "accessLog"
)

// Middleware is an independent first-class entity that holds the configuration for a
// single middleware behaviour. Middlewares are attached to Routes or Groups via
// their ID in middlewareIds. Per-route behaviour can be tuned through
// MiddlewareOverride entries on Route or RouteGroup.
type Middleware struct {
	// ID is the unique identifier of the middleware.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the middleware.
	Name string `json:"name" yaml:"name"`

	// Type identifies which middleware type this entity provides.
	// Exactly one of the Config* fields below must match this type.
	Type MiddlewareType `json:"type" yaml:"type"`

	// CORS holds the CORS configuration. Set when Type == "cors".
	CORS *CORSConfig `json:"cors,omitempty" yaml:"cors,omitempty"`

	// JWT holds the JWT authentication configuration. Set when Type == "jwt".
	JWT *JWTConfig `json:"jwt,omitempty" yaml:"jwt,omitempty"`

	// ExtAuthz holds the external authorisation configuration.
	// Set when Type == "extAuthz".
	ExtAuthz *ExtAuthzConfig `json:"extAuthz,omitempty" yaml:"extAuthz,omitempty"`

	// ExtProc holds the external processing configuration.
	// Set when Type == "extProc".
	ExtProc *ExtProcConfig `json:"extProc,omitempty" yaml:"extProc,omitempty"`

	// RateLimit holds the rate limit configuration.
	// Set when Type == "rateLimit".
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty"`

	// Headers holds the header mutation filter configuration.
	// Set when Type == "headers".
	Headers *HeadersConfig `json:"headers,omitempty" yaml:"headers,omitempty"`

	// AccessLog holds the access log configuration.
	// Set when Type == "accessLog".
	AccessLog *AccessLogConfig `json:"accessLog,omitempty" yaml:"accessLog,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// CORS
// ────────────────────────────────────────────────────────────────────────────

// CORSConfig holds the configuration for CORS.
type CORSConfig struct {
	// AllowOrigins lists the allowed origin patterns.
	// Each entry is matched as an exact string or a regex if Regex is true.
	AllowOrigins []CORSOrigin `json:"allowOrigins,omitempty" yaml:"allowOrigins,omitempty"`

	// AllowMethods lists the HTTP methods allowed in CORS requests.
	// Example: ["GET", "POST", "OPTIONS"]
	AllowMethods []string `json:"allowMethods,omitempty" yaml:"allowMethods,omitempty"`

	// AllowHeaders lists the request headers allowed in CORS requests.
	AllowHeaders []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`

	// ExposeHeaders lists the response headers the browser is allowed to access.
	ExposeHeaders []string `json:"exposeHeaders,omitempty" yaml:"exposeHeaders,omitempty"`

	// MaxAge sets the preflight cache duration in seconds.
	// Maps to the Access-Control-Max-Age response header.
	MaxAge int32 `json:"maxAge,omitempty" yaml:"maxAge,omitempty"`

	// AllowCredentials indicates whether the request can include user credentials.
	AllowCredentials bool `json:"allowCredentials,omitempty" yaml:"allowCredentials,omitempty"`
}

// CORSOrigin describes a single allowed origin entry in CORSConfig.
type CORSOrigin struct {
	// Value is the origin string or regex pattern.
	Value string `json:"value" yaml:"value"`

	// Regex indicates that Value should be treated as a regular expression.
	Regex bool `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// JWT
// ────────────────────────────────────────────────────────────────────────────

// JWTConfig holds the configuration for JWT authentication.
// JWTConfig holds the configuration for a single JWT validation middleware.
// Each middleware validates tokens from one issuer. Use multiple JWT
// middlewares with skipWhen/onlyWhen for multi-provider setups.
type JWTConfig struct {
	// Issuer is the expected value of the "iss" claim.
	Issuer string `json:"issuer" yaml:"issuer"`

	// Audiences lists the expected "aud" claim values. If empty, audience
	// validation is skipped.
	Audiences []string `json:"audiences,omitempty" yaml:"audiences,omitempty"`

	// JWKsPath is the HTTP path on the destination from which the JSON Web
	// Key Set is fetched (e.g. "/.well-known/jwks.json").
	// When set, JWKsDestinationID must also be set.
	// Mutually exclusive with JWKsInline.
	JWKsPath string `json:"jwksPath,omitempty" yaml:"jwksPath,omitempty"`

	// JWKsDestinationID references the Destination that hosts the JWKS endpoint.
	// Required when JWKsPath is set.
	JWKsDestinationID string `json:"jwksDestinationId,omitempty" yaml:"jwksDestinationId,omitempty"`

	// JWKsInline is a literal JSON Web Key Set document.
	// Mutually exclusive with JWKsPath.
	JWKsInline string `json:"jwksInline,omitempty" yaml:"jwksInline,omitempty"`

	// JWKsRetrievalTimeout is the maximum time to download the JWKS
	// document from the remote endpoint. Only applies when JWKsPath is set.
	// Default: "10s".
	JWKsRetrievalTimeout string `json:"jwksRetrievalTimeout,omitempty" yaml:"jwksRetrievalTimeout,omitempty"`

	// ForwardJWT indicates whether the original Authorization header should be
	// forwarded to the upstream after successful validation.
	ForwardJWT bool `json:"forwardJwt,omitempty" yaml:"forwardJwt,omitempty"`

	// ClaimToHeaders maps JWT claim names to upstream request header names.
	ClaimToHeaders []JWTClaimHeader `json:"claimToHeaders,omitempty" yaml:"claimToHeaders,omitempty"`

	// AssertClaims is a list of CEL expressions evaluated against the decoded
	// JWT claims after the token is verified. Each expression receives a
	// `claims` map (string → any) with the decoded payload. All expressions
	// must evaluate to true for the request to pass. If any fails, 403.
	AssertClaims []string `json:"assertClaims,omitempty" yaml:"assertClaims,omitempty"`
}

// JWTClaimHeader extracts a value from the JWT claims using a CEL expression
// and injects it as a request header forwarded upstream.
type JWTClaimHeader struct {
	// Expr is a CEL expression evaluated against the decoded `claims` map.
	// Must return a string value. Supports nested access, array indexing,
	// and CEL built-in functions.
	// Examples: "claims.sub", "claims.user.id", "claims.roles[0]",
	//           "claims.orgs.map(o, o.name).join(',')"
	Expr string `json:"expr" yaml:"expr"`

	// Header is the request header name that receives the expression result.
	Header string `json:"header" yaml:"header"`
}

// ────────────────────────────────────────────────────────────────────────────
// ExtAuthz
// ────────────────────────────────────────────────────────────────────────────

// ExtAuthzConfig holds the configuration for the external authorization middleware.
type ExtAuthzConfig struct {
	// DestinationID references the Destination that hosts the authz service.
	DestinationID string `json:"destinationId" yaml:"destinationId"`

	// Mode selects the transport protocol: "http" or "grpc". Default: "http".
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`

	// Path is the authorization endpoint path (e.g. "/oauth2/auth").
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// DecisionTimeout is the maximum time for the authorization service to
	// return an allow/deny decision. Covers the entire call including
	// connect, TLS, and response. Default: "5s".
	DecisionTimeout string `json:"decisionTimeout,omitempty" yaml:"decisionTimeout,omitempty"`

	// FailureModeAllow lets requests through when the authz service is unreachable.
	FailureModeAllow bool `json:"failureModeAllow,omitempty" yaml:"failureModeAllow,omitempty"`

	// IncludeBody forwards the request body to the authz service.
	IncludeBody bool `json:"includeBody,omitempty" yaml:"includeBody,omitempty"`

	// OnCheck controls what is sent to the authz service.
	OnCheck *ExtAuthzOnCheck `json:"onCheck,omitempty" yaml:"onCheck,omitempty"`

	// OnAllow controls what happens when the authz allows the request.
	OnAllow *ExtAuthzOnAllow `json:"onAllow,omitempty" yaml:"onAllow,omitempty"`

	// OnDeny controls what is returned to the client when the authz denies.
	OnDeny *ExtAuthzOnDeny `json:"onDeny,omitempty" yaml:"onDeny,omitempty"`
}

// ExtAuthzOnCheck controls what is sent in the check request.
type ExtAuthzOnCheck struct {
	// ForwardHeaders lists client request header names to forward to the authz.
	// Host, Method, Path, and Content-Length are always sent automatically.
	ForwardHeaders []string `json:"forwardHeaders,omitempty" yaml:"forwardHeaders,omitempty"`

	// InjectHeaders are additional headers added to the check request.
	// Values support interpolation: ${request.host}, ${request.path},
	// ${request.method}, ${request.scheme}, ${request.header.X-Custom}.
	InjectHeaders []HeaderValue `json:"injectHeaders,omitempty" yaml:"injectHeaders,omitempty"`
}

// ExtAuthzOnAllow controls what happens when the authz returns 2xx.
type ExtAuthzOnAllow struct {
	// CopyToUpstream lists header name patterns from the authz response
	// that are added to the request forwarded to the upstream.
	// Supports wildcard suffix: "x-auth-request-*" matches any header
	// starting with "x-auth-request-".
	CopyToUpstream []string `json:"copyToUpstream,omitempty" yaml:"copyToUpstream,omitempty"`
}

// ExtAuthzOnDeny controls what is returned to the client when the authz denies.
type ExtAuthzOnDeny struct {
	// CopyToClient lists header name patterns from the authz response
	// that are returned to the client. Supports wildcard suffix.
	CopyToClient []string `json:"copyToClient,omitempty" yaml:"copyToClient,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// ExtProc
// ────────────────────────────────────────────────────────────────────────────

// ExtProcConfig holds the configuration for the external processor middleware.
// An external processor is a standalone service (gRPC or HTTP) that receives
// HTTP request/response phases and can inspect, mutate, or reject them.
type ExtProcConfig struct {
	// DestinationID references the Destination entity that hosts the
	// external processor service.
	DestinationID string `json:"destinationId" yaml:"destinationId"`

	// Mode selects the transport protocol: "grpc" (bidirectional stream)
	// or "http" (one POST per phase). Default: "grpc".
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`

	// PhaseTimeout is the maximum time for the processor to respond to each
	// phase message. If the processor does not respond within this duration,
	// the phase is treated as a failure (subject to AllowOnError).
	// Default: "200ms".
	PhaseTimeout string `json:"phaseTimeout,omitempty" yaml:"phaseTimeout,omitempty"`

	// Phases controls which parts of the HTTP transaction are sent to the
	// processor. If nil, only request and response headers are sent.
	Phases *ExtProcPhases `json:"phases,omitempty" yaml:"phases,omitempty"`

	// AllowOnError lets requests pass through when the processor is
	// unreachable or returns an error. When false (default), requests
	// are rejected with StatusOnError.
	AllowOnError bool `json:"allowOnError,omitempty" yaml:"allowOnError,omitempty"`

	// StatusOnError is the HTTP status code returned to the client when
	// the processor fails and AllowOnError is false. Default: 500.
	StatusOnError uint32 `json:"statusOnError,omitempty" yaml:"statusOnError,omitempty"`

	// AllowedMutations restricts which headers the processor is allowed
	// to set or remove. If nil, all headers can be mutated.
	AllowedMutations *MutationRules `json:"allowedMutations,omitempty" yaml:"allowedMutations,omitempty"`

	// ForwardRules restricts which request headers are sent to the
	// processor. Use this to prevent forwarding sensitive headers
	// like Authorization or Cookie. If nil, all headers are forwarded.
	ForwardRules *ForwardRules `json:"forwardRules,omitempty" yaml:"forwardRules,omitempty"`

	// DisableReject prevents the processor from rejecting requests.
	// When true, RejectRequest responses from the processor are ignored
	// and the request continues normally.
	DisableReject bool `json:"disableReject,omitempty" yaml:"disableReject,omitempty"`

	// ObserveMode enables fire-and-forget mode. When enabled, Vrata sends
	// phases to the processor but does not wait for responses. Useful for
	// logging, auditing, or analytics processors.
	ObserveMode *ObserveModeConfig `json:"observeMode,omitempty" yaml:"observeMode,omitempty"`

	// MetricsPrefix is an optional prefix for metrics emitted by this
	// middleware instance. Allows distinguishing between multiple
	// external processors in the same proxy.
	MetricsPrefix string `json:"metricsPrefix,omitempty" yaml:"metricsPrefix,omitempty"`
}

// ObserveModeConfig configures fire-and-forget processing. Requests are
// queued and processed by a background worker pool without blocking the
// client request.
type ObserveModeConfig struct {
	// Enabled activates observe mode. When false (default), the processor
	// response is awaited synchronously.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Workers is the number of background workers that drain the queue.
	// Default: 64.
	Workers int `json:"workers,omitempty" yaml:"workers,omitempty"`

	// QueueSize is the maximum number of pending requests. When full,
	// new requests are dropped with a warning log. Default: 4096.
	QueueSize int `json:"queueSize,omitempty" yaml:"queueSize,omitempty"`
}

// ExtProcPhases controls which phases of an HTTP transaction are sent
// to the external processor.
type ExtProcPhases struct {
	// RequestHeaders controls whether request headers are sent.
	// "send" (default) or "skip".
	RequestHeaders PhaseMode `json:"requestHeaders,omitempty" yaml:"requestHeaders,omitempty"`

	// ResponseHeaders controls whether response headers are sent.
	// "send" (default) or "skip".
	ResponseHeaders PhaseMode `json:"responseHeaders,omitempty" yaml:"responseHeaders,omitempty"`

	// RequestBody controls how the request body is sent.
	// "none" (default), "buffered", "bufferedPartial", or "streamed".
	RequestBody BodyMode `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`

	// ResponseBody controls how the response body is sent.
	// "none" (default), "buffered", "bufferedPartial", or "streamed".
	ResponseBody BodyMode `json:"responseBody,omitempty" yaml:"responseBody,omitempty"`

	// MaxBodyBytes is the maximum number of bytes to buffer when using
	// "bufferedPartial" mode. Excess bytes are not sent to the processor.
	// Default: 1048576 (1MB).
	MaxBodyBytes int64 `json:"maxBodyBytes,omitempty" yaml:"maxBodyBytes,omitempty"`
}

// PhaseMode controls whether a headers phase is sent to the processor.
type PhaseMode string

const (
	// PhaseModeSend sends the phase to the processor (default).
	PhaseModeSend PhaseMode = "send"

	// PhaseModeSkip skips the phase entirely.
	PhaseModeSkip PhaseMode = "skip"
)

// BodyMode controls how a body phase is sent to the processor.
type BodyMode string

const (
	// BodyModeNone does not send the body (default).
	BodyModeNone BodyMode = "none"

	// BodyModeBuffered buffers the entire body in memory and sends it
	// as a single message.
	BodyModeBuffered BodyMode = "buffered"

	// BodyModeBufferedPartial buffers the body up to a size limit and
	// sends whatever was buffered. If the body exceeds the limit, only
	// the buffered portion is sent.
	BodyModeBufferedPartial BodyMode = "bufferedPartial"

	// BodyModeStreamed streams body chunks to the processor as they
	// arrive. The processor responds to each chunk individually.
	BodyModeStreamed BodyMode = "streamed"
)

// MutationRules restricts which headers an external processor is allowed
// to set or remove. Both lists use exact header name matching (case-insensitive).
type MutationRules struct {
	// AllowHeaders lists header names the processor is allowed to mutate.
	// If empty, all headers are allowed (subject to DenyHeaders).
	AllowHeaders []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`

	// DenyHeaders lists header names the processor is NOT allowed to mutate.
	// Overrides AllowHeaders if a header appears in both.
	DenyHeaders []string `json:"denyHeaders,omitempty" yaml:"denyHeaders,omitempty"`
}

// ForwardRules restricts which request headers Vrata sends to the
// external processor. Both lists use exact header name matching (case-insensitive).
type ForwardRules struct {
	// AllowHeaders lists header names that are forwarded to the processor.
	// If empty, all headers are forwarded (subject to DenyHeaders).
	AllowHeaders []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`

	// DenyHeaders lists header names that are never forwarded to the
	// processor. Overrides AllowHeaders if a header appears in both.
	DenyHeaders []string `json:"denyHeaders,omitempty" yaml:"denyHeaders,omitempty"`
}

// ExtProcOverride carries per-route overrides for the external processor
// middleware. Used inside MiddlewareOverride.
type ExtProcOverride struct {
	// Phases overrides the processing phases for this route.
	Phases *ExtProcPhases `json:"phases,omitempty" yaml:"phases,omitempty"`

	// AllowOnError overrides the fail-open behavior for this route.
	AllowOnError *bool `json:"allowOnError,omitempty" yaml:"allowOnError,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// Rate Limit
// ────────────────────────────────────────────────────────────────────────────

// RateLimitConfig holds the configuration for the embedded rate limiter.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained rate of requests allowed per client IP.
	// Default: 10.
	RequestsPerSecond float64 `json:"requestsPerSecond,omitempty" yaml:"requestsPerSecond,omitempty"`

	// Burst is the maximum number of requests allowed in a burst above the
	// sustained rate. Default: same as RequestsPerSecond.
	Burst int `json:"burst,omitempty" yaml:"burst,omitempty"`

	// TrustedProxies lists CIDR ranges from which X-Forwarded-For is trusted.
	// When empty, X-Forwarded-For is ignored and the direct client IP is used.
	// Example: ["10.0.0.0/8", "172.16.0.0/12"]
	TrustedProxies []string `json:"trustedProxies,omitempty" yaml:"trustedProxies,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// Header Manipulation
// ────────────────────────────────────────────────────────────────────────────

// HeadersConfig holds the configuration for header manipulation.
// Adds or removes request/response headers. Modeled as a Filter entity so it
// follows the same middleware pattern as CORS, JWT, etc.
type HeadersConfig struct {
	// RequestHeadersToAdd are headers added to the request before forwarding
	// to the upstream.
	RequestHeadersToAdd []HeaderValue `json:"requestHeadersToAdd,omitempty" yaml:"requestHeadersToAdd,omitempty"`

	// RequestHeadersToRemove are header names removed from the request
	// before forwarding.
	RequestHeadersToRemove []string `json:"requestHeadersToRemove,omitempty" yaml:"requestHeadersToRemove,omitempty"`

	// ResponseHeadersToAdd are headers added to the response before
	// returning to the client.
	ResponseHeadersToAdd []HeaderValue `json:"responseHeadersToAdd,omitempty" yaml:"responseHeadersToAdd,omitempty"`

	// ResponseHeadersToRemove are header names removed from the response
	// before returning to the client.
	ResponseHeadersToRemove []string `json:"responseHeadersToRemove,omitempty" yaml:"responseHeadersToRemove,omitempty"`
}

// HeaderValue is a key-value pair for header manipulation.
type HeaderValue struct {
	// Key is the header name.
	Key string `json:"key" yaml:"key"`

	// Value is the header value.
	Value string `json:"value" yaml:"value"`

	// Append controls whether the header is appended (true) or replaced
	// (false) if it already exists. Default: true.
	Append bool `json:"append,omitempty" yaml:"append,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// Rate Limit Descriptors (for MiddlewareOverride)
// ────────────────────────────────────────────────────────────────────────────


// MiddlewareOverride carries per-route or per-group overrides for a specific filter.
// The map key in Route.MiddlewareOverrides / RouteGroup.MiddlewareOverrides is the Filter ID.
//
// Merge semantics when both group and route carry an override for the same filter:
// the route override wins. Fields absent in the route override are not filled in
// from the group override — the entire route override replaces the group override
// for that filter.
type MiddlewareOverride struct {
	// Disabled completely disables the filter for this route/group.
	// When true, no other field is evaluated.
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty"`

	// SkipWhen is a list of CEL expressions evaluated against the request.
	// If ANY expression evaluates to true, the middleware is skipped.
	// The middleware is active by default. Mutually exclusive with OnlyWhen.
	SkipWhen []string `json:"skipWhen,omitempty" yaml:"skipWhen,omitempty"`

	// OnlyWhen is a list of CEL expressions evaluated against the request.
	// The middleware is only executed if at least one expression evaluates to true.
	// The middleware is inactive by default. Mutually exclusive with SkipWhen.
	OnlyWhen []string `json:"onlyWhen,omitempty" yaml:"onlyWhen,omitempty"`

	// Headers overrides header manipulation for this route/group.
	// Only meaningful when the referenced filter is of type "headers".
	Headers *HeadersConfig `json:"headers,omitempty" yaml:"headers,omitempty"`

	// ExtProc overrides external processor settings for this route/group.
	// Only meaningful when the referenced middleware is of type "extProc".
	ExtProc *ExtProcOverride `json:"extProc,omitempty" yaml:"extProc,omitempty"`
}
