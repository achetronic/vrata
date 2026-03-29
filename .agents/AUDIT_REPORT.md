# xDS Translator Audit Report

---

## Audit #1 — 2026-03-29

**Branch**: `feat/envoy-xds-control-plane`
**Scope**: xDS translator completeness — what is implemented, what is missing, what is discarded.
**Method**: code inspection of `server/internal/xds/server.go`, `middlewares.go`, `helpers.go`, `extensions/`.

---

## Legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Implemented and wired in the translator |
| ❌ | Not implemented |
| 🚫 | Discarded — not applicable in Envoy, architectural mismatch |
| ⚠️ | Partially implemented or known caveat |

---

## 1. Cluster / Destination translation

| Feature | Status | Notes |
|---------|--------|-------|
| Cluster type auto-derived (EDS / STATIC / STRICT_DNS) | ✅ | `clusterTypeFor()`: IP → STATIC, hostname → STRICT_DNS, k8s discovery → EDS |
| Endpoints populated from `Destination.Endpoints` | ✅ | `ClusterLoadAssignment` built per destination |
| Connect timeout (`Options.Timeouts.Connect`) | ✅ | Default 2s, overridden by model field |
| LB policy ROUND_ROBIN | ✅ | `lbPolicyFor()` |
| LB policy LEAST_REQUEST | ✅ | `lbPolicyFor()` |
| LB policy RING_HASH | ✅ | `lbPolicyFor()` + ring size config |
| LB policy MAGLEV | ✅ | `lbPolicyFor()` + table size config |
| LB policy RANDOM | ✅ | `lbPolicyFor()` |
| Circuit breaker (max connections, pending, requests, retries) | ✅ | `CircuitBreakers.Thresholds` |
| Outlier detection (consecutive 5xx, gateway errors, interval, base ejection time, max ejection %) | ✅ | `OutlierDetection` |
| Active health check (HTTP) | ✅ | `buildHealthChecks()` |
| Upstream TLS (mode, SNI, CA file, client cert) | ✅ | `buildUpstreamTLS()` → `UpstreamTlsContext` |
| HTTP/2 upstream | ✅ | `Http2ProtocolOptions` |
| Max requests per connection | ✅ | `MaxRequestsPerConnection` |
| Upstream timeout (`Options.Timeouts.Request`) | ❌ | Field exists in model, not wired to cluster |
| Upstream timeout (`Options.Timeouts.Idle`) | ❌ | Field exists in model, not wired |

---

## 2. Route match translation

| Feature | Status | Notes |
|---------|--------|-------|
| Exact path match | ✅ | `RouteMatch_Path` |
| Prefix path match | ✅ | `RouteMatch_Prefix` |
| Regex path match (RE2) | ✅ | `RouteMatch_SafeRegex`, no Istio-style restrictions |
| Group path prefix composition | ✅ | Prepended to route path |
| Header matchers (group + route, exact) | ✅ | `HeaderMatcher_ExactMatch` |
| Header matchers (regex) | ❌ | Model has `Regex bool` on `HeaderMatcher`, not translated |
| Method matchers (all methods) | ✅ | Each method → `:method` header matcher |
| Query param matchers (exact, regex, presence) | ✅ | `QueryParameterMatcher` with all three modes |
| gRPC content-type match (`Match.GRPC`) | ✅ | `content-type: application/grpc` prefix header matcher |
| Hostname match per route (`Match.Hostnames`) | ❌ | Field exists, not wired to VirtualHost domains (only group hostnames are used) |
| CEL route match (`Match.CEL`) | 🚫 | Not applicable — Envoy route matching is declarative, no runtime CEL |
| Port match (`Match.Ports`) | 🚫 | Not applicable — port is a Listener property, not a route matcher |

---

## 3. Route action — Forward

| Feature | Status | Notes |
|---------|--------|-------|
| Single destination forward | ✅ | `RouteAction_Cluster` |
| Weighted multi-destination (A/B) | ✅ | `RouteAction_WeightedClusters` |
| Request timeout (`Forward.Timeouts.Request`) | ✅ | `RouteAction.Timeout` |
| Per-attempt timeout | ✅ | `RetryPolicy.PerTryTimeout` |
| MaxGRPC timeout | ❌ | Field exists in model, not wired |
| Retry (attempts, conditions, backoff base+max) | ✅ | `buildRetryPolicy()` with full condition mapping |
| URL rewrite — prefix | ✅ | `PrefixRewrite` |
| URL rewrite — regex | ✅ | `RegexRewrite` with RE2 |
| URL rewrite — host literal | ✅ | `HostRewriteLiteral` |
| URL rewrite — host from header | ✅ | `HostRewriteHeader` |
| URL rewrite — auto host | ✅ | `AutoHostRewrite` |
| URL rewrite — strip query | ✅ | `StripQuery` on redirect (not forward) |
| Traffic mirror | ✅ | `RequestMirrorPolicies` with runtime fraction percentage |
| Hash policy — cookie (WCH) | ✅ | `RouteAction_HashPolicy_Cookie` |
| Hash policy — cookie (STICKY) | ✅ | Same mechanism |
| STICKY destination balancing (Redis) | ✅ | Go filter `vrata.sticky` auto-injected |
| WEIGHTED_CONSISTENT_HASH destination balancing | ⚠️ | Hash policy wired, but endpoint-level WCH not differentiated from STICKY |
| SkipWhen / OnlyWhen conditions | 🚫 | Not applicable — Envoy filters run unconditionally; inlineAuthz covers the use case |
| OnError fallback routes | 🚫 | Deferred — complex in Envoy, out of scope for this iteration |

---

## 4. Route action — Redirect

| Feature | Status | Notes |
|---------|--------|-------|
| Scheme redirect | ✅ | `RedirectAction_SchemeRedirect` |
| Host redirect | ✅ | `HostRedirect` |
| Path redirect | ✅ | `RedirectAction_PathRedirect` |
| Strip query | ✅ | `StripQuery` |
| Response codes 301/302/303/307/308 | ✅ | All mapped |

---

## 5. Route action — Direct response

| Feature | Status | Notes |
|---------|--------|-------|
| Status code + inline body | ✅ | `DirectResponseAction` |

---

## 6. Listener translation

| Feature | Status | Notes |
|---------|--------|-------|
| Address + port | ✅ | `SocketAddress` |
| TLS termination (cert + key files) | ✅ | `DownstreamTlsContext` |
| TLS min/max version | ✅ | `TlsParameters` |
| mTLS client auth (require/optional + CA) | ✅ | `RequireClientCertificate` + `CertificateValidationContext` |
| HTTP/2 (h2c) | ❌ | Model has `HTTP2 bool`, not wired to HCM or ListenerOptions |
| Server name header | ❌ | `ServerName` field exists, not set in HCM |
| Max request headers KB | ❌ | `MaxRequestHeadersKB` field exists, not wired to HCM |
| ClientHeader timeout → `RequestHeadersTimeout` | ✅ | `buildHCM()` |
| ClientRequest timeout → `RequestTimeout` | ✅ | `buildHCM()` |
| IdleBetweenRequests → `StreamIdleTimeout` | ✅ | `buildHCM()` |
| GroupIDs → selective VirtualHost attachment | ✅ | Empty = all groups, non-empty = selective |
| GroupIDs model design | ⚠️ | Open issue: owner considers routes first-class; GroupIDs on Listener is provisional |

---

## 7. Middleware translation

| Middleware | Status | Envoy mechanism | Notes |
|-----------|--------|-----------------|-------|
| CORS | ✅ | `envoy.filters.http.cors` | Origins (exact + regex), methods, headers, expose headers |
| JWT | ✅ | `envoy.filters.http.jwt_authn` | Local JWKS + remote JWKS, issuer, audiences |
| ExtAuthz | ✅ | `envoy.filters.http.ext_authz` | HTTP and gRPC modes, timeout, failure mode |
| ExtProc | ✅ | `envoy.filters.http.ext_proc` | Phase config (headers/body modes), timeout, failure mode |
| RateLimit | ✅ | `envoy.filters.http.local_ratelimit` | Token bucket (RPS + burst) |
| Headers (request add/remove, response add/remove) | ✅ | `envoy.filters.http.header_mutation` | |
| AccessLog (file, JSON + text, variable mapping) | ✅ | HCM `access_log` with `envoy.access_loggers.file` | |
| InlineAuthz (CEL, body access, lazy buffering) | ✅ | Go plugin `vrata.inlineauthz` (auto-injected) | |
| XFCC injection (spoof protection) | ✅ | Go plugin `vrata.xfcc` (auto-injected on mTLS) | |
| Sticky (Redis, request + response side) | ✅ | Go plugin `vrata.sticky` (auto-injected on STICKY routes) | |
| Middleware `skipWhen` / `onlyWhen` conditions | 🚫 | Not applicable — inlineAuthz covers the use case |
| Middleware `MiddlewareOverride` per route | ❌ | Not translated — middlewares applied at group level only |
| AssertClaims (JWT CEL) | ❌ | Model has the field; would require JWT + inlineAuthz combo |

---

## 8. Go filter extensions

| Extension | Request-side | Response-side | Auto-injected | Notes |
|-----------|-------------|---------------|---------------|-------|
| `vrata.sticky` | ✅ `SetUpstreamOverrideHost` | ✅ `UpstreamRemoteAddress` → Redis write (async) | ✅ When any route has STICKY | `VRATA_STICKY_STRICT` for 503 on unavailable pin |
| `vrata.inlineauthz` | ✅ CEL + lazy body buffer | — | ✅ When middleware type `inlineAuthz` | |
| `vrata.xfcc` | ✅ Strip + inject from TLS metadata | — | ✅ When listener has mTLS | |

---

## 9. Control plane infrastructure

| Feature | Status | Notes |
|---------|--------|-------|
| ADS gRPC server on `:18000` | ✅ | `discoveryv3.RegisterAggregatedDiscoveryServiceServer` |
| Snapshot cache (IDHash) | ✅ | `cachev3.NewSnapshotCache` |
| Gateway: store events → xDS push | ✅ | `gateway.go` subscribes to store, calls `PushSnapshot` |
| K8s EndpointSlice watcher → endpoint merge | ✅ | `k8s.Watcher` injects dynamic endpoints into destinations |
| REST API unchanged | ✅ | Same handlers, same routes |
| BoltDB + Raft store | ✅ | Unchanged from main branch |
| Snapshot versioning (auto-increment) | ✅ | `atomic.Int64` per push |
| Per-fleet xDS (multiple Envoy fleets) | ❌ | Single wildcard node ID `""` — all Envoys get the same config |
| xDS node filtering (push only to relevant node) | ❌ | `IDHash` used but all nodes receive same snapshot via `""` key |
| TLS on xDS gRPC channel | ❌ | Currently plaintext gRPC |
| Metrics on control plane (push count, latency, errors) | ❌ | No Prometheus endpoint |
| xDS translator unit tests | ❌ | All translation functions untested |
| E2E tests with real Envoy | ❌ | No test infrastructure |

---

## 10. Summary

**Total feature areas audited**: 10
**Fully implemented**: ~65% of surface area
**Partially implemented or caveated**: ~8%
**Not implemented (but planned)**: ~15%
**Discarded (architectural)**: ~12%

### Top gaps to address next

1. Route-level hostname match (`Match.Hostnames` → VirtualHost domains per route, not just group)
2. Header matcher regex (model field `Regex bool` not wired)
3. HTTP/2 on listener (h2c)
4. Server name + max request headers KB on HCM
5. `MiddlewareOverride` per route (not yet propagated to Envoy per-route filter config)
6. Per-fleet xDS (multiple Envoy fleets from one control plane)
7. TLS on xDS gRPC channel
8. Upstream timeouts (request + idle) on cluster
9. Translator unit tests
10. E2E tests

