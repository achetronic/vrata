// Package model defines the core domain types used throughout Rutoso.
package model

// MiddlewareType identifies which Envoy HTTP filter a Filter entity configures.
type MiddlewareType string

const (
	// MiddlewareTypeCORS configures CORS behaviour.
	MiddlewareTypeCORS MiddlewareType = "cors"

	// MiddlewareTypeJWT configures JWT authentication.
	MiddlewareTypeJWT MiddlewareType = "jwt"

	// MiddlewareTypeExtAuthz configures the Envoy external authorisation filter
	// (external authorization).
	MiddlewareTypeExtAuthz MiddlewareType = "extAuthz"

	// MiddlewareTypeExtProc configures the Envoy external processing filter
	// (external processing).
	MiddlewareTypeExtProc MiddlewareType = "extProc"

	// MiddlewareTypeRateLimit configures the Envoy rate limit filter
	// provides embedded rate limiting.
	MiddlewareTypeRateLimit MiddlewareType = "rateLimit"

	// MiddlewareTypeHeaders configures the Envoy header mutation filter
	// Adds or removes request/response
	// headers as a middleware, consistent with the Filter entity pattern.
	MiddlewareTypeHeaders MiddlewareType = "headers"
)

// Filter is an independent first-class entity that holds the configuration for a
// single Envoy HTTP filter. Filters are registered on Listeners by referencing
// their ID in Listener.FilterIDs. Per-route behaviour can be tuned through
// MiddlewareOverride entries on Route or RouteGroup.
type Middleware struct {
	// ID is the unique identifier of the filter.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the filter.
	Name string `json:"name" yaml:"name"`

	// Type identifies which Envoy HTTP filter this entity configures.
	// Exactly one of the Config* fields below must match this type.
	Type MiddlewareType `json:"type" yaml:"type"`

	// CORS holds the CORS filter configuration. Set when Type == "cors".
	CORS *CORSConfig `json:"cors,omitempty" yaml:"cors,omitempty"`

	// JWT holds the JWT authn filter configuration. Set when Type == "jwt".
	JWT *JWTConfig `json:"jwt,omitempty" yaml:"jwt,omitempty"`

	// ExtAuthz holds the external authorisation filter configuration.
	// Set when Type == "extAuthz".
	ExtAuthz *ExtAuthzConfig `json:"extAuthz,omitempty" yaml:"extAuthz,omitempty"`

	// ExtProc holds the external processing filter configuration.
	// Set when Type == "extProc".
	ExtProc *ExtProcConfig `json:"extProc,omitempty" yaml:"extProc,omitempty"`

	// RateLimit holds the rate limit filter configuration.
	// Set when Type == "rateLimit".
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty"`

	// Headers holds the header mutation filter configuration.
	// Set when Type == "headers".
	Headers *HeadersConfig `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// CORS
// ────────────────────────────────────────────────────────────────────────────

// CORSConfig holds the configuration for the Envoy CORS HTTP filter.
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

// JWTConfig holds the configuration for the Envoy jwt_authn HTTP filter.
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

// ExtAuthzConfig holds the configuration for the Envoy ext_authz HTTP filter.
type ExtAuthzConfig struct {
	// DestinationID references the Destination entity that hosts the
	// authorisation service. Rutoso builds the full server_uri from the
	// Destination's host, port, and TLS config automatically.
	DestinationID string `json:"destinationId" yaml:"destinationId"`

	// Mode selects the transport protocol to the authz service.
	// "grpc" or "http". Default: "http".
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`

	// Path is the path of the authorization endpoint on the service.
	// Example: "/oauth2/auth". Rutoso appends it to the Destination's
	// host:port to build the full server_uri automatically.
	// Ignored in gRPC mode.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// Timeout is the authorisation request deadline (e.g. "5s", "500ms").
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// FailureModeAllow controls what happens when the authz service is
	// unreachable. If true, requests are allowed through (fail-open).
	// Default is false (fail-closed).
	FailureModeAllow bool `json:"failureModeAllow,omitempty" yaml:"failureModeAllow,omitempty"`

	// IncludeRequestBodyInCheck forwards the request body to the authz service.
	IncludeRequestBodyInCheck bool `json:"includeRequestBodyInCheck,omitempty" yaml:"includeRequestBodyInCheck,omitempty"`

	// AllowedHeaders lists header patterns from the client request that are
	// forwarded to the authz service. Host, Method, Path, Content-Length,
	// and Authorization are always included automatically.
	AllowedHeaders []string `json:"allowedHeaders,omitempty" yaml:"allowedHeaders,omitempty"`

	// HeadersToAdd are extra headers injected into the check request sent
	// to the authz service.
	HeadersToAdd []HeaderValue `json:"headersToAdd,omitempty" yaml:"headersToAdd,omitempty"`

	// AllowedUpstreamHeaders lists header patterns from the authz response
	// that are added to the original client request (forwarded to upstream)
	// when the authz service returns 200 OK.
	AllowedUpstreamHeaders []string `json:"allowedUpstreamHeaders,omitempty" yaml:"allowedUpstreamHeaders,omitempty"`

	// AllowedClientHeaders lists header patterns from the authz response
	// that are returned to the client when the authz service denies.
	AllowedClientHeaders []string `json:"allowedClientHeaders,omitempty" yaml:"allowedClientHeaders,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// ExtProc
// ────────────────────────────────────────────────────────────────────────────

// ExtProcConfig holds the configuration for the Envoy ext_proc HTTP filter.
type ExtProcConfig struct {
	// DestinationID references the Destination entity that hosts the
	// external processing service. Must be a gRPC service.
	DestinationID string `json:"destinationId" yaml:"destinationId"`

	// Timeout is the processing request deadline (e.g. "2s").
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// ProcessingMode controls which parts of the HTTP transaction are sent to
	// the external processor.
	ProcessingMode *ExtProcMode `json:"processingMode,omitempty" yaml:"processingMode,omitempty"`
}

// ExtProcPhase describes how ext_proc handles a specific message phase.
type ExtProcPhase string

const (
	// ExtProcPhaseSkip instructs ext_proc to skip this message phase entirely.
	ExtProcPhaseSkip ExtProcPhase = "SKIP"

	// ExtProcPhaseSend instructs ext_proc to send this message to the processor.
	ExtProcPhaseSend ExtProcPhase = "SEND"

	// ExtProcPhaseBuffered instructs ext_proc to buffer and send the full body.
	ExtProcPhaseBuffered ExtProcPhase = "BUFFERED"
)

// ExtProcMode describes which parts of the HTTP transaction ext_proc processes.
type ExtProcMode struct {
	// RequestHeaderMode controls processing of request headers.
	RequestHeaderMode ExtProcPhase `json:"requestHeaderMode,omitempty" yaml:"requestHeaderMode,omitempty"`

	// ResponseHeaderMode controls processing of response headers.
	ResponseHeaderMode ExtProcPhase `json:"responseHeaderMode,omitempty" yaml:"responseHeaderMode,omitempty"`

	// RequestBodyMode controls processing of the request body.
	RequestBodyMode ExtProcPhase `json:"requestBodyMode,omitempty" yaml:"requestBodyMode,omitempty"`

	// ResponseBodyMode controls processing of the response body.
	ResponseBodyMode ExtProcPhase `json:"responseBodyMode,omitempty" yaml:"responseBodyMode,omitempty"`
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

// HeadersConfig holds the configuration for the Envoy header mutation filter.
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

	// ExtAuthzContextExtensions adds key/value pairs to the ext_authz check request.
	// Only meaningful when the referenced filter is of type "extAuthz".
	ExtAuthzContextExtensions map[string]string `json:"extAuthzContextExtensions,omitempty" yaml:"extAuthzContextExtensions,omitempty"`



	// Headers overrides header manipulation for this route/group.
	// Only meaningful when the referenced filter is of type "headers".
	Headers *HeadersConfig `json:"headers,omitempty" yaml:"headers,omitempty"`
}
