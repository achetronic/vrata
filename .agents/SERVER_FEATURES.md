# Feature Coverage Report — Vrata (xDS Branch)

Generated: 2026-03-29
Branch: `feat/envoy-xds-control-plane`

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

## xDS Translation — Routes

| Feature                                                | Status | Tests        |
| ------------------------------------------------------ | ------ | ------------ |
| Path prefix match                                      | Done   | Needs tests  |
| Path exact match                                       | Done   | Needs tests  |
| Path regex match (SafeRegex RE2)                       | Done   | Needs tests  |
| Header matchers (group + route merged)                 | Done   | Needs tests  |
| Method matcher                                         | Done   | Needs tests  |
| Group path prefix composition                          | Done   | Needs tests  |
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
| Query param matchers                                   | Not done | —          |
| Port matchers                                          | Not done | —          |
| CEL expression match                                   | Not done | —          |
| gRPC content-type match                                | Not done | —          |
| Hostname match (wildcard)                              | Done   | Needs tests  |

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

## xDS Translation — Middlewares → Envoy HTTP Filters

| Feature                                                      | Status   | Tests        |
| ------------------------------------------------------------ | -------- | ------------ |
| CORS → `envoy.filters.http.cors`                            | Done     | Needs tests  |
| JWT → `envoy.filters.http.jwt_authn` (local + remote JWKS)  | Done     | Needs tests  |
| ExtAuthz → `envoy.filters.http.ext_authz` (HTTP + gRPC)     | Done     | Needs tests  |
| RateLimit → `envoy.filters.http.local_ratelimit`            | Done     | Needs tests  |
| Headers → `envoy.filters.http.header_mutation`               | Done     | Needs tests  |
| AccessLog → HCM `access_log` (file, JSON/text)              | Done     | Needs tests  |
| InlineAuthz → Go plugin `vrata.inlineauthz`                 | Done     | Needs tests  |
| XFCC → Go plugin `vrata.xfcc` (auto on mTLS)                | Done     | Needs tests  |
| ExtProc → `envoy.filters.http.ext_proc`                     | Not done | —            |
| Router filter always last                                    | Done     | Needs tests  |

## Go Filter Extensions

| Feature                                                      | Status   | Tests        |
| ------------------------------------------------------------ | -------- | ------------ |
| sticky: request-side Redis lookup + header injection         | Done     | Needs tests  |
| sticky: response-side session pinning to Redis               | Not done | —            |
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
