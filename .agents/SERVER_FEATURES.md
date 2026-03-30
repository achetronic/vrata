# Feature Coverage Report — Vrata (xDS Branch)

Generated: 2026-03-30
Branch: `feat/envoy-xds-control-plane`
Audit method: Line-by-line code inspection of `internal/xds/` against `internal/model/`.

## API CRUD

| Feature                  | Status | Tests       |
| ------------------------ | ------ | ----------- |
| Routes CRUD              | 100%   | Unit + E2E  |
| Groups CRUD              | 100%   | Unit + E2E  |
| Destinations CRUD        | 100%   | Unit + E2E  |
| Listeners CRUD           | 100%   | Unit + E2E  |
| Middlewares CRUD         | 100%   | Unit + E2E  |
| Config dump              | 100%   | Unit + E2E  |
| Route action validation  | 100%   | Unit        |
| Invalid JSON → 400       | 100%   | Unit        |
| Handlers use r.Context() | 100%   | Code review |

## Versioned Snapshots

| Feature                    | Status | Tests      |
| -------------------------- | ------ | ---------- |
| Create                     | 100%   | Unit + E2E |
| List (with active flag)    | 100%   | Unit + E2E |
| Get                        | 100%   | Unit + E2E |
| Delete (clears active)     | 100%   | Unit + E2E |
| Activate                   | 100%   | Unit + E2E |

Note: In this branch, xDS push happens on every store change, not on snapshot
activate. Snapshot API exists for auditing/rollback but is not the gating mechanism.

## xDS Translation — Clusters (Destinations → Envoy)

| Feature                                                | Status | Tests        |
| ------------------------------------------------------ | ------ | ------------ |
| Cluster type auto-derived (EDS / STATIC / STRICT_DNS)  | Done   | Needs tests  |
| LB policy (ROUND_ROBIN, LEAST_REQUEST, RING_HASH, MAGLEV, RANDOM) | Done | Needs tests |
| Ring hash config (min/max ring size)                    | Done   | Needs tests  |
| Maglev config (table size)                              | Done   | Needs tests  |
| Circuit breaker (max connections, pending, requests, retries) | Done | Needs tests |
| Outlier detection (consecutive 5xx, gateway errors, ejection) | Done | Needs tests |
| Health checks (HTTP, interval, thresholds)              | Done   | Needs tests  |
| Connect timeout from destination timeouts               | Done   | Needs tests  |
| Upstream TLS (SNI, CA, min/max version)                 | Done   | Needs tests  |
| Upstream mTLS (client cert + key)                       | Done   | Needs tests  |
| HTTP/2 upstream                                         | Done   | Needs tests  |
| Max requests per connection                             | Done   | Needs tests  |
| Endpoints from store + k8s watcher merge                | Done   | Needs tests  |
| LeastRequest.ChoiceCount                                | Gap    | —            |
| RingHash/Maglev HashPolicy (cluster-level)              | Gap    | —            |
| EndpointBalancing.Sticky (endpoint-level)               | Gap    | —            |
| Destination Timeouts.Request (total)                    | Gap    | —            |
| Destination Timeouts.IdleConnection                     | Gap    | —            |

## xDS Translation — Routes

| Feature                                                | Status | Tests        |
| ------------------------------------------------------ | ------ | ------------ |
| Path prefix match                                      | Done   | Needs tests  |
| Path exact match                                       | Done   | Needs tests  |
| Path regex match (SafeRegex RE2)                       | Done   | Needs tests  |
| Header matchers — exact, regex, presence (all 3 modes) | Done   | Needs tests  |
| Method matcher                                         | Done   | Needs tests  |
| Query param matchers — exact, regex, presence (all 3)  | Done   | Needs tests  |
| gRPC content-type match                                | Done   | Needs tests  |
| Group PathPrefix composition                           | Done   | Needs tests  |
| Hostname match (group + route merged into VirtualHost)  | Done   | Needs tests  |
| Forward action (single cluster)                        | Done   | Needs tests  |
| Forward action (weighted clusters)                     | Done   | Needs tests  |
| Route timeout                                          | Done   | Needs tests  |
| Retry policy (conditions, backoff, per-attempt timeout)| Done   | Needs tests  |
| URL rewrite (prefix)                                   | Done   | Needs tests  |
| URL rewrite (regex)                                    | Done   | Needs tests  |
| Host rewrite (literal, from header, auto)              | Done   | Needs tests  |
| Request mirror (cluster + percentage)                  | Done   | Needs tests  |
| Redirect action (scheme, host, path, code)             | Done   | Needs tests  |
| Direct response action (status + body)                 | Done   | Needs tests  |
| Hash policy (WCH cookie)                               | Done   | Needs tests  |
| Hash policy (STICKY cookie)                            | Done   | Needs tests  |
| Forward.MaxGRPCTimeout                                 | Gap    | —            |
| Forward.Retry.RetriableCodes                           | Gap    | —            |
| Redirect.URL (full URL)                                | Gap    | —            |
| CEL expression match                                   | Gap    | Architecture decision needed |
| Group PathRegex composition                            | Gap    | Only PathPrefix used |
| RouteGroup RetryDefault → VirtualHost retry            | Gap    | —            |
| RouteGroup IncludeAttemptCount                         | Gap    | —            |
| Route-level MiddlewareIDs                              | Gap    | Only group-level read |
| MiddlewareOverrides (route + group)                    | Gap    | Not translated to per-route config |

## xDS Translation — Listeners

| Feature                                                | Status | Tests        |
| ------------------------------------------------------ | ------ | ------------ |
| Listener → Envoy Listener (address + port)             | Done   | Needs tests  |
| HCM with RDS (ADS)                                    | Done   | Needs tests  |
| TLS termination (DownstreamTlsContext)                 | Done   | Needs tests  |
| mTLS client auth (require_client_certificate + CA)     | Done   | Needs tests  |
| TLS version params (min/max)                           | Done   | Needs tests  |
| GroupIDs → selective VirtualHost attachment             | Done   | Needs tests  |
| Empty GroupIDs = catch-all (all groups)                | Done   | Needs tests  |
| Timeouts.ClientHeader → RequestHeadersTimeout          | Done   | Needs tests  |
| Timeouts.ClientRequest → RequestTimeout                | Done   | Needs tests  |
| Timeouts.IdleBetweenRequests → StreamIdleTimeout       | Done   | Needs tests  |
| HTTP2 (h2c)                                            | Gap    | —            |
| ServerName → HCM server_name                           | Gap    | —            |
| MaxRequestHeadersKB → HCM max_request_headers_kb       | Gap    | —            |

## xDS Translation — Middlewares → Envoy HTTP Filters

| Feature                                                      | Status   | Tests        |
| ------------------------------------------------------------ | -------- | ------------ |
| CORS → `envoy.filters.http.cors`                            | Partial  | Needs tests  |
| JWT → `envoy.filters.http.jwt_authn` (local + remote JWKS)  | Partial  | Needs tests  |
| ExtAuthz → `envoy.filters.http.ext_authz` (HTTP + gRPC)     | Partial  | Needs tests  |
| ExtProc → `envoy.filters.http.ext_proc` (gRPC, phases)      | Partial  | Needs tests  |
| RateLimit → `envoy.filters.http.local_ratelimit`            | Partial  | Needs tests  |
| Headers → `envoy.filters.http.header_mutation`               | Partial  | Needs tests  |
| AccessLog → HCM `access_log` (file, JSON/text)              | Done     | Needs tests  |
| InlineAuthz → Go plugin `vrata.inlineauthz`                 | Done     | Needs tests  |
| XFCC → Go plugin `vrata.xfcc` (auto on mTLS)                | Done     | Needs tests  |
| Sticky → Go plugin `vrata.sticky` (auto on STICKY routes)    | Done     | Needs tests  |
| Router filter always last                                    | Done     | Needs tests  |

### Middleware field gaps detail

| Middleware | Missing fields |
|---|---|
| CORS | `MaxAge`, `AllowCredentials` |
| JWT | `JWKsRetrievalTimeout` (hardcoded 5s), `ForwardJWT`, `ClaimToHeaders`, `AssertClaims` |
| ExtAuthz | `IncludeBody`, `OnCheck` (ForwardHeaders, InjectHeaders), `OnAllow` (CopyToUpstream), `OnDeny` (CopyToClient) |
| ExtProc | `Mode` (http — gRPC only), `StatusOnError`, `AllowedMutations`, `ForwardRules`, `DisableReject`, `ObserveMode`, `MetricsPrefix`, `Phases.MaxBodyBytes` |
| RateLimit | `TrustedProxies` |
| Headers | Per-header `Append` flag (always APPEND_IF_EXISTS_OR_ADD) |

## Go Filter Extensions

| Feature                                                      | Status   | Tests        |
| ------------------------------------------------------------ | -------- | ------------ |
| sticky: request-side Redis lookup + header injection         | Done     | Needs tests  |
| sticky: response-side session pinning to Redis (async)       | Done     | Needs tests  |
| sticky: auto-injected in HCM for STICKY routes              | Done     | Code review  |
| sticky: filter factory registration via init()               | Done     | Code review  |
| inlineauthz: CEL evaluation with header + body access        | Done     | Needs tests  |
| inlineauthz: lazy body buffering                             | Done     | Code review  |
| inlineauthz: filter factory registration via init()          | Done     | Code review  |
| xfcc: strip incoming + inject from TLS metadata              | Done     | Needs tests  |
| xfcc: filter factory registration via init()                 | Done     | Code review  |

## Kubernetes Discovery

| Feature                | Status | Tests       |
| ---------------------- | ------ | ----------- |
| EndpointSlice watching | 100%   | Unit        |
| ExternalName Service   | 100%   | Unit        |
| Watch cleanup          | 100%   | Unit        |
| Non-EDS ignored        | 100%   | Unit        |
| OnChange → gw.Rebuild  | 100%   | Code review |

## HA Cluster (Raft)

| Feature                                       | Status | Tests                                      |
| --------------------------------------------- | ------ | ------------------------------------------ |
| Raft FSM (apply commands to bolt)             | 100%   | Unit (7 command types + unknown + invalid) |
| Raft snapshot/restore                         | 100%   | Unit + integration                         |
| Static peer discovery                         | 100%   | Unit                                       |
| DNS peer discovery                            | 100%   | E2E                                        |
| Write-forwarding (follower → leader)          | 100%   | E2E                                        |
| Single-node cluster                           | 100%   | Unit                                       |
| 3-node replication                            | 100%   | Unit + E2E                                 |

## Store

| Feature                                 | Status | Tests |
| --------------------------------------- | ------ | ----- |
| Bolt (all CRUD + snapshots)             | 100%   | Unit  |
| Memory (all CRUD + snapshots)           | 100%   | Unit  |
| Event subscription (publish outside tx) | 100%   | Unit  |

## Control Plane

| Feature                    | Status | Tests  |
| -------------------------- | ------ | ------ |
| Request logger             | 100%   | Unit   |
| Panic recovery             | 100%   | Unit   |
| Respond helpers            | 100%   | Unit   |
| Gateway rebuild → xDS push | 100%   | —      |
| Swagger UI                 | 100%   | Manual |

## What's NOT in this branch (removed from main)

| Feature                                                        | Reason |
| -------------------------------------------------------------- | ------ |
| Native Go proxy (`internal/proxy/`)                            | Replaced by Envoy fleet |
| SSE sync client (`internal/sync/`)                             | Replaced by xDS ADS |
| Go middleware implementations (CORS, JWT, etc.)                | Replaced by Envoy native filters |
| CEL evaluator (`internal/proxy/celeval/`)                      | Moved to extensions/inlineauthz |
| Proxy router, balancer, pool, circuit breaker, health checker  | Envoy handles all of this natively |
| Proxy listener manager                                         | Envoy manages its own listeners |
| Proxy metrics collector (22 Prometheus metrics)                | Envoy has native metrics |
| httpsnoop ResponseWriter interception                          | Not needed — no Go proxy |
| Routing table atomic swap with cleanup callbacks               | Not needed — Envoy handles config atomically |
