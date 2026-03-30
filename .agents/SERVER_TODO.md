# Server TODO — Vrata (xDS Branch)

## Pending

### Quick wins — model fields not yet wired to xDS

- [ ] **CORS `MaxAge` + `AllowCredentials`** — wire to `CorsPolicy.MaxAge` and `CorsPolicy.AllowCredentials`
- [ ] **JWT `JWKsRetrievalTimeout`** — use model field instead of hardcoded 5s in `buildJWTFilter`
- [ ] **ExtProc `StatusOnError`** — wire to `ExternalProcessor.StatusOnError`
- [ ] **ExtProc `MetricsPrefix`** — wire to `ExternalProcessor.StatPrefix`
- [ ] **Forward `MaxGRPCTimeout`** — wire to `RouteAction.MaxGrpcTimeout`
- [ ] **Forward `Retry.RetriableCodes`** — wire to `RetryPolicy.RetriableStatusCodes`
- [ ] **Listener `ServerName`** — wire to HCM `server_name`
- [ ] **Listener `MaxRequestHeadersKB`** — wire to HCM `max_request_headers_kb`
- [ ] **RouteGroup `RetryDefault`** — wire to `VirtualHost.RetryPolicy`
- [ ] **RouteGroup `IncludeAttemptCount`** — wire to `VirtualHost.IncludeRequestAttemptCount`
- [ ] **EndpointBalancing `LeastRequest.ChoiceCount`** — wire to `LeastRequestLbConfig.ChoiceCount`
- [ ] **Headers `Append` flag** — respect per-header `Append` field instead of always using APPEND_IF_EXISTS_OR_ADD

### Middleware field gaps — medium effort

- [ ] **JWT `ForwardJWT`** — wire to `JwtProvider.Forward` or `ForwardPayloadHeader`
- [ ] **JWT `ClaimToHeaders`** — wire to `JwtProvider.ClaimToHeaders`
- [ ] **ExtAuthz `IncludeBody`** — wire request body forwarding to authz service
- [ ] **ExtAuthz `OnCheck.ForwardHeaders`** — wire to `AuthorizationRequest.AllowedHeaders`
- [ ] **ExtAuthz `OnCheck.InjectHeaders`** — wire to `AuthorizationRequest.HeadersToAdd`
- [ ] **ExtAuthz `OnAllow.CopyToUpstream`** — wire to `AuthorizationResponse.AllowedUpstreamHeaders`
- [ ] **ExtAuthz `OnDeny.CopyToClient`** — wire to `AuthorizationResponse.AllowedClientHeaders`
- [ ] **ExtProc `AllowedMutations`** — wire to `ExternalProcessor.MutationRules`
- [ ] **ExtProc `ForwardRules`** — wire to `ExternalProcessor.ForwardRules`
- [ ] **ExtProc `ObserveMode`** — wire to `ExternalProcessor.ObservabilityMode` (Envoy 1.29+)
- [ ] **ExtProc `Phases.MaxBodyBytes`** — wire to `ProcessingMode.MaxMessageBodySize`
- [ ] **Listener `HTTP2`** (h2c) — wire to HCM `codec_type` or listener `enable_h2c`

### Architecture decisions needed

- [ ] **CEL route matching** — `MatchRule.CEL` exists in model but has 0 xDS translation. Options: `envoy.filters.http.rbac` with CEL, or custom Go plugin.
- [ ] **RouteGroup `PathRegex` composition** — model documents regex composition rules, but `buildRouteMatch` only reads `g.PathPrefix`. Need to implement the documented composition.
- [ ] **Route-level `MiddlewareIDs` + `MiddlewareOverrides`** — xDS only reads group-level middlewares. Per-route filter config requires Envoy per-route typed config override.
- [ ] **Listener.GroupIDs redesign** — owner considers routes first-class, GroupIDs on Listener doesn't fit. Needs rethinking before stabilising API.
- [ ] **ExtProc `Mode: "http"`** — Envoy ext_proc is gRPC-only. Either remove http mode from model or build an adapter.
- [ ] **JWT `AssertClaims`** — would require JWT + inlineAuthz CEL combo. Complex.
- [ ] **`Redirect.URL`** (full URL redirect) — Envoy has no single-URL redirect, would need decomposition into scheme+host+path.
- [ ] **ExtProc `DisableReject`** — no exact Envoy equivalent.

### Housekeeping

- [ ] **xDS translator unit tests** — all translation functions in `internal/xds/` are untested.
- [ ] **E2E tests with Envoy** — no e2e test infrastructure that starts Envoy + control plane together.
- [ ] **Control plane metrics** — no Prometheus endpoint for xDS push counts, errors, latency.
- [ ] **TLS on xDS gRPC channel** — currently plaintext gRPC.
- [ ] **Per-fleet xDS** — single wildcard node ID, no multi-fleet support.
- [ ] **REST API authentication** — no auth on the control plane API.
- [ ] **extensions go mod tidy** — all three extension modules need `go mod tidy` (requires network access).

## Done

- [x] ADS gRPC server on `:18000`
- [x] Gateway → xDS push (store events → full snapshot rebuild)
- [x] Cluster builder — LB policy, circuit breaker, outlier detection, health checks, connect timeout, upstream TLS/mTLS, HTTP/2, max requests per connection, ring hash/maglev config
- [x] Route builder — forward (single/weighted), redirect, direct response, timeout, retry, rewrite (prefix/regex/host), mirror, hash policy (WCH/STICKY)
- [x] Route match — exact/prefix/regex path, headers (exact/regex/presence), methods, query params (exact/regex/presence), gRPC content-type, hostname merge
- [x] Listener builder — TLS termination, mTLS client auth, HCM with RDS, GroupIDs → selective VirtualHost, listener timeouts (ClientHeader, ClientRequest, IdleBetweenRequests)
- [x] CORS filter (native, origins exact + regex, methods, headers, expose headers)
- [x] JWT filter (native, local + remote JWKS, issuer, audiences)
- [x] ExtAuthz filter (native, HTTP + gRPC, timeout, failure mode)
- [x] ExtProc filter (native, gRPC, phases request/response headers/body, timeout, failure mode)
- [x] RateLimit filter (native, token bucket RPS + burst)
- [x] Headers filter (native, request + response add/remove)
- [x] AccessLog (HCM, file logger, JSON/text, Vrata→Envoy variable mapping)
- [x] InlineAuthz Go plugin (CEL evaluation, header + body access, lazy body buffering)
- [x] XFCC Go plugin (strip + inject, auto on mTLS)
- [x] Sticky Go plugin (request-side Redis lookup, response-side Redis write, auto-injected in HCM)
- [x] Envoy bootstrap example
- [x] Query param matchers (`QueryParameterMatcher` with exact, regex, presence)
- [x] gRPC content-type match (`content-type: application/grpc` prefix header matcher)
- [x] Listener timeouts in HCM (`RequestHeadersTimeout`, `RequestTimeout`, `StreamIdleTimeout`)
- [~] Port matchers — not applicable (port is a Listener property in Envoy)
