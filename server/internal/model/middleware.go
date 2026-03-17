// Package model defines the core domain types used throughout Rutoso.
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
type JWTConfig struct {
	// Providers is a map of provider name to JWTProvider configuration.
	// The map key is referenced from per-route overrides to select a specific
	// provider (or disable authentication for that route).
	Providers map[string]JWTProvider `json:"providers,omitempty" yaml:"providers,omitempty"`

	// Rules defines which request paths require JWT validation and which
	// provider to apply. If empty, all paths are validated by all providers.
	Rules []JWTRule `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// JWTProvider describes a single JWT identity provider.
type JWTProvider struct {
	// Issuer is the expected value of the "iss" claim.
	Issuer string `json:"issuer" yaml:"issuer"`

	// Audiences lists the expected "aud" claim values. If empty, audience
	// validation is skipped.
	Audiences []string `json:"audiences,omitempty" yaml:"audiences,omitempty"`

	// JWKsURI is the URL path from which the JSON Web Key Set is fetched.
	// When set, JWKsDestinationID must also be set to identify the upstream
	// that serves the JWKS endpoint.
	// Mutually exclusive with JWKsInline.
	JWKsURI string `json:"jwksUri,omitempty" yaml:"jwksUri,omitempty"`

	// JWKsDestinationID references the Destination entity that hosts the
	// JWKS endpoint. Required when JWKsURI is set. The Destination's cluster
	// (with its TLS config) is used as the upstream for fetching keys.
	JWKsDestinationID string `json:"jwksDestinationId,omitempty" yaml:"jwksDestinationId,omitempty"`

	// JWKsInline is a literal JSON Web Key Set document.
	// Mutually exclusive with JWKsURI.
	JWKsInline string `json:"jwksInline,omitempty" yaml:"jwksInline,omitempty"`

	// ForwardJWT indicates whether the original Authorization header should be
	// forwarded to the upstream after successful validation.
	ForwardJWT bool `json:"forwardJwt,omitempty" yaml:"forwardJwt,omitempty"`

	// ClaimToHeaders maps JWT claim names to upstream request header names.
	// The claim value is set as the header value before forwarding.
	ClaimToHeaders []JWTClaimHeader `json:"claimToHeaders,omitempty" yaml:"claimToHeaders,omitempty"`
}

// JWTClaimHeader maps a JWT claim to a request header forwarded upstream.
type JWTClaimHeader struct {
	// Claim is the JWT claim name (e.g. "sub", "email").
	Claim string `json:"claim" yaml:"claim"`

	// Header is the request header name that receives the claim value.
	Header string `json:"header" yaml:"header"`
}

// JWTRule associates a path prefix with a required JWT provider.
type JWTRule struct {
	// Match is the path prefix this rule applies to.
	Match string `json:"match" yaml:"match"`

	// Requires lists the provider names that must validate the request.
	// All listed providers must succeed (AND semantics).
	Requires []string `json:"requires,omitempty" yaml:"requires,omitempty"`

	// AllowMissing allows requests without a JWT token to pass through.
	// Useful for public endpoints that coexist in the same listener.
	AllowMissing bool `json:"allowMissing,omitempty" yaml:"allowMissing,omitempty"`
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

	// Timeout is the check request deadline (e.g. "5s").
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`

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

	// Timeout is the per-message deadline (e.g. "200ms", "2s").
	// If the processor does not respond within this duration, the phase
	// is treated as a failure (subject to AllowOnError). Default: "200ms".
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`

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

	// ObserveOnly enables fire-and-forget mode. Rutoso sends phases to
	// the processor but does not wait for responses. Useful for logging,
	// auditing, or analytics processors.
	ObserveOnly bool `json:"observeOnly,omitempty" yaml:"observeOnly,omitempty"`

	// MetricsPrefix is an optional prefix for metrics emitted by this
	// middleware instance. Allows distinguishing between multiple
	// external processors in the same proxy.
	MetricsPrefix string `json:"metricsPrefix,omitempty" yaml:"metricsPrefix,omitempty"`
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

// ForwardRules restricts which request headers Rutoso sends to the
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

	// JWTProvider selects a specific JWT provider by name (instead of requiring all).
	// Only meaningful when the referenced filter is of type "jwt".
	JWTProvider string `json:"jwtProvider,omitempty" yaml:"jwtProvider,omitempty"`

	// Headers overrides header manipulation for this route/group.
	// Only meaningful when the referenced filter is of type "headers".
	Headers *HeadersConfig `json:"headers,omitempty" yaml:"headers,omitempty"`

	// ExtProc overrides external processor settings for this route/group.
	// Only meaningful when the referenced middleware is of type "extProc".
	ExtProc *ExtProcOverride `json:"extProc,omitempty" yaml:"extProc,omitempty"`
}
