# Server TODO — Vrata

## Pending

### Housekeeping

- [ ] Add authentication to the REST API
- [ ] E2e tests for skipWhen/onlyWhen
- [ ] E2e test for assertClaims

### Multi-value matchers on MatchRule

`MatchRule` currently accepts a single `path`, `pathPrefix`, or `pathRegex` (mutually
exclusive). Supporting arrays (`paths []string`, `pathPrefixes []string`,
`pathRegexes []string`) with OR semantics would allow one Route to match
multiple paths. This reduces the number of entities the controller creates —
an HTTPRoute rule with 3 matches becomes 1 Route instead of 3. Impact:
- `model/group.go` MatchRule struct changes
- `proxy/router.go` compiledRoute.match() iterates arrays
- `proxy/table.go` compileRoute handles group+route composition per array entry
- ~38% fewer routes for real-world HTTPRoute workloads (6961 → ~4310)

### Proxy fleets — single control plane, multiple fleets

A single control plane should be able to manage multiple independent proxy
fleets, each with its own routing config. A fleet identifier (e.g. a label
or a path parameter) distinguishes which config a proxy receives when it
connects via SSE. This allows one control plane cluster to serve staging,
production, and canary fleets without separate deployments.

## Done

- [x] **Comprehensive timeout configuration** — 4 listener timeouts, 7 destination timeouts, 3 middleware timeout renames (decisionTimeout, phaseTimeout, jwksRetrievalTimeout), jwksUri → jwksPath, route idle timeout moved to destination
- [x] **Circuit breaker configurability** — `FailureThreshold` and `OpenDuration` configurable on `CircuitBreakerOptions`
- [x] **DestinationTimeouts.Request wiring** — falls back from route to destination when route has no `forward.timeouts.request`
- [x] **ExtProc httpsnoop migration** — `interceptResponseWriter` replaced with `httpsnoop.Wrap` hooks
- [x] **Full audit** — 103 findings, 90 fixed, 0 deferred critical. See SERVER_AUDIT.md
- [x] **Prometheus metrics** — 22 metrics across 5 dimensions, per-listener, isolated registries
- [x] **onError fallback routes** — typed error matching, forward/redirect/directResponse, X-Vrata-Error-\* headers
- [x] **JSON error responses** — all proxy and middleware errors return `{"error":"..."}` with application/json
- [x] **Config restructure** — `controlPlane:` (address, storePath, raft), `proxy:` (controlPlaneUrl, reconnectInterval)
- [x] **Sync endpoint rename** — `/sync/snapshot`, `/sync/raft`
- [x] **GitHub Actions** — release-binaries.yml (4 platforms) + release-docker.yml (multi-arch)
- [x] **Helm chart** — `controlPlane.config` and `proxy.config` as free YAML maps, raft detected from config
- [x] **skipWhen / onlyWhen CEL** — middleware conditions with precompiled CEL programs
- [x] **assertClaims CEL for JWT** — CEL expressions against decoded claims
- [x] **Endpoint concept** — model.Endpoint, proxy.Endpoint with runtime state, DestinationPool
- [x] **K8s watcher** — EndpointSlice + ExternalName, wired to gateway rebuild
- [x] **Endpoint STICKY** — Redis-backed zero-disruption endpoint pinning
- [x] **Session store** — interface + Redis implementation
- [x] **Three destination balancing algorithms** — WEIGHTED_RANDOM, WEIGHTED_CONSISTENT_HASH, STICKY
- [x] **Six endpoint balancing algorithms** — ROUND_ROBIN, RANDOM, LEAST_REQUEST, RING_HASH, MAGLEV, STICKY
- [x] **Versioned snapshots** — capture, list, activate, rollback, SSE serves active only
- [x] **CEL expressions** — compiled once, AND with static matchers
- [x] **HA — Raft consensus** — 3-5 nodes, write-forwarding, DNS discovery
- [x] **External processor** — gRPC+HTTP, all body modes, observe-only
- [x] **External authorization** — HTTP+gRPC
- [x] **JWT** — RSA/EC/Ed25519, JWKS remote+inline, assertClaims, claimToHeaders
- [x] **Full proxy** — routing, middlewares, balancers, health, circuit breaker, outlier, TLS, HTTP/2, retry, rewrite, mirror, WebSocket, access log
- [x] **226 unit tests + 92 e2e tests** — all passing
