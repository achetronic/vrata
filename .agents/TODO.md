# TODO - Vrata

## In Progress

- [ ] **E2e tests for skipWhen/onlyWhen** — test with two middlewares, one skipWhen and one onlyWhen
- [ ] **E2e test for assertClaims** — JWT middleware rejects invalid claims via CEL
- [ ] **Update docs/features.md** — document skipWhen, onlyWhen, assertClaims

## Pending

### Housekeeping
- [ ] Add authentication to the REST API
- [ ] Update `ARCHITECTURE.md` to reflect current package structure

### Proxy fleets — single control plane, multiple fleets
A single control plane should be able to manage multiple independent proxy
fleets, each with its own routing config. A fleet identifier (e.g. a label
or a path parameter) distinguishes which config a proxy receives when it
connects via SSE. This allows one control plane cluster to serve staging,
production, and canary fleets without separate deployments.

This is ASAP.

## Done

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
- [x] **175 unit tests + 69 e2e tests** — all passing
