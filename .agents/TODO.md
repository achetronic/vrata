# TODO - Vrata

## In Progress

_(nothing)_

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

- [x] **Rename: Rutoso → Vrata** — module is now `github.com/achetronic/vrata`, binary is `vrata`, cookie is `_vrata_pin`, Helm chart is `charts/vrata`
- [x] **Helm chart** — `charts/vrata/` with controlplane/ and proxy/ template subdirs, professional values.yaml, ci/kind-values.yaml
- [x] **HA — Raft consensus** — 3-5 node control plane cluster with embedded hashicorp/raft
  - `internal/raft/fsm.go`: FSM applies commands to bolt store, Dump/Restore for snapshots
  - `internal/raft/node.go`: Raft lifecycle, static + DNS peer discovery, bootstrap with retry, advertise address, resource cleanup on shutdown
  - `internal/raft/logger.go`: hclog→slog adapter with level parsing
  - `internal/store/raftstore/`: store.Store wrapper (reads→local bolt, writes→Raft leader)
  - `internal/api/handlers/raft.go`: internal apply endpoint secured to private IPs only
  - Config: `cluster` block with `nodeId`, `bindAddress`, `advertiseAddress`, `dataDir`, `peers`, `discovery.dns`
  - Write-forwarding: followers forward to leader transparently via HTTP with 10s timeout
  - Makefile: `make e2e-cluster` builds image, loads into kind, deploys via helm, runs cluster tests
  - 14 unit tests + 8 kind e2e (all nodes indistinguishable)
- [x] **Destination pinning** — weighted consistent hash for canary-safe sticky sessions
- [x] **BackendRef → DestinationRef rename** — consistent terminology
- [x] **Audit rounds — 30+ bugs fixed** (JWT ECDSA P1363, RSA alg-aware, infinite loop, retry, circuit breaker, outlier, rate limiter, health checks, regex pre-compile, cleanup callbacks, etc.)
- [x] **External processor middleware** — proto, gRPC+HTTP, all body modes, observe-only worker pool
- [x] **External authorization gRPC mode** — proto, HTTP+gRPC
- [x] **JWT EC/Ed25519 support** — P1363 format
- [x] **Versioned snapshots** — capture, list, activate, rollback, SSE serves active only
- [x] **CEL expressions** — compiled once, ~940ns/eval, AND with static matchers
- [x] **Kubernetes ExternalName Service** — watches Service object, resolves spec.externalName
- [x] **Store publish outside bolt transaction** — prevents stale reads during rebuild
- [x] **Full proxy implementation** — routing, middlewares, balancers, health, circuit breaker, outlier, TLS, HTTP/2, retry, rewrite, mirror, WebSocket, access log
- [x] **235 tests** — unit + e2e against live cluster and kind
