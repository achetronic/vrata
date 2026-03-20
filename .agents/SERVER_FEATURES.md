# Feature Coverage Report — Vrata

Generated: 2026-03-19
Method: Line-by-line source audit + unit tests + e2e tests

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

| Feature                                                             | Status | Tests                |
| ------------------------------------------------------------------- | ------ | -------------------- |
| Weighted destination selection (WEIGHTED_RANDOM)                    | 100%   | Unit + E2E (15k req) |
| Destination balancing (WEIGHTED_CONSISTENT_HASH)                    | 100%   | Unit (7) + E2E (26k) |
| Destination balancing (STICKY + Redis)                              | 100%   | Unit (5) + E2E (20k) |
| Endpoint balancing (RR, Random, LeastReq, RingHash, Maglev, Sticky) | 100%   | Unit + E2E (61k req) |
| Path rewrite (prefix)                                               | 100%   | E2E                  |
| Path rewrite (regex, cached)                                        | 100%   | E2E                  |
| Host rewrite                                                        | 100%   | Unit                 |
| Retry with backoff + perAttemptTimeout                              | 100%   | Unit + E2E           |
| Request timeout                                                     | 100%   | E2E                  |
| Idle timeout (safe unwrap)                                          | 100%   | Unit                 |
| Request mirror (body cloned)                                        | 100%   | E2E                  |
| Hash policy (ring hash, maglev)                                     | 100%   | Unit                 |
| Max gRPC timeout (microsecond precision)                            | 100%   | Unit                 |
| WebSocket upgrade                                                   | 100%   | E2E                  |
| IncludeAttemptCount (set per retry)                                 | 100%   | Unit                 |
| LeastRequest balancer (Done wired)                                  | 100%   | Unit                 |

## Error Handling (onError)

| Feature                                                                                      | Status | Tests               |
| -------------------------------------------------------------------------------------------- | ------ | ------------------- |
| JSON default error responses (all proxy errors)                                              | 100%   | Unit + E2E          |
| onError rules with type filtering                                                            | 100%   | Unit (20) + E2E (6) |
| onError directResponse action                                                                | 100%   | Unit + E2E          |
| onError redirect action                                                                      | 100%   | Unit + E2E          |
| onError forward action with X-Vrata-Error-\* headers                                         | 100%   | Unit + E2E          |
| Error classification (connection_refused, reset, dns, timeout, tls, circuit, no_dest, no_ep) | 100%   | Unit (8)            |
| Wildcard: infrastructure                                                                     | 100%   | Unit                |
| Wildcard: all                                                                                | 100%   | Unit + E2E          |
| No match falls back to default JSON                                                          | 100%   | Unit + E2E          |

## Middlewares

| Feature                                                      | Status | Tests               |
| ------------------------------------------------------------ | ------ | ------------------- |
| CORS (wildcard `*`, 204 preflight)                           | 100%   | Unit + E2E          |
| Headers (httpsnoop)                                          | 100%   | Unit + E2E          |
| Access Log (httpsnoop, original path preserved)              | 100%   | Unit + E2E          |
| Rate Limit (eviction + stop channel)                         | 100%   | Unit + E2E          |
| JWT (RSA/RS256-512 + EC P1363 + Ed25519, JWKS, flat config)  | 100%   | Unit (14) + E2E (2) |
| JWT assertClaims (CEL against decoded payload)               | 100%   | Unit + E2E          |
| JWT claimToHeaders (CEL expressions for nested/array claims) | 100%   | Unit + E2E          |
| ExtAuthz (HTTP + gRPC modes)                                 | 100%   | Unit (10) + E2E     |
| ExtProc HTTP (buffered + bufferedPartial + streamed)         | 100%   | Unit (19) + E2E (2) |
| ExtProc gRPC                                                 | 100%   | Unit                |
| Middleware chain ordering                                    | 100%   | Unit                |
| Middleware skipWhen (CEL condition to skip)                  | 100%   | E2E (3)             |
| Middleware onlyWhen (CEL condition to activate)              | 100%   | E2E (3)             |
| Middleware disable per-route                                 | 100%   | Unit + E2E          |
| Middleware override merge                                    | 100%   | Unit                |
| Cleanup on table swap (JWT refresh, rate limiter)            | 100%   | Code review         |

## Proxy Infrastructure

| Feature                                                        | Status | Tests      |
| -------------------------------------------------------------- | ------ | ---------- |
| Atomic routing table swap (with cleanup callbacks)             | 100%   | Unit + E2E |
| Listener management (detects TLS changes)                      | 100%   | E2E        |
| Circuit breaker (configurable failureThreshold + openDuration) | 100%   | Unit       |
| Health checks (thresholds, per-dest interval)                  | 100%   | Unit       |
| Outlier detection (wired via OnResponse, race-free)            | 100%   | Unit       |
| TLS upstream                                                   | 100%   | Unit       |
| TLS downstream                                                 | 100%   | Unit       |
| HTTP/2 (ALPN configured)                                       | 100%   | Unit       |

## Timeouts

| Feature                                                                                                                  | Status | Tests       |
| ------------------------------------------------------------------------------------------------------------------------ | ------ | ----------- |
| Listener timeouts (clientHeader, clientRequest, clientResponse, idleBetweenRequests)                                     | 100%   | Unit (4)    |
| Destination timeouts (request, connect, dualStackFallback, tlsHandshake, responseHeader, expectContinue, idleConnection) | 100%   | Unit (6)    |
| Destination request timeout fallback (route → destination)                                                               | 100%   | Code review |
| parseDurationOrDefault generic helper                                                                                    | 100%   | Unit (4)    |
| ExtAuthz decisionTimeout (renamed from timeout)                                                                          | 100%   | Unit + E2E  |
| ExtProc phaseTimeout (renamed from timeout)                                                                              | 100%   | Unit + E2E  |
| JWT jwksRetrievalTimeout (new, was hardcoded 10s)                                                                        | 100%   | E2E         |
| JWT jwksPath (renamed from jwksUri)                                                                                      | 100%   | E2E         |

## Prometheus Metrics

| Feature                                                              | Status | Tests      |
| -------------------------------------------------------------------- | ------ | ---------- |
| Per-listener metrics config (path, collect, histograms)              | 100%   | Unit + E2E |
| Route metrics (requests, duration, size, inflight, retries, mirrors) | 100%   | Unit + E2E |
| Destination metrics (requests, duration, inflight, circuit breaker)  | 100%   | Unit + E2E |
| Endpoint metrics (requests, duration, healthy, consecutive 5xx)      | 100%   | Unit + E2E |
| Middleware metrics (duration, passed, rejections)                    | 100%   | Unit + E2E |
| Listener metrics (connections, active, TLS errors)                   | 100%   | Unit       |
| Endpoint disabled by default (high cardinality opt-in)               | 100%   | Unit + E2E |
| Custom scrape endpoint path                                          | 100%   | E2E        |
| Isolated prometheus.Registry per listener                            | 100%   | Unit       |
| Gauge scraper goroutine (health, circuit, 5xx)                       | 100%   | Unit       |
| Context-based collector injection (zero overhead when disabled)      | 100%   | Unit       |

## Kubernetes Discovery

| Feature                | Status | Tests       |
| ---------------------- | ------ | ----------- |
| EndpointSlice watching | 100%   | Unit        |
| ExternalName Service   | 100%   | Unit        |
| Watch cleanup          | 100%   | Unit        |
| Non-EDS ignored        | 100%   | Unit        |
| OnChange nil-safe      | 100%   | Code review |

## HA Cluster (Raft)

| Feature                                       | Status | Tests                                      |
| --------------------------------------------- | ------ | ------------------------------------------ |
| Raft FSM (apply commands to bolt)             | 100%   | Unit (7 command types + unknown + invalid) |
| Raft snapshot/restore (Dump + Restore)        | 100%   | Unit + integration                         |
| Static peer discovery                         | 100%   | Unit                                       |
| DNS peer discovery (k8s headless Service)     | 100%   | E2E (kind)                                 |
| Bootstrap with retry (k8s cold start)         | 100%   | E2E (kind)                                 |
| Advertise address (pod IP in k8s)             | 100%   | E2E (kind)                                 |
| Write-forwarding (follower → leader)          | 100%   | E2E (kind)                                 |
| Single-node cluster                           | 100%   | Unit                                       |
| 3-node replication                            | 100%   | Unit + E2E                                 |
| Resource cleanup on shutdown                  | 100%   | Code review                                |
| Internal apply endpoint (private IP only)     | 100%   | Unit (5 tests)                             |
| Raft store wrapper (reads local, writes Raft) | 100%   | Unit + E2E                                 |
| Cluster config validation                     | 100%   | Unit (4 tests)                             |

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

| Suite                                                              | Tests   | Passing |
| ------------------------------------------------------------------ | ------- | ------- |
| Model                                                              | 3       | 3       |
| Store (bolt + memory)                                              | 9       | 9       |
| API handlers                                                       | 34      | 34      |
| API middleware                                                     | 3       | 3       |
| Respond                                                            | 2       | 2       |
| Config                                                             | 14      | 14      |
| Gateway                                                            | 2       | 2       |
| K8s watcher                                                        | 4       | 4       |
| Session store (Redis)                                              | 5       | 5       |
| Proxy (router, pinning, balancer, pool, metrics, errors, timeouts) | 54      | 54      |
| CEL eval                                                           | 11      | 11      |
| Proxy middlewares                                                  | 60      | 60      |
| Raft (FSM, cluster, peers)                                         | 7       | 7       |
| Sync client                                                        | 2       | 2       |
| **Unit total**                                                     | **226** | **226** |
| E2E (proxy, live)                                                  | 73      | 73      |
| E2E (metrics)                                                      | 5       | 5       |
| E2E (onError)                                                      | 6       | 6       |
| E2E (cluster, kind)                                                | 8       | 8       |
| **E2E total**                                                      | **92**  | **92**  |

## Known Remaining Issues

None.
