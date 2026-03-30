# Envoy xDS Control Plane — Design & Status

**Branch**: `feat/envoy-xds-control-plane`
**Date**: 2026-03-30
**Status**: xDS translator core complete, model field gaps remain

---

## What this branch does

Replaces the native Go proxy with Envoy as the data plane.
Vrata becomes a pure control plane that:

1. Exposes the same REST API (no API changes for users).
2. Translates Vrata model entities → Envoy xDS resources.
3. Pushes xDS snapshots to a fleet of Envoy instances via ADS (gRPC).
4. Provides Go filter extensions for features Envoy doesn't have natively.

---

## Architecture

```
┌─────────────────────────────────────────┐
│  Vrata Control Plane (server/)          │
│                                         │
│  REST API (:8080)  ←→  Store (BoltDB)   │
│       ↓                                 │
│  Gateway (store events → xDS push)      │
│       ↓                                 │
│  xDS Server (:18000) — ADS/gRPC         │
└──────────────┬──────────────────────────┘
               │  xDS (ADS)
    ┌──────────┴──────────┐
    │  Envoy fleet        │
    │  (DaemonSet/Gateway)│
    │  + Go filter .so    │
    └─────────────────────┘
```

---

## Model change: Listener.GroupIDs (pending redesign)

The only model change vs main branch: `model.Listener` gains a `GroupIDs []string` field.

- Empty = attach all groups (catch-all, same behaviour as native proxy).
- Non-empty = only the listed groups are attached to this listener.

This is needed because Envoy requires explicit routing topology (Listener → RouteConfiguration → VirtualHost), while the native proxy inferred it at runtime.

**Open issue**: the owner considers routes (not just groups) as first-class citizens,
so `GroupIDs` on Listener doesn't feel right. This needs redesigning before stabilising
the API. Possible directions: attach routes directly to listeners, or rethink the
listener→group→route topology entirely. Do not invest in GroupIDs-dependent features
until this is resolved.

---

## xDS Translation

| Vrata entity | Envoy resource |
|---|---|
| `Listener` (with `GroupIDs`) | `envoy.Listener` + HCM filter + RDS |
| `Listener.TLS` | `DownstreamTlsContext` on filter chain |
| `Listener.TLS.ClientAuth` | mTLS with `require_client_certificate` + CA validation |
| `Listener.Timeouts` | HCM `RequestHeadersTimeout`, `RequestTimeout`, `StreamIdleTimeout` |
| `RouteGroup` + `Route` | `RouteConfiguration` with `VirtualHost` per group |
| `Destination` (with Endpoints) | `Cluster` (EDS/STATIC/STRICT_DNS auto-derived) + `ClusterLoadAssignment` |
| `Destination.Options.TLS` | `UpstreamTlsContext` on cluster transport socket |
| `Destination.Options.EndpointBalancing` | Cluster `LbPolicy` (ROUND_ROBIN, LEAST_REQUEST, RING_HASH, MAGLEV, RANDOM) |
| `Destination.Options.CircuitBreaker` | Cluster `CircuitBreakers.Thresholds` |
| `Destination.Options.OutlierDetection` | Cluster `OutlierDetection` |
| `Destination.Options.HealthCheck` | Cluster `HealthChecks` (HTTP) |
| `Destination.Options.HTTP2` | Cluster `Http2ProtocolOptions` |
| Multiple `DestinationRef` with weights | `WeightedCluster` in route action |
| `Match.Path` / `Match.PathPrefix` / `Match.PathRegex` | `RouteMatch` (exact / prefix / SafeRegex RE2) |
| `Match.Headers` (exact, regex, presence) | Envoy `HeaderMatcher` (all three modes) |
| `Match.Methods` | `:method` header matchers |
| `Match.QueryParams` (exact, regex, presence) | Envoy `QueryParameterMatcher` (all three modes) |
| `Match.GRPC` | `content-type: application/grpc` prefix header matcher |
| `Match.Hostnames` | Merged into VirtualHost domains via `mergeHostnames()` |
| `Forward.Timeouts.Request` | Route-level timeout |
| `Forward.Retry` | Route `RetryPolicy` (conditions, backoff, per-attempt timeout) |
| `Forward.Rewrite` | Route `PrefixRewrite` / `RegexRewrite` / host rewrite |
| `Forward.Mirror` | Route `RequestMirrorPolicies` |
| `Forward.DestinationBalancing` (WCH/STICKY) | Route `HashPolicy` (cookie-based) |
| `Route.Redirect` | Route `RedirectAction` |
| `Route.DirectResponse` | Route `DirectResponseAction` |
| Middleware `cors` | `envoy.filters.http.cors` (native) |
| Middleware `jwt` | `envoy.filters.http.jwt_authn` (native) |
| Middleware `extAuthz` | `envoy.filters.http.ext_authz` (native, HTTP + gRPC) |
| Middleware `extProc` | `envoy.filters.http.ext_proc` (native, gRPC, phases) |
| Middleware `rateLimit` | `envoy.filters.http.local_ratelimit` (native) |
| Middleware `headers` | `envoy.filters.http.header_mutation` (native) |
| Middleware `accessLog` | HCM `access_log` with `envoy.access_loggers.file` |
| Middleware `inlineAuthz` | Go plugin `vrata.inlineauthz` |
| Sticky (auto on STICKY routes) | Go plugin `vrata.sticky` (request + response side) |
| xfcc (auto on mTLS) | Go plugin `vrata.xfcc` |

---

## Go Filter Extensions (extensions/)

Each extension is an independent Go module compiled as `plugin` (.so).
Loaded by Envoy via `envoy.filters.http.dynamic_modules` filter.

### extensions/sticky/
Redis-backed sticky session routing.
- **Request side** (`DecodeHeaders`): reads session cookie, looks up pinned upstream in Redis, calls `SetUpstreamOverrideHost`.
- **Response side** (`EncodeHeaders`): on first response for unpinned session, reads upstream Envoy selected via `StreamInfo().UpstreamRemoteAddress()`, writes pin to Redis async.
- Config via env vars: `VRATA_STICKY_REDIS_ADDR`, `VRATA_STICKY_COOKIE_NAME`, `VRATA_STICKY_TTL_SECONDS`, `VRATA_STICKY_STRICT`.

### extensions/inlineauthz/
CEL-based authorization evaluated locally. Equivalent to Vrata's `inlineAuthz`
middleware. Lazy body buffering: only buffers when a rule references `request.body`.
Config via `VRATA_AUTHZ_RULES_JSON` (JSON array of `{cel, action}` rules).

### extensions/xfcc/
X-Forwarded-Client-Cert injection with spoof protection. Strips incoming XFCC,
injects new one from Envoy's verified TLS client cert metadata.
No configuration required.

---

## Building extensions

```bash
# Build all extensions as .so plugins
cd extensions/sticky   && go build -buildmode=plugin -o sticky.so .
cd extensions/inlineauthz && go build -buildmode=plugin -o inlineauthz.so .
cd extensions/xfcc     && go build -buildmode=plugin -o xfcc.so .

# Or build the Envoy image with extensions baked in
docker build -t vrata-envoy -f extensions/Dockerfile .
```

---

## Done

### Control plane infrastructure
- [x] ADS gRPC server on `:18000` (`discoveryv3.RegisterAggregatedDiscoveryServiceServer`)
- [x] Snapshot cache with `IDHash` and auto-increment versioning
- [x] Gateway: store events → full xDS snapshot rebuild → push
- [x] k8s EndpointSlice watcher → endpoint merge into destinations
- [x] Envoy bootstrap config example (`extensions/ENVOY_BOOTSTRAP.md`)

### Cluster / Destination translation
- [x] Cluster type auto-derived: IP → STATIC, hostname → STRICT_DNS, k8s discovery → EDS
- [x] Endpoints populated from `Destination.Endpoints` → `ClusterLoadAssignment`
- [x] Connect timeout (`Options.Timeouts.Connect`, default 2s)
- [x] LB policy: ROUND_ROBIN, LEAST_REQUEST, RING_HASH, MAGLEV, RANDOM
- [x] Ring hash config (min/max ring size)
- [x] Maglev config (table size)
- [x] Circuit breaker (max connections, pending, requests, retries)
- [x] Outlier detection (consecutive 5xx, gateway errors, interval, ejection)
- [x] Active health check (HTTP, interval, timeout, thresholds)
- [x] Upstream TLS (mode, SNI, CA file, client cert for mTLS)
- [x] HTTP/2 upstream (`Http2ProtocolOptions`)
- [x] Max requests per connection

### Route match translation
- [x] Exact path match (`RouteMatch_Path`)
- [x] Prefix path match (`RouteMatch_Prefix`)
- [x] Regex path match (`RouteMatch_SafeRegex`, RE2)
- [x] Group `PathPrefix` composition (prepended to route path)
- [x] Header matchers — exact, regex (SafeRegex RE2), presence (all three modes)
- [x] Method matchers (`:method` header matcher per method)
- [x] Query param matchers — exact, regex, presence (all three modes)
- [x] gRPC content-type match (`content-type: application/grpc` prefix header)
- [x] Hostname match — group + route hostnames merged into VirtualHost domains

### Route action — Forward
- [x] Single destination forward (`RouteAction_Cluster`)
- [x] Weighted multi-destination (`RouteAction_WeightedClusters`)
- [x] Request timeout (`RouteAction.Timeout`)
- [x] Retry policy (attempts, conditions, backoff base+max, per-attempt timeout)
- [x] URL rewrite — prefix (`PrefixRewrite`)
- [x] URL rewrite — regex (`RegexRewrite` with RE2)
- [x] URL rewrite — host literal, host from header, auto host rewrite
- [x] Traffic mirror (`RequestMirrorPolicies` with runtime fraction percentage)
- [x] Hash policy — WCH cookie (`RouteAction_HashPolicy_Cookie`)
- [x] Hash policy — STICKY cookie (same mechanism)

### Route action — Redirect
- [x] Scheme, host, path redirect
- [x] Strip query
- [x] Response codes 301/302/303/307/308

### Route action — Direct response
- [x] Status code + inline body

### Listener translation
- [x] Address + port (`SocketAddress`)
- [x] TLS termination (`DownstreamTlsContext` with cert/key files)
- [x] TLS min/max version (`TlsParameters`)
- [x] mTLS client auth (`RequireClientCertificate` + CA validation)
- [x] `Timeouts.ClientHeader` → HCM `RequestHeadersTimeout`
- [x] `Timeouts.ClientRequest` → HCM `RequestTimeout`
- [x] `Timeouts.IdleBetweenRequests` → HCM `StreamIdleTimeout`
- [x] `GroupIDs` → selective VirtualHost attachment (empty = all groups)

### Middleware translation
- [x] CORS → `envoy.filters.http.cors` (origins exact + regex, methods, headers, expose headers)
- [x] JWT → `envoy.filters.http.jwt_authn` (local JWKS + remote JWKS, issuer, audiences)
- [x] ExtAuthz → `envoy.filters.http.ext_authz` (HTTP and gRPC modes, timeout, failure mode)
- [x] ExtProc → `envoy.filters.http.ext_proc` (gRPC, phases request/response headers/body, timeout, failure mode)
- [x] RateLimit → `envoy.filters.http.local_ratelimit` (token bucket: RPS + burst)
- [x] Headers → `envoy.filters.http.header_mutation` (request + response add/remove)
- [x] AccessLog → HCM `access_log` with `envoy.access_loggers.file` (JSON + text, variable mapping)
- [x] InlineAuthz → Go plugin `vrata.inlineauthz` (auto-injected for `inlineAuthz` type)
- [x] XFCC → Go plugin `vrata.xfcc` (auto-injected when listener has mTLS)
- [x] Sticky → Go plugin `vrata.sticky` (auto-injected when any route uses STICKY destination balancing)
- [x] Router filter always last in chain

### Go filter extensions
- [x] `vrata.sticky` — request-side: Redis lookup + `SetUpstreamOverrideHost`
- [x] `vrata.sticky` — response-side: `UpstreamRemoteAddress` → Redis write (async)
- [x] `vrata.sticky` — auto-injected in HCM when routes have STICKY balancing
- [x] `vrata.inlineauthz` — CEL evaluation with header + body access, lazy body buffering
- [x] `vrata.xfcc` — strip incoming XFCC + inject from TLS metadata
- [x] All three: filter factory registration via `init()`

---

## Pending — Model fields not yet translated to xDS

These fields exist in the model and are accepted by the REST API, but `internal/xds/`
does not translate them into Envoy config. Grouped by priority.

### Cluster / Destination gaps

| Model field | Envoy equivalent | Notes |
|---|---|---|
| `Options.Timeouts.Request` | Not directly on Cluster — route-level handles this | Evaluate if needed |
| `Options.Timeouts.IdleConnection` | `Cluster.TypedExtensionProtocolOptions` idle timeout | |
| `Options.Timeouts.ResponseHeader` | No direct Envoy cluster equivalent | Native proxy concept |
| `Options.Timeouts.TLSHandshake` | No direct Envoy cluster equivalent | Native proxy concept |
| `Options.Timeouts.DualStackFallback` | No direct Envoy cluster equivalent | Native proxy concept |
| `Options.Timeouts.ExpectContinue` | No direct Envoy cluster equivalent | Native proxy concept |
| `Options.EndpointBalancing.LeastRequest.ChoiceCount` | `Cluster.LeastRequestLbConfig.ChoiceCount` | Easy wire |
| `Options.EndpointBalancing.RingHash.HashPolicy` | `Cluster.LbConfig` hash policy entries | |
| `Options.EndpointBalancing.Maglev.HashPolicy` | `Cluster.LbConfig` hash policy entries | |
| `Options.EndpointBalancing.Sticky` (endpoint-level) | Go plugin or custom cluster config | |
| `Options.CircuitBreaker.FailureThreshold` | No direct Envoy equivalent — use outlier detection instead | |
| `Options.CircuitBreaker.OpenDuration` | No direct Envoy equivalent — use outlier detection instead | |

### Route gaps

| Model field | Envoy equivalent | Notes |
|---|---|---|
| `Forward.MaxGRPCTimeout` | `RouteAction.MaxGrpcTimeout` | Easy wire |
| `Forward.Retry.RetriableCodes` | `RetryPolicy.RetriableStatusCodes` | Easy wire (list of uint32) |
| `Redirect.URL` (full URL) | Decompose into scheme+host+path or Envoy has no single-URL redirect | |
| `Match.CEL` | `envoy.filters.http.rbac` with CEL, or Go plugin | Architecture decision needed |
| `RouteGroup.PathRegex` composition | Compose regex patterns per model doc rules | Currently only `PathPrefix` used |
| `RouteGroup.RetryDefault` | `VirtualHost.RetryPolicy` | Easy wire |
| `RouteGroup.IncludeAttemptCount` | `VirtualHost.IncludeRequestAttemptCount` | Easy wire |
| Route-level `MiddlewareIDs` | Per-route filter config in Envoy | Currently only group-level middlewares |
| `MiddlewareOverrides` (route + group) | Per-route typed filter config override | Complex — needs per-route config on each filter |
| `MiddlewareOverride.Disabled` | Disable filter per-route | |
| `MiddlewareOverride.SkipWhen` / `OnlyWhen` | CEL conditions — no direct Envoy equivalent | |
| `Route.OnError` | Not applicable in Envoy (retry + circuit breaking + outlier cover it) | Model preserved for API compat |

### Listener gaps

| Model field | Envoy equivalent | Notes |
|---|---|---|
| `HTTP2` (h2c) | HCM `codec_type: AUTO` or `HTTP2` + listener `enable_h2c` | |
| `ServerName` | HCM `server_name` | Easy wire |
| `MaxRequestHeadersKB` | HCM `max_request_headers_kb` | Easy wire |
| `Timeouts.ClientResponse` | No direct HCM equivalent | Native proxy concept |
| `Metrics` | Envoy has native stats — model `Metrics` was for native proxy | Likely not applicable |

### Middleware field gaps

| Middleware | Field | Envoy equivalent | Notes |
|---|---|---|---|
| CORS | `MaxAge` | `CorsPolicy.MaxAge` | Easy wire |
| CORS | `AllowCredentials` | `CorsPolicy.AllowCredentials` | Easy wire |
| JWT | `JWKsRetrievalTimeout` | `RemoteJwks.HttpUri.Timeout` | Currently hardcoded 5s |
| JWT | `ForwardJWT` | `JwtProvider.ForwardPayloadHeader` or `forward` | |
| JWT | `ClaimToHeaders` | `JwtProvider.ClaimToHeaders` | |
| JWT | `AssertClaims` | Requires JWT + inlineAuthz CEL combo | Complex |
| ExtAuthz | `IncludeBody` | `HttpService.AuthorizationRequest.AllowedHeaders` + body | |
| ExtAuthz | `OnCheck.ForwardHeaders` | `HttpService.AuthorizationRequest.AllowedHeaders` | |
| ExtAuthz | `OnCheck.InjectHeaders` | `HttpService.AuthorizationRequest.HeadersToAdd` | |
| ExtAuthz | `OnAllow.CopyToUpstream` | `HttpService.AuthorizationResponse.AllowedUpstreamHeaders` | |
| ExtAuthz | `OnDeny.CopyToClient` | `HttpService.AuthorizationResponse.AllowedClientHeaders` | |
| ExtProc | `Mode` (http) | Only gRPC supported by Envoy ext_proc | gRPC only |
| ExtProc | `StatusOnError` | `ExternalProcessor.StatusOnError` | Easy wire |
| ExtProc | `AllowedMutations` | `ExternalProcessor.MutationRules` | |
| ExtProc | `ForwardRules` | `ExternalProcessor.ForwardRules` (Envoy proto) | |
| ExtProc | `DisableReject` | `ExternalProcessor.DisableClearRouteCache` (partial) | Not exact match |
| ExtProc | `ObserveMode` | `ExternalProcessor.ObservabilityMode` | Envoy 1.29+ |
| ExtProc | `MetricsPrefix` | `ExternalProcessor.StatPrefix` | Easy wire |
| ExtProc | `Phases.MaxBodyBytes` | `ProcessingMode.MaxMessageBodySize` | |
| RateLimit | `TrustedProxies` | No direct Envoy local_ratelimit equivalent | Would need XFF parsing |
| Headers | `Append` flag per header | `HeaderValueOption.AppendAction` | Currently always APPEND_IF_EXISTS_OR_ADD |

---

## Pending — Architecture / infrastructure

| Item | Notes |
|---|---|
| **Listener.GroupIDs redesign** | Owner considers routes first-class; GroupIDs on Listener doesn't fit. Needs rethinking before stabilising API. |
| **CEL route matching** | `MatchRule.CEL` exists in model, no xDS translation. Options: `envoy.filters.http.rbac` with CEL conditions, or custom Go plugin. Architecture decision needed. |
| **xDS translator unit tests** | All translation functions in `internal/xds/` are untested. |
| **E2E tests with Envoy** | No test infrastructure that starts Envoy + control plane together. |
| **TLS on xDS gRPC channel** | Currently plaintext gRPC between control plane and Envoy. |
| **Per-fleet xDS** | Single wildcard node ID `""` — all Envoys get the same config. No multi-fleet support. |
| **Control plane metrics** | No Prometheus endpoint for xDS push count, latency, errors. |
| **extensions go mod tidy** | All three extension modules need `go mod tidy` (requires network access). |

---

## Discarded — not applicable in Envoy

| Model field | Reason |
|---|---|
| `Match.CEL` (as native route matcher) | Envoy route matching is declarative, no runtime CEL. Must use RBAC filter or Go plugin instead. |
| `MatchRule.Ports` | Port is a Listener property in Envoy, not a route matcher. |
| `MiddlewareOverride.SkipWhen` / `OnlyWhen` | Envoy filters run unconditionally; conditional execution requires RBAC or inlineAuthz. |
| `Route.OnError` fallback routes | Complex in Envoy, covered by retry + circuit breaking + outlier detection. Model preserved for API compat. |
| `Destination.Options.Timeouts.DualStackFallback` | Native proxy concept (Happy Eyeballs). Envoy handles DNS resolution differently. |
| `Destination.Options.Timeouts.TLSHandshake` | No separate TLS handshake timeout in Envoy — `ConnectTimeout` covers it. |
| `Destination.Options.Timeouts.ExpectContinue` | No Envoy equivalent. |
| `Destination.Options.Timeouts.ResponseHeader` | No direct cluster-level equivalent. Route timeout covers the use case. |
| `Listener.Metrics` | Envoy has native stats system. Model `Metrics` was for the native Go proxy. |
