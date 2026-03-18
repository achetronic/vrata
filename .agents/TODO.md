# TODO - Rutoso

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

This is ASAP after the rename.

### Rename: Rutoso → Vrata
Full project rename. Vrata means "door" / "gate" in Slavic languages.

Scope:
- Go module path: `github.com/achetronic/rutoso` → `github.com/achetronic/vrata`
- All import paths across every `.go` file
- Proto package: `rutoso.extproc.v1` → `vrata.extproc.v1`, `rutoso.extauthz.v1` → `vrata.extauthz.v1`
- Proto `go_package` option
- Binary name: `rutoso` → `vrata`
- Config references: `_rutoso_pin` cookie → `_vrata_pin`
- Makefile, Dockerfile, README, `.agents/` docs
- bbolt bucket names stay (internal, no user impact)
- API paths stay (`/api/v1/...` — no "rutoso" in them)
- Regenerate protos and swagger docs after rename

## Done

- [x] **HA — Raft consensus** — 3-5 node control plane cluster with embedded hashicorp/raft
  - `internal/raft/fsm.go`: FSM applies commands to bolt store, Dump/Restore for snapshots
  - `internal/raft/node.go`: Raft lifecycle, static + DNS peer discovery, bootstrap with retry, advertise address, resource cleanup on shutdown
  - `internal/raft/logger.go`: hclog→slog adapter with level parsing
  - `internal/store/raftstore/`: store.Store wrapper (reads→local bolt, writes→Raft leader)
  - `internal/api/handlers/raft.go`: internal apply endpoint secured to private IPs only
  - Config: `cluster` block with `nodeId`, `bindAddress`, `advertiseAddress`, `dataDir`, `peers`, `discovery.dns`
  - Write-forwarding: followers forward to leader transparently via HTTP with 10s timeout
  - k8s manifests: StatefulSet + headless Service with `publishNotReadyAddresses: true`
  - Makefile: `make e2e-cluster` builds image, loads into kind, deploys, waits, runs cluster tests
  - 14 unit tests (FSM apply, snapshot/restore, peer parsing, single-node, 3-node replication, dump/restore)
  - 5 e2e tests against kind cluster (basic write, snapshot activation, replication, SSE stream, config dump)
  - 5 handler tests (non-cluster 503, public IP 403, private IP 200, loopback 200, isPrivateAddr)
  - Exported `CommandType` constants used across fsm, raftstore, and bolt (DRY)
- [x] **Destination pinning** — weighted consistent hash for canary-safe sticky sessions
- [x] **BackendRef → DestinationRef rename** — consistent terminology
- [x] **Audit round 5 — 30 bugs fixed** (JWT ECDSA P1363, RSA alg-aware, infinite loop, retry, circuit breaker, outlier, rate limiter, health checks, regex pre-compile, cleanup callbacks, etc.)
- [x] **External processor middleware** — proto, gRPC+HTTP, all body modes, 19 unit + 2 e2e tests
- [x] **External authorization gRPC mode** — proto, HTTP+gRPC, 10 unit + 1 e2e
- [x] **JWT EC/Ed25519 support** — P1363 format, 13 unit + 2 e2e
- [x] **Versioned snapshots** — capture, list, activate, rollback, SSE serves active only
- [x] **CEL expressions** — compiled once, ~940ns/eval, AND with static matchers
- [x] **Kubernetes ExternalName Service** — watches Service object, resolves spec.externalName
- [x] **Store publish outside bolt transaction** — prevents stale reads during rebuild
- [x] **Full proxy implementation** — routing, middlewares, balancers, health, circuit breaker, outlier, TLS, HTTP/2, retry, rewrite, mirror, WebSocket, access log
- [x] **235 tests** — unit + e2e against live cluster and kind
