# Feature Coverage Report — Rutoso

Generated: 2026-03-17 (rev 2 — corrected after full source audit)
Method: Source code review of every function + empirical e2e testing against live environment

## How to read this report

- **100%** = Implemented correctly, tested, verified working
- **Percentage** = Implemented but with known bugs or missing pieces (described)
- **0%** = Not implemented or dead code

## API CRUD (control plane)

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Routes CRUD | 100% | Unit + E2E | None |
| Groups CRUD | 100% | Unit + E2E | None |
| Destinations CRUD | 100% | Unit + E2E | None |
| Listeners CRUD | 100% | Unit + E2E | None |
| Middlewares CRUD | 100% | Unit + E2E | None |
| Config dump | 100% | Unit + E2E | None |
| Route action validation (exactly-one-of) | 100% | Unit | None |
| Invalid JSON → 400 | 100% | Unit | None |

## Versioned Snapshots

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Create snapshot (capture live config) | 100% | Unit + E2E | None |
| List snapshots (with active flag) | 100% | Unit + E2E | None |
| Get snapshot (full payload) | 100% | Unit + E2E | None |
| Delete snapshot (clears active if needed) | 100% | Unit + E2E | None |
| Activate snapshot | 100% | Unit + E2E | None |
| SSE stream serves active snapshot | 100% | Unit + E2E | None |
| SSE pushes on activate | 100% | Unit | None |
| SSE silent without active snapshot | 100% | Unit + E2E | None |
| Proxy reconnects on disconnect | 100% | Unit | None |

## Proxy Routing — Matching

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Path prefix | 100% | Unit + E2E | None |
| Path exact | 100% | Unit | None |
| Path regex | 100% | Unit + E2E | None |
| Method match | 100% | Unit + E2E | None |
| Header match | 100% | Unit + E2E | None |
| Hostname match | 100% | Unit | None |
| Query param match | 100% | Unit | None |
| CEL expression match | 100% | Unit + E2E | ~940ns/eval, compiled once |
| gRPC content-type match | 80% | Unit | Code in match(), not e2e tested |
| Group pathRegex + route composition | 100% | Unit + E2E | All 8 composition cases |
| Group hostname/header merge | 100% | Unit | De-duplicated |

## Route Actions

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Direct response | 100% | Unit + E2E | None |
| Redirect | 100% | Unit + E2E | None |
| Forward (proxy to upstream) | 100% | Unit + E2E | None |
| Mutual exclusivity validation | 100% | Unit | None |

## Forward Action Features

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Weighted backend selection | 90% | Unit | **Bug**: all-zero weights always picks last backend, not uniform |
| Path rewrite (prefix) | 100% | E2E | None |
| Path rewrite (regex) | 70% | Code review | **Bug**: regex recompiled on every request (no cache). `compileOnce` is misnamed |
| Host rewrite | 70% | Code review | `AutoHost` sets `r.Host=""` — may not propagate correctly to upstream |
| Retry with backoff | 30% | Code review | **Bug**: `perAttemptTimeout` parsed but never applied. `time.Sleep` ignores context cancel. Final response body already closed on fallback |
| Request timeout | 80% | Code review | Works but skips circuit breaker recording when timeout handler is used |
| Idle timeout | 20% | Code review | **Bug**: panics if retry transport is wrapping the real transport — `proxy.Transport.(*http.Transport)` will fail the type assertion |
| Request mirror | 30% | Code review | **Bug**: passes original `*http.Request` to goroutine — body already read, context may be cancelled. No timeout on mirror call |
| Hash policy (consistent hashing) | 20% | Code review | **Bug**: `RingHashBalancer.Build()` and `MaglevBalancer.Build()` never called from production code. Ring/table always empty. Falls back to `SelectBackend` every time |
| Max gRPC timeout | 90% | Code review | Minor: truncates sub-millisecond durations to 0 |
| WebSocket upgrade | 80% | Code review | Go ReverseProxy handles natively. Not explicitly tested |
| Group retryDefault / includeAttemptCount | 50% | Code review | `X-Request-Attempt-Count` hardcoded to "1", never incremented on retries |

## Middlewares

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| CORS | 95% | Unit + E2E | No wildcard `*` origin support. Preflight returns 200 instead of 204. Bad regex silently dropped |
| Headers (add/remove req/resp) | 90% | Unit + E2E | ResponseWriter wrapper missing `http.Flusher` — breaks SSE/streaming through this middleware |
| Access Log | 95% | Unit + E2E | File handle never closed. Non-JSON field order non-deterministic. ResponseWriter missing `http.Flusher` |
| Rate Limit | 85% | Unit + E2E | **Memory leak**: per-IP buckets never evicted. No cleanup goroutine. No `Retry-After` header. Only keyed by client IP |
| JWT | 40% | Code review | **Critical**: no signature verification. RSA keys parsed but never used to verify JWT. Token with forged signature passes if claims match. Refresh goroutine leaked (no shutdown). Only RSA keys supported |
| ExtAuthz | 75% | Code review | Functional but: request body never forwarded to authz service (always nil body). `io.Copy` error unchecked. No gRPC mode (HTTP only) |
| ExtProc (HTTP mode) | 85% | Unit (16 tests) | gRPC connection created per-request (no pooling). ResponseWriter missing `http.Flusher`. Body streaming modes (streamed, bufferedPartial) not implemented yet |
| ExtProc (gRPC mode) | 60% | Unit | Same issues as HTTP plus no e2e test with real gRPC processor |
| Middleware chain ordering | 100% | Unit | None |
| Middleware disable per-route | 100% | Unit | None |
| Middleware override (group + route merge) | 100% | Unit | None |

## Kubernetes Discovery

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| EndpointSlice watching (ClusterIP) | 100% | Unit (fake client) | None |
| ExternalName Service resolution | 100% | Unit (fake client) | None |
| Watch cleanup on destination delete | 100% | Unit | None |
| Non-EDS destinations ignored | 100% | Unit | None |

## Proxy Infrastructure

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Atomic routing table swap | 100% | Unit + E2E | None |
| Listener management (reconcile) | 80% | E2E | `sameListener` ignores TLS/MaxRequestHeaders changes. HTTP/2 flag compared but never wired. Race on listen error |
| Circuit breaker | 30% | Unit (structure only) | **Bug**: `RecordFailure()` never called from handler — breaker never opens on upstream errors. Half-open allows unlimited requests. Threshold hardcoded to 5 |
| Health checks (active HTTP) | 40% | Code review | Per-destination interval ignored (hardcoded 5s). No healthy/unhealthy threshold counters. No expected status code matching |
| Outlier detection | 0% | Code review | **Dead code**: `RecordResponse()` never called from anywhere. Race condition on fields. Completely unwired |
| TLS upstream | 70% | Code review | Transport built correctly with cert/key/CA/SNI/min-max version. Not e2e tested (needs certs) |
| TLS downstream (listener) | 70% | Code review | Cert loaded in listener.go. Not e2e tested. Config changes don't trigger restart |
| HTTP/2 upstream | 40% | Code review | `ForceAttemptHTTP2` set on transport but no ALPN config on TLS. Not tested |

## Store

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Bolt store (all CRUD + snapshots) | 100% | Unit (interface suite) | None |
| Memory store (all CRUD + snapshots) | 100% | Unit (interface suite) | None |
| Event subscription | 100% | Unit | Publish outside transaction (fixed) |

## Control Plane Infrastructure

| Feature | Status | Verified By | Known Issues |
|---|---|---|---|
| Request logger middleware | 100% | Unit | None |
| Panic recovery middleware | 100% | Unit | None |
| Respond helpers | 100% | Unit | None |
| Gateway rebuild on store event | 100% | Unit | None |
| Swagger UI / OpenAPI docs | 100% | Manual | None |

## Cross-cutting Issues Found

These affect multiple features:

| Issue | Impact | Affected Components |
|---|---|---|
| Custom ResponseWriters missing `http.Flusher`/`http.Hijacker` | Breaks SSE, WebSocket, HTTP/2 push when middleware is active | headers.go, accesslog.go, extproc.go |
| Circuit breaker `RecordFailure()` never called | Breaker never opens on upstream failures | handler.go, circuit.go |
| Outlier detection completely unwired | Feature is dead code | outlier.go, handler.go |
| Balancer `Build()` never called | Ring hash and maglev are non-functional | balancer.go, upstream.go |
| Rate limiter memory leak | Bucket map grows unbounded | ratelimit.go |
| JWT no signature verification | Critical security hole | jwt.go |

## Test Count

| Suite | Tests | Passing |
|---|---|---|
| Store interface (bolt + memory) | 18 | 18 |
| API handlers | 33 | 33 |
| API middleware | 3 | 3 |
| Respond helpers | 2 | 2 |
| Gateway | 2 | 2 |
| K8s watcher | 4 | 4 |
| Proxy router | 8 | 8 |
| CEL eval | 11 | 11 |
| Proxy middlewares | 28 | 28 |
| Sync client | 2 | 2 |
| Config | 5 | 5 |
| E2E (live) | 18 | 18 |
| **Total** | **134** | **134** |
