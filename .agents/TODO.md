# TODO - Vrata

## In Progress

### Comprehensive timeout configuration

All timeouts across listeners, destinations, and middlewares must be
configurable with semantic names. See DECISIONS.md for the naming convention.

**Listener timeouts** — add `timeouts` to `model.Listener`:

- [x] `clientHeader` (default 10s) — `Server.ReadHeaderTimeout`
- [x] `clientRequest` (default 60s) — `Server.ReadTimeout`
- [x] `clientResponse` (default 60s) — `Server.WriteTimeout`
- [x] `idleBetweenRequests` (default 120s) — `Server.IdleTimeout`
- [x] Wire in `proxy/listener.go:startListener`
- [x] Remove hardcoded `ReadHeaderTimeout: 10s`
- [ ] Unit tests for all four
- [ ] Update docs/features.md

**Destination timeouts** — add `timeouts` to `model.DestinationOptions`, replace `connectTimeout`:

- [x] `request` (default 30s) — `Client.Timeout`
- [x] `connect` (default 5s) — `Dialer.Timeout` (replaces `options.connectTimeout`)
- [x] `dualStackFallback` (default 300ms) — `Dialer.FallbackDelay`
- [x] `tlsHandshake` (default 5s) — `Transport.TLSHandshakeTimeout`
- [x] `responseHeader` (default 10s) — `Transport.ResponseHeaderTimeout`
- [x] `expectContinue` (default 1s) — `Transport.ExpectContinueTimeout`
- [x] `idleConnection` (default 90s) — `Transport.IdleConnTimeout`
- [x] Wire in `proxy/endpoint.go:NewEndpoint`
- [x] Remove old `options.connectTimeout` field
- [x] Remove `forward.timeouts.idle` from route (now `idleConnection` on destination)
- [ ] Unit tests for all seven
- [ ] Update docs/features.md

**Middleware timeout renames**:

- [x] `extAuthz.timeout` → `extAuthz.decisionTimeout`
- [x] `extProc.timeout` → `extProc.phaseTimeout`
- [x] Add `jwt.jwksRetrievalTimeout` (default 10s, currently hardcoded)
- [x] `jwt.jwksUri` → `jwt.jwksPath` (it's a path within the destination, not a full URI)
- [x] Update model, middleware code, tests
- [ ] Update docs/features.md

**Route cleanup**:

- [x] Remove `forward.timeouts.idle` (moved to destination)
- [x] Keep `forward.timeouts.request` as the external watchdog

## Pending

### Housekeeping

- [ ] Add authentication to the REST API
- [ ] Update `ARCHITECTURE.md` to reflect current package structure

### Deferred from audit

- [ ] **Circuit breaker configurability** — add `OpenDuration` and `FailureThreshold` fields to `CircuitBreakerOptions` model. Currently hardcoded to 30s and 5. Wire in `proxy/circuit.go`.
- [ ] **ExtProc interceptResponseWriter → httpsnoop** — `proxy/middlewares/extproc.go` manually implements `http.ResponseWriter` for response interception. Needs refactor to use `httpsnoop.Wrap` to preserve optional interfaces.
- [ ] **DestinationTimeouts.Request wiring** — model field exists but is not wired. `httputil.ReverseProxy` doesn't use `http.Client`, so `Client.Timeout` can't be set directly. Design options: (a) wrap Transport with context deadline, (b) apply in forwardHandler as fallback when route has no `forward.timeouts.request`.

### Proxy fleets — single control plane, multiple fleets

A single control plane should be able to manage multiple independent proxy
fleets, each with its own routing config. A fleet identifier (e.g. a label
or a path parameter) distinguishes which config a proxy receives when it
connects via SSE. This allows one control plane cluster to serve staging,
production, and canary fleets without separate deployments.

This is ASAP.

## Done

- [x] **Prometheus metrics** — 22 metrics across 5 dimensions, per-listener, isolated registries
- [x] **onError fallback routes** — typed error matching, forward/redirect/directResponse actions, X-Vrata-Error-\* headers
- [x] **JSON error responses** — all proxy errors return `{"error":"..."}` with Content-Type: application/json
- [x] **Middleware JSON errors** — JWT and rate limit errors now return JSON instead of text/plain
- [x] **Config restructure** — `server:` → `controlPlane:`, `controlPlane:` → `proxy:`, `cluster:` → `controlPlane.raft:`, unified `storePath`
- [x] **Sync endpoint rename** — `/sync/stream` → `/sync/snapshot`, `/internal/raft/apply` → `/sync/raft`
- [x] **GitHub Actions** — release-binaries.yml (4 platforms) + release-docker.yml (multi-arch)
- [x] **Helm chart config maps** — `controlPlane.config` and `proxy.config` as free YAML maps
- [x] **skipWhen / onlyWhen CEL** — added to MiddlewareOverride, implemented in handler.go with precompiled CEL programs. skipWhen: skip middleware if any expression matches. onlyWhen: only run if at least one matches. Mutually exclusive.
- [x] **assertClaims CEL for JWT** — replaces old `rules` field. List of CEL expressions evaluated against decoded JWT claims map. All must pass or 403. Uses new `celeval.ClaimsProgram`.
- [x] **Removed JWTRule** — old rules field deleted from model and middleware code
- [x] **Endpoint concept** — `model.Endpoint{Host,Port}`, `proxy.Endpoint` embeds model with runtime state. `DestinationPool` groups N endpoints + balancer per destination. Zero `Upstream` in codebase.
- [x] **Static endpoint lists** — `Destination.Endpoints []Endpoint` via API, resolved by `ResolvedEndpoints()`
- [x] **K8s watcher wired** — `internal/k8s` watcher connected to gateway via `EndpointProvider` interface. Discovered endpoints merged into destinations at rebuild time.
- [x] **Endpoint STICKY** — Redis-backed zero-disruption endpoint pinning. `pickStickyEndpoint` in pool.go.
- [x] **Session store abstraction** — `session.Store` interface, `session/redis/` sub-package. Ready for memcached/dynamodb implementations.
- [x] **Three destination balancing algorithms** — WEIGHTED_RANDOM, WEIGHTED_CONSISTENT_HASH, STICKY (Redis)
- [x] **Six endpoint balancing algorithms** — ROUND_ROBIN, RANDOM, LEAST_REQUEST, RING_HASH, MAGLEV, STICKY
- [x] **Gateway in controlplane mode** — controlplane now runs proxy gateway + listener manager (was missing, required separate proxy process)
- [x] **Comprehensive e2e test suite** — 69 e2e tests, ~130k requests total in balancing tests
- [x] **docs/features.md** — complete feature reference with JSON examples and field tables
- [x] **Rename: Rutoso → Vrata** — module is now `github.com/achetronic/vrata`, binary is `vrata`, cookie is `_vrata_pin`, Helm chart is `charts/vrata`
- [x] **Helm chart** — `charts/vrata/` with controlplane/ and proxy/ template subdirs, professional values.yaml, ci/kind-values.yaml
- [x] **HA — Raft consensus** — 3-5 node control plane cluster with embedded hashicorp/raft
- [x] **Destination pinning** — weighted consistent hash for canary-safe sticky sessions
- [x] **BackendRef → DestinationRef rename** — consistent terminology
- [x] **Audit rounds — 30+ bugs fixed**
- [x] **External processor middleware** — proto, gRPC+HTTP, all body modes, observe-only worker pool
- [x] **External authorization gRPC mode** — proto, HTTP+gRPC
- [x] **JWT EC/Ed25519 support** — P1363 format
- [x] **Versioned snapshots** — capture, list, activate, rollback, SSE serves active only
- [x] **CEL expressions** — compiled once, ~940ns/eval, AND with static matchers
- [x] **Kubernetes ExternalName Service** — watches Service object, resolves spec.externalName
- [x] **Store publish outside bolt transaction** — prevents stale reads during rebuild
- [x] **Full proxy implementation** — routing, middlewares, balancers, health, circuit breaker, outlier, TLS, HTTP/2, retry, rewrite, mirror, WebSocket, access log
- [x] **210 unit tests + 80 e2e tests** — all passing
