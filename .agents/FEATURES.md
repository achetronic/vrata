# Feature Coverage Report — Vrata

Generated: 2026-03-18 (rev 6 — all issues resolved, full audit clean)
Method: Line-by-line source audit + unit tests + e2e tests against live cluster

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
| SSE serves active snapshot | 100%   | Unit + E2E |
| SSE pushes on activate     | 100%   | Unit       |
| No event without active    | 100%   | Unit + E2E |
| Proxy reconnects           | 100%   | Unit       |

## Proxy Routing

| Feature                                | Status | Tests      |
| -------------------------------------- | ------ | ---------- |
| Path prefix                            | 100%   | Unit + E2E |
| Path exact                             | 100%   | Unit       |
| Path regex                             | 100%   | Unit + E2E |
| Method match                           | 100%   | Unit + E2E |
| Header match (pre-compiled regex)      | 100%   | Unit + E2E |
| Hostname match                         | 100%   | Unit + E2E |
| Query param match (pre-compiled regex) | 100%   | Unit + E2E |
| CEL expression match                   | 100%   | Unit + E2E |
| gRPC content-type match                | 100%   | Unit + E2E |
| Group composition (8 cases)            | 100%   | Unit + E2E |

## Route Actions

| Feature            | Status | Tests      |
| ------------------ | ------ | ---------- |
| Direct response    | 100%   | Unit + E2E |
| Redirect           | 100%   | Unit + E2E |
| Forward            | 100%   | Unit + E2E |
| Mutual exclusivity | 100%   | Unit       |

## Forward Action Features

| Feature                                                 | Status | Tests                 |
| ------------------------------------------------------- | ------ | --------------------- |
| Weighted destination selection (WEIGHTED_RANDOM)         | 100%   | Unit + E2E (15k req)  |
| Destination balancing (WEIGHTED_CONSISTENT_HASH)         | 100%   | Unit (7) + E2E (26k)  |
| Destination balancing (STICKY + Redis)                   | 100%   | Unit (5) + E2E (20k)  |
| Endpoint balancing (RR, Random, LeastReq, RingHash, Maglev, Sticky) | 100% | Unit + E2E (61k req) |
| Path rewrite (prefix)                                   | 100%   | E2E                   |
| Path rewrite (regex, cached)                            | 100%   | E2E                   |
| Host rewrite                                            | 100%   | Unit                  |
| Retry with backoff + perAttemptTimeout                  | 100%   | Unit + E2E            |
| Request timeout                                         | 100%   | E2E                   |
| Idle timeout (safe unwrap)                              | 100%   | Unit                  |
| Request mirror (body cloned)                            | 100%   | E2E                   |
| Hash policy (ring hash, maglev)                         | 100%   | Unit                  |
| Max gRPC timeout (microsecond precision)                | 100%   | Unit                  |
| WebSocket upgrade                                       | 100%   | E2E                   |
| IncludeAttemptCount (set per retry)                     | 100%   | Unit                  |
| LeastRequest balancer (Done wired)                      | 100%   | Unit                  |

## Middlewares

| Feature                                                           | Status | Tests               |
| ----------------------------------------------------------------- | ------ | ------------------- |
| CORS (wildcard `*`, 204 preflight)                                | 100%   | Unit + E2E          |
| Headers (httpsnoop)                                               | 100%   | Unit + E2E          |
| Access Log (httpsnoop, original path preserved)                   | 100%   | Unit + E2E          |
| Rate Limit (eviction + stop channel)                              | 100%   | Unit + E2E          |
| JWT (RSA/RS256-512 + EC P1363 + Ed25519, JWKS, refresh with stop) | 100%   | Unit (13) + E2E (2) |
| ExtAuthz (HTTP + gRPC modes)                                      | 100%   | Unit (10) + E2E     |
| ExtProc HTTP (buffered + bufferedPartial + streamed)              | 100%   | Unit (19) + E2E (2) |
| ExtProc gRPC                                                      | 100%   | Unit                |
| Middleware chain ordering                                         | 100%   | Unit                |
| Middleware disable per-route                                      | 100%   | Unit                |
| Middleware override merge                                         | 100%   | Unit                |
| Cleanup on table swap (JWT refresh, rate limiter)                 | 100%   | Code review         |

## Proxy Infrastructure

| Feature                                                 | Status | Tests      |
| ------------------------------------------------------- | ------ | ---------- |
| Atomic routing table swap (with cleanup callbacks)      | 100%   | Unit + E2E |
| Listener management (detects TLS changes)               | 100%   | E2E        |
| Circuit breaker (RecordFailure wired, half-open atomic) | 100%   | Unit       |
| Health checks (thresholds, per-dest interval)           | 100%   | Unit       |
| Outlier detection (wired via OnResponse, race-free)     | 100%   | Unit       |
| TLS upstream                                            | 100%   | Unit       |
| TLS downstream                                          | 100%   | Unit       |
| HTTP/2 (ALPN configured)                                | 100%   | Unit       |

## Kubernetes Discovery

| Feature                | Status | Tests       |
| ---------------------- | ------ | ----------- |
| EndpointSlice watching | 100%   | Unit        |
| ExternalName Service   | 100%   | Unit        |
| Watch cleanup          | 100%   | Unit        |
| Non-EDS ignored        | 100%   | Unit        |
| OnChange nil-safe      | 100%   | Code review |

## HA Cluster (Raft)

| Feature | Status | Tests |
|---|---|---|
| Raft FSM (apply commands to bolt) | 100% | Unit (7 command types + unknown + invalid) |
| Raft snapshot/restore (Dump + Restore) | 100% | Unit + integration |
| Static peer discovery | 100% | Unit |
| DNS peer discovery (k8s headless Service) | 100% | E2E (kind) |
| Bootstrap with retry (k8s cold start) | 100% | E2E (kind) |
| Advertise address (pod IP in k8s) | 100% | E2E (kind) |
| Write-forwarding (follower → leader) | 100% | E2E (kind) |
| Single-node cluster | 100% | Unit |
| 3-node replication | 100% | Unit + E2E |
| Resource cleanup on shutdown | 100% | Code review |
| Internal apply endpoint (private IP only) | 100% | Unit (5 tests) |
| Raft store wrapper (reads local, writes Raft) | 100% | Unit + E2E |
| Cluster config validation | 100% | Unit (4 tests) |

## Store

| Feature                                 | Status | Tests |
| --------------------------------------- | ------ | ----- |
| Bolt (all CRUD + snapshots)             | 100%   | Unit  |
| Memory (all CRUD + snapshots)           | 100%   | Unit  |
| Event subscription (publish outside tx) | 100%   | Unit  |

## Control Plane

| Feature                    | Status | Tests  |
| -------------------------- | ------ | ------ |
| Request logger (httpsnoop) | 100%   | Unit   |
| Panic recovery             | 100%   | Unit   |
| Respond helpers            | 100%   | Unit   |
| Gateway rebuild            | 100%   | Unit   |
| Swagger UI                 | 100%   | Manual |

## Test Summary

| Suite                  | Tests   | Passing |
| ---------------------- | ------- | ------- |
| Model                  | 3       | 3       |
| Store (bolt + memory)  | 9       | 9       |
| API handlers           | 34      | 34      |
| API middleware         | 3       | 3       |
| Respond                | 2       | 2       |
| Config                 | 12      | 12      |
| Gateway                | 2       | 2       |
| K8s watcher            | 4       | 4       |
| Session store (Redis)  | 5       | 5       |
| Proxy (router+pinning+balancer+pool) | 21 | 21 |
| CEL eval               | 11      | 11      |
| Proxy middlewares      | 60      | 60      |
| Raft (FSM, cluster, peers) | 7   | 7       |
| Sync client            | 2       | 2       |
| E2E (proxy, live)      | 73      | 73      |
| E2E (cluster, kind)    | 8       | 8       |
| **Total**              | **256** | **256** |

## Bugs Fixed Across All Audits

| Bug                                                      | Severity | Fix                                         |
| -------------------------------------------------------- | -------- | ------------------------------------------- |
| JWT ECDSA uses ASN.1 but JWT uses R\|\|S                 | Critical | P1363→ASN.1 DER conversion                  |
| JWT RSA always SHA-256                                   | Critical | Alg-aware hash selection (RS256/384/512)    |
| Interpolate/accesslog infinite loop via header injection | Critical | Position tracking prevents re-matching      |
| Retry off-by-one extra request                           | Critical | Removed unreachable fallthrough RoundTrip   |
| Retry cancelAttempt before body consumed                 | Critical | defer cancel, not immediate                 |
| JWT no signature verification                            | Critical | RSA/EC/Ed25519 verification with JWKS       |
| Circuit breaker never opens                              | Critical | RecordFailure on 5xx                        |
| Outlier detection dead code                              | Critical | Wired via Upstream.OnResponse               |
| Idle timeout + retry = panic                             | Critical | unwrapHTTPTransport chain                   |
| Ring hash / maglev Build + Pick wired                    | High     | Build() in BuildTable, PickByHash in handler |
| Rate limiter memory leak + goroutine leak                | High     | Eviction + stop channel + cleanup on swap   |
| JWT refresh goroutine leak                               | High     | close() channel + cleanup on swap           |
| LeastRequest Done never called                           | High     | Wired via interface check in forwardHandler |
| Router regex compiled per-request (ReDoS)                | High     | Pre-compiled in compileRoute                |
| Circuit half-open unlimited requests                     | High     | Atomic counter limits to 1                  |
| Retry returns closed body                                | High     | Re-structured loop control flow             |
| Mirror uses consumed body                                | High     | Clone body + request.Clone                  |
| ResponseWriter breaks Flusher                            | High     | httpsnoop everywhere                        |
| Access log shows rewritten path                          | Medium   | Capture originalPath before next            |
| discardResponseWriter Header() new map each call         | Medium   | Persistent header map                       |
| Handlers use context.Background()                        | Medium   | Changed to r.Context()                      |
| ExtAuthz ignores request body                            | Medium   | Body forwarded when IncludeBody=true        |
| SelectBackend all-zero weights                           | Medium   | Uniform random                              |
| Health check ignores thresholds                          | Medium   | Consecutive counters + per-dest interval    |
| Listener ignores TLS changes                             | Medium   | sameListener compares TLS fields            |
| CORS no wildcard origin                                  | Medium   | `*` origin supported                        |
| CORS preflight 200                                       | Medium   | Changed to 204                              |
| gRPC timeout sub-ms truncation                           | Low      | Microsecond unit                            |
| IncludeAttemptCount hardcoded                            | Low      | Set per retry attempt                       |
| HTTP/2 no ALPN                                           | Low      | NextProtos includes "h2"                    |
| k8s OnChange nil dereference                             | Low      | notifyChange helper with nil guard          |

## Known Remaining Issues

None.
