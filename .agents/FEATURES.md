# Feature Coverage Report — Rutoso

Generated: 2026-03-17
Method: Empirical testing against live control plane (:8080) + proxy (:3000) + kind cluster (rutoso-dev)

## Summary

| Category | Features | 100% | Partial | 0% |
|---|---|---|---|---|
| API CRUD | 5 resources | 5 | 0 | 0 |
| Proxy routing | 8 match types | 7 | 0 | 1 |
| Middlewares | 7 types | 4 | 1 | 2 |
| Snapshots | 5 operations | 5 | 0 | 0 |
| Sync (SSE) | 1 | 1 | 0 | 0 |
| K8s discovery | 2 types | 1 | 0 | 1 |
| Proxy features | 10 | 4 | 2 | 4 |

## Detailed Feature Matrix

### API CRUD (control plane)

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Routes: list, create, get, update, delete | 100% | ✅ | ✅ | Action validation (exactly-one-of) works |
| Groups: list, create, get, update, delete | 100% | ✅ | ✅ | |
| Destinations: list, create, get, update, delete | 100% | ✅ | ✅ | |
| Listeners: list, create, get, update, delete | 100% | ✅ | ✅ | Default address 0.0.0.0 applied |
| Middlewares: list, create, get, update, delete | 100% | ✅ | ✅ | |
| Config dump (GET /debug/config) | 100% | ✅ | ✅ | Returns all 5 entity types |
| Invalid JSON body → 400 | 100% | ✅ | — | All 5 create handlers tested |

### Proxy Routing

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Path prefix matching | 100% | ✅ | ✅ | Verified via /e2e-forward, /e2e-direct |
| Path exact matching | 100% | ✅ | — | Tested in router_test.go |
| Path regex matching | 100% | ✅ | ✅ | Group regex composition /(es\|en\|pk) verified |
| Method matching | 100% | ✅ | ✅ | POST-only route rejects GET |
| Header matching | 100% | ✅ | ✅ | X-Test: yes required |
| Hostname matching | 100% | ✅ | — | Tested in router_test.go |
| CEL expression matching | 100% | ✅ | ✅ | Complex expression with header check verified |
| Query param matching | 100% | ✅ | — | Tested in router_test.go (CEL + static) |
| gRPC matching (content-type) | 0% | — | — | Model exists, not tested end-to-end |

### Group Composition

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Group pathPrefix + route path | 100% | ✅ | — | Concatenation |
| Group pathRegex + route pathPrefix | 100% | ✅ | ✅ | Regex non-capture group wrapping |
| Group pathRegex + route pathRegex | 100% | ✅ | — | Double regex composition |
| Group hostnames merged with route | 100% | ✅ | — | De-duplicated |
| Group headers merged with route | 100% | ✅ | — | Appended |

### Route Actions

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Forward (proxy to upstream) | 100% | ✅ | ✅ | Verified with podinfo |
| Direct response | 100% | ✅ | ✅ | 418 "i am a teapot" |
| Redirect | 100% | ✅ | ✅ | 302 → https://example.com |
| Mutual exclusivity validation | 100% | ✅ | — | 400 if >1 or 0 actions set |

### Forward Action Features

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Weighted backend selection | 100% | ✅ | — | balancer_test.go |
| Path rewrite (prefix) | 100% | — | ✅ | /e2e-forward → / verified |
| Path rewrite (regex) | 50% | — | — | Code exists, not e2e tested |
| Host rewrite | 50% | — | — | Code exists, not e2e tested |
| Retry with backoff | 50% | — | — | retry.go exists with transport wrapper |
| Request timeout | 50% | — | — | Code exists in handler.go |
| Idle timeout | 50% | — | — | Code exists in handler.go |
| Request mirror | 50% | — | — | mirrorRequest() exists, fire-and-forget |
| Hash policy (ring hash, maglev) | 100% | ✅ | — | balancer_test.go |
| WebSocket upgrade | 50% | — | — | Go ReverseProxy handles natively |
| Max gRPC timeout | 50% | — | — | Code exists, parses grpc-timeout header |

### Middlewares

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| CORS | 100% | ✅ | ✅ | Preflight + normal + no-origin all verified |
| Headers (add/remove req/resp) | 100% | ✅ | ✅ | Response header injection verified |
| Access Log | 100% | ✅ | ✅ | JSON to stdout, request + response phases |
| Rate Limit (token bucket) | 100% | ✅ | ✅ | Burst 2, then 429 verified |
| External Processor (extProc) | 80% | ✅ | — | 16 unit tests pass. HTTP mode tested. gRPC mode not e2e tested (needs live processor). Missing: streamed body mode, bufferedPartial |
| JWT | 0% | — | — | Model exists, handler stub exists, no real validation logic |
| ExtAuthz | 0% | — | — | Model exists, handler stub exists, no real validation logic |
| Middleware override (disable per-route) | 100% | ✅ | — | Tested in handler.go buildRouteHandler |
| Middleware chain ordering | 100% | ✅ | — | Outermost first |

### Versioned Snapshots

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Create snapshot (capture live config) | 100% | ✅ | ✅ | All 5 entity types captured |
| List snapshots (with active flag) | 100% | ✅ | ✅ | Active flag correct |
| Get snapshot (full payload) | 100% | ✅ | ✅ | |
| Delete snapshot | 100% | ✅ | ✅ | Clears active pointer if deleted |
| Activate snapshot | 100% | ✅ | ✅ | Proxy receives config within 500ms |
| Rollback (activate previous) | 100% | — | ✅ | Implicit: activate any previous ID |

### SSE Sync Stream

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Serves active snapshot on connect | 100% | ✅ | ✅ | |
| Pushes on snapshot activate | 100% | ✅ | — | |
| No event without active snapshot | 100% | ✅ | ✅ | |
| Ignores non-snapshot store events | 100% | ✅ | — | |
| Proxy reconnects on disconnect | 100% | ✅ | — | sync/client_test.go |

### Kubernetes Discovery

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| EndpointSlice watching (ClusterIP) | 100% | ✅ | — | Fake client, 2 ready + 1 not-ready |
| ExternalName Service resolution | 100% | ✅ | — | Resolves spec.externalName as endpoint |
| Non-EDS destinations ignored | 100% | ✅ | — | No watch started |
| Watch cleanup on destination delete | 100% | ✅ | — | Endpoints cleared |

### Proxy Infrastructure

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Atomic routing table swap | 100% | ✅ | ✅ | In-flight requests unaffected |
| Listener management (add/remove) | 100% | — | ✅ | Reconcile on config change |
| Circuit breaker | 100% | ✅ | — | circuit_test.go |
| Health checks (active HTTP) | 50% | — | — | health.go exists, not end-to-end verified |
| Outlier detection | 50% | — | — | outlier.go exists, not end-to-end verified |
| TLS upstream | 0% | — | — | Model exists, transport built in upstream.go, not tested |
| TLS downstream (listener) | 0% | — | — | Code exists in listener.go, needs cert |
| HTTP/2 upstream | 0% | — | — | ForceAttemptHTTP2 set, not tested |

### Store

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Bolt store (all CRUD + snapshots) | 100% | ✅ | ✅ | Interface test suite covers both impls |
| Memory store (all CRUD + snapshots) | 100% | ✅ | — | Used by all handler/sync tests |
| Event subscription (pub/sub) | 100% | ✅ | — | Publish outside transaction (bolt fix) |

### API Middleware (control plane)

| Feature | Status | Unit Test | E2E Test | Notes |
|---|---|---|---|---|
| Request logger | 100% | ✅ | — | |
| Panic recovery → 500 JSON | 100% | ✅ | — | |
| Respond helpers (JSON, Error) | 100% | ✅ | — | |

## Test Count Summary

| Suite | Tests | Passing |
|---|---|---|
| Store interface (bolt + memory) | 18 | 18 |
| API handlers (CRUD + snapshots + sync) | 33 | 33 |
| API middleware (logger + recovery) | 3 | 3 |
| Respond helpers | 2 | 2 |
| Gateway | 2 | 2 |
| K8s watcher | 4 | 4 |
| Proxy router | 8 | 8 |
| CEL eval | 11 | 11 |
| Proxy middlewares (CORS, headers, accesslog, extproc, ratelimit) | 28 | 28 |
| Sync client | 2 | 2 |
| Config | 5 | 5 |
| **E2E (live cluster)** | **18** | **18** |
| **Total** | **134** | **134** |

## Features at 0% (not implemented or no tests)

| Feature | Reason |
|---|---|
| JWT middleware | Model defined but no validation/JWKS logic implemented |
| ExtAuthz middleware | Model defined but no real HTTP/gRPC call logic |
| gRPC content-type matching | Model field exists, no e2e coverage |
| TLS upstream/downstream | Code exists but needs real certs to test |
| HTTP/2 | Transport flag set but not verified |
| Health checks e2e | Active checker exists but not triggered in tests |
| Outlier detection e2e | Detection logic exists but not triggered in tests |

## Features at 50% (partial)

| Feature | What works | What's missing |
|---|---|---|
| ExtProc | HTTP mode, all phases, reject, mutations, observe-only (16 tests) | gRPC mode e2e, streamed body, bufferedPartial |
| Path rewrite regex | Code in applyRewrite | No e2e test |
| Retry | Transport wrapper in retry.go | No e2e test with failing upstream |
| Timeouts | Code in forwardHandler | No e2e test |
| Mirror | mirrorRequest() goroutine | No e2e test |
