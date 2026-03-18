# TODO - Rutoso

## In Progress

_(nothing)_

## Pending

### HA — Embedded distributed store (Hashicorp Raft + bbolt)
Rutoso must run in HA with N replicas where any node can die without losing
configuration and proxies always have a Rutoso available.

Tasks:
- [ ] Add `hashicorp/raft` and `hashicorp/raft-boltdb` to go.mod
- [ ] Implement `internal/raft/fsm.go`
- [ ] Implement `internal/raft/cluster.go`
- [ ] Implement `store/raft/raft.go`: store.Store wrapper
- [ ] Add `cluster` config block to config.yaml
- [ ] Leader detection: non-leader nodes redirect writes
- [ ] Snapshot/restore: serialize and restore full bbolt state
- [ ] Integration test: 3-node cluster

### Housekeeping
- [ ] Add authentication to the REST API
- [ ] Update `ARCHITECTURE.md` to reflect current package structure

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
- [x] **209 tests** — unit + e2e against live cluster with controllable upstreams
