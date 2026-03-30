# xDS Translator Audit Report

---

## Audit #2 — 2026-03-30

**Branch**: `feat/envoy-xds-control-plane`
**Scope**: Full empirical audit — every model field checked against `internal/xds/` code.
**Method**: Line-by-line inspection of `server.go`, `middlewares.go`, `helpers.go`, `extensions/`, and all `model/` types.

---

## Legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Implemented and wired in the translator |
| ❌ | Model field exists but not translated to xDS |
| 🚫 | Discarded — not applicable in Envoy, or native proxy concept |
| ⚠️ | Partially implemented or known caveat |

---

## 1. Cluster / Destination translation

| Feature | Status | Code location |
|---------|--------|---------------|
| Cluster type auto-derived (EDS / STATIC / STRICT_DNS) | ✅ | server.go:825-833 `clusterTypeFor()` |
| Endpoints populated from `Destination.Endpoints` | ✅ | server.go:131-153 `ClusterLoadAssignment` |
| Connect timeout (`Options.Timeouts.Connect`) | ✅ | server.go:214-223 (default 2s) |
| LB policy ROUND_ROBIN | ✅ | server.go:842 |
| LB policy LEAST_REQUEST | ✅ | server.go:844 |
| LB policy RING_HASH | ✅ | server.go:846 |
| LB policy MAGLEV | ✅ | server.go:848 |
| LB policy RANDOM | ✅ | server.go:850 |
| Circuit breaker (max connections, pending, requests, retries) | ✅ | server.go:167-185 |
| Outlier detection (consecutive 5xx, gateway errors, interval, ejection) | ✅ | server.go:188-211 |
| Active health check (HTTP) | ✅ | server.go:246-248, 901-938 |
| Upstream TLS (mode, SNI, CA file, client cert) | ✅ | server.go:226-233, 861-898 |
| HTTP/2 upstream | ✅ | server.go:236-238 |
| Max requests per connection | ✅ | server.go:241-243 |
| Ring hash config (min/max ring size) | ✅ | server.go:253-261 |
| Maglev config (table size) | ✅ | server.go:263-269 |
| `Options.Timeouts.Request` (total upstream) | ❌ | Field exists, not wired |
| `Options.Timeouts.IdleConnection` | ❌ | Field exists, not wired |
| `Options.Timeouts.ResponseHeader` | 🚫 | No Envoy cluster equivalent |
| `Options.Timeouts.TLSHandshake` | 🚫 | Envoy `ConnectTimeout` covers this |
| `Options.Timeouts.DualStackFallback` | 🚫 | Native proxy concept (Happy Eyeballs) |
| `Options.Timeouts.ExpectContinue` | 🚫 | No Envoy equivalent |
| `Options.EndpointBalancing.LeastRequest.ChoiceCount` | ❌ | Field exists, not wired to `LeastRequestLbConfig` |
| `Options.EndpointBalancing.RingHash.HashPolicy` | ❌ | Field exists, not wired to cluster hash policy |
| `Options.EndpointBalancing.Maglev.HashPolicy` | ❌ | Field exists, not wired |
| `Options.EndpointBalancing.Sticky` (endpoint-level) | ❌ | Field exists, no translation |
| `Options.CircuitBreaker.FailureThreshold` | 🚫 | No direct Envoy equivalent — use outlier detection |
| `Options.CircuitBreaker.OpenDuration` | 🚫 | No direct Envoy equivalent — use outlier detection |

---

## 2. Route match translation

| Feature | Status | Code location |
|---------|--------|---------------|
| Exact path match | ✅ | server.go:562 |
| Prefix path match | ✅ | server.go:564 |
| Regex path match (RE2) | ✅ | server.go:566-571 |
| Group `PathPrefix` composition | ✅ | server.go:558 |
| Header matchers — exact | ✅ | server.go:1011-1014 |
| Header matchers — regex (SafeRegex RE2) | ✅ | server.go:990-1003 |
| Header matchers — presence | ✅ | server.go:1005-1009 |
| Method matchers | ✅ | server.go:588-595 |
| Query param matchers (exact, regex, presence) | ✅ | server.go:598-631 |
| gRPC content-type match | ✅ | server.go:634-643 |
| Hostname match (group + route merged) | ✅ | server.go:344, 953-982 `mergeHostnames()` |
| `Match.CEL` | ❌ | Field exists, 0 code in xDS |
| Group `PathRegex` composition | ❌ | Model documents composition rules, but `buildRouteMatch` only uses `g.PathPrefix` |
| Port matchers (`Match.Ports`) | 🚫 | Port is a Listener property in Envoy |

---

## 3. Route action — Forward

| Feature | Status | Code location |
|---------|--------|---------------|
| Single destination forward | ✅ | server.go:422-429 |
| Weighted multi-destination | ✅ | server.go:431-445 |
| Request timeout | ✅ | server.go:448-452 |
| Per-attempt timeout | ✅ | server.go:708-712 |
| Retry (attempts, conditions, backoff base+max) | ✅ | server.go:703-733 |
| URL rewrite — prefix | ✅ | server.go:759-761 |
| URL rewrite — regex | ✅ | server.go:763-770 |
| URL rewrite — host literal | ✅ | server.go:773 |
| URL rewrite — host from header | ✅ | server.go:775 |
| URL rewrite — auto host | ✅ | server.go:777 |
| Traffic mirror | ✅ | server.go:465-481 |
| Hash policy — WCH cookie | ✅ | server.go:487-503 |
| Hash policy — STICKY cookie | ✅ | server.go:504-521 |
| `Forward.MaxGRPCTimeout` | ❌ | Field exists, not wired to `RouteAction.MaxGrpcTimeout` |
| `Forward.Retry.RetriableCodes` | ❌ | Field exists, not wired to `RetryPolicy.RetriableStatusCodes` |
| `Redirect.URL` (full URL) | ❌ | Field exists, not translated |
| `Route.OnError` fallback routes | 🚫 | Not applicable — Envoy uses retry + circuit breaking + outlier detection |

---

## 4. Route action — Redirect

| Feature | Status | Code location |
|---------|--------|---------------|
| Scheme redirect | ✅ | server.go:790 |
| Host redirect | ✅ | server.go:793 |
| Path redirect | ✅ | server.go:796 |
| Strip query | ✅ | server.go:799 |
| Response codes 301/302/303/307/308 | ✅ | server.go:802-815 |

---

## 5. Route action — Direct response

| Feature | Status | Code location |
|---------|--------|---------------|
| Status code + inline body | ✅ | server.go:533-543 |

---

## 6. Listener translation

| Feature | Status | Code location |
|---------|--------|---------------|
| Address + port | ✅ | server.go:685-696 |
| TLS termination (cert + key files) | ✅ | server.go:666-683, helpers.go:74-107 |
| TLS min/max version | ✅ | helpers.go:110-131 |
| mTLS client auth | ✅ | helpers.go:91-99 |
| `Timeouts.ClientHeader` → `RequestHeadersTimeout` | ✅ | helpers.go:46-49 |
| `Timeouts.ClientRequest` → `RequestTimeout` | ✅ | helpers.go:50-53 |
| `Timeouts.IdleBetweenRequests` → `StreamIdleTimeout` | ✅ | helpers.go:54-58 |
| `GroupIDs` → selective VirtualHost attachment | ✅ | server.go:313-322 |
| `HTTP2` (h2c) | ❌ | Field exists, not wired to HCM codec_type |
| `ServerName` | ❌ | Field exists, not set in HCM |
| `MaxRequestHeadersKB` | ❌ | Field exists, not wired to HCM |
| `Timeouts.ClientResponse` | 🚫 | No direct HCM equivalent |
| `Metrics` | 🚫 | Envoy has native stats — model `Metrics` was for native proxy |

---

## 7. RouteGroup translation

| Feature | Status | Code location |
|---------|--------|---------------|
| `PathPrefix` composition | ✅ | server.go:558 |
| `Hostnames` → VirtualHost domains | ✅ | server.go:964-966 |
| `Headers` → merged header matchers | ✅ | server.go:582 |
| `MiddlewareIDs` → filter lookup | ✅ | server.go:358-370 |
| `PathRegex` → regex composition | ❌ | Model documents rules, `buildRouteMatch` only uses `g.PathPrefix` |
| `RetryDefault` → VirtualHost `RetryPolicy` | ❌ | Field exists, not wired |
| `IncludeAttemptCount` → VirtualHost `IncludeRequestAttemptCount` | ❌ | Field exists, not wired |
| `MiddlewareOverrides` (group-level) | ❌ | Field exists, not translated to per-route config |
| Route-level `MiddlewareIDs` | ❌ | Field exists on Route, xDS only reads `g.MiddlewareIDs` |
| Route-level `MiddlewareOverrides` | ❌ | Field exists, not translated |

---

## 8. Middleware translation

| Middleware | Status | Code location | Gaps |
|-----------|--------|---------------|------|
| CORS | ⚠️ | middlewares.go:92-123 | `MaxAge`, `AllowCredentials` not wired |
| JWT | ⚠️ | middlewares.go:129-180 | `JWKsRetrievalTimeout` hardcoded 5s, `ForwardJWT`, `ClaimToHeaders`, `AssertClaims` not wired |
| ExtAuthz | ⚠️ | middlewares.go:186-239 | `IncludeBody`, `OnCheck`, `OnAllow`, `OnDeny` not wired |
| ExtProc | ⚠️ | middlewares.go:477-542 | `Mode` (http), `StatusOnError`, `AllowedMutations`, `ForwardRules`, `DisableReject`, `ObserveMode`, `MetricsPrefix`, `Phases.MaxBodyBytes` not wired |
| RateLimit | ⚠️ | middlewares.go:245-273 | `TrustedProxies` not wired |
| Headers | ⚠️ | middlewares.go:300-353 | `Append` flag ignored — always APPEND_IF_EXISTS_OR_ADD |
| AccessLog | ✅ | middlewares.go:361-414 | Complete |
| InlineAuthz | ✅ | middlewares.go:72 | Go plugin — complete |
| XFCC | ✅ | middlewares.go:40-44 | Auto on mTLS — complete |
| Sticky | ✅ | middlewares.go:48-52 | Auto on STICKY — complete |
| Router | ✅ | middlewares.go:79-83 | Always last — complete |

---

## 9. Go filter extensions

| Extension | Request-side | Response-side | Auto-injected | Code location |
|-----------|-------------|---------------|---------------|---------------|
| `vrata.sticky` | ✅ `SetUpstreamOverrideHost` | ✅ `UpstreamRemoteAddress` → Redis write (async) | ✅ When any route has STICKY | extensions/sticky/filter.go |
| `vrata.inlineauthz` | ✅ CEL + lazy body buffer | — | ✅ When middleware type `inlineAuthz` | extensions/inlineauthz/filter.go |
| `vrata.xfcc` | ✅ Strip + inject from TLS metadata | — | ✅ When listener has mTLS | extensions/xfcc/filter.go |

---

## 10. Control plane infrastructure

| Feature | Status | Notes |
|---------|--------|-------|
| ADS gRPC server on `:18000` | ✅ | server.go:54-80 |
| Snapshot cache (IDHash) | ✅ | server.go:48 |
| Gateway: store events → xDS push | ✅ | gateway.go:51-88 |
| K8s EndpointSlice watcher → endpoint merge | ✅ | gateway.go:119-126 |
| REST API unchanged | ✅ | Same handlers, same routes |
| BoltDB + Raft store | ✅ | Unchanged from main branch |
| Snapshot versioning (auto-increment) | ✅ | server.go:92 `atomic.Int64` |
| Per-fleet xDS (multiple Envoy fleets) | ❌ | Single wildcard node ID `""` |
| TLS on xDS gRPC channel | ❌ | Currently plaintext gRPC |
| Control plane Prometheus metrics | ❌ | No metrics endpoint |
| xDS translator unit tests | ❌ | All translation functions untested |
| E2E tests with real Envoy | ❌ | No test infrastructure |

---

## 11. Summary

**Total model fields audited**: ~120
**Fully translated to xDS**: ~70%
**Partially translated (core wired, advanced fields missing)**: ~10%
**Not translated (field exists, no xDS code)**: ~12%
**Discarded (not applicable in Envoy)**: ~8%

### Quick wins (easy to wire)

1. `CORS.MaxAge` → `CorsPolicy.MaxAge`
2. `CORS.AllowCredentials` → `CorsPolicy.AllowCredentials`
3. `JWT.JWKsRetrievalTimeout` → use field instead of hardcoded 5s
4. `ExtProc.StatusOnError` → `ExternalProcessor.StatusOnError`
5. `ExtProc.MetricsPrefix` → `ExternalProcessor.StatPrefix`
6. `Forward.MaxGRPCTimeout` → `RouteAction.MaxGrpcTimeout`
7. `Forward.Retry.RetriableCodes` → `RetryPolicy.RetriableStatusCodes`
8. `Listener.ServerName` → HCM `server_name`
9. `Listener.MaxRequestHeadersKB` → HCM `max_request_headers_kb`
10. `RouteGroup.RetryDefault` → `VirtualHost.RetryPolicy`
11. `RouteGroup.IncludeAttemptCount` → `VirtualHost.IncludeRequestAttemptCount`
12. `EndpointBalancing.LeastRequest.ChoiceCount` → `LeastRequestLbConfig.ChoiceCount`
13. `Headers.Append` flag → `HeaderValueOption.AppendAction`

### Architecture decisions needed

1. `MatchRule.CEL` → RBAC filter or Go plugin?
2. `RouteGroup.PathRegex` composition → implement the documented composition rules
3. Route-level `MiddlewareIDs` + `MiddlewareOverrides` → per-route filter config in Envoy
4. `Listener.GroupIDs` redesign — routes as first-class citizens
5. `ExtProc.Mode: "http"` → Envoy ext_proc is gRPC-only; http mode needs removal or adapter

---

## Audit #1 — 2026-03-29 (superseded)

First audit was based on incomplete code inspection and had several items incorrectly
listed as pending that were actually implemented. Audit #2 supersedes it entirely.
