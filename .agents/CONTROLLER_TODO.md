# Controller — TODO

## Pending

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **Regex overlap detection** — detect semantic overlaps when one of the paths is a RegularExpression. Currently regex paths are skipped by the dedup detector.

## Done

- [x] **Batch snapshot coordination** — `vrata.io/batch` and `vrata.io/batch-size` annotations with FIFO work queue, idle timeout, and incomplete batch detection. See `CONTROLLER_DECISIONS.md`.
- [x] **Garbage collection** — inter-group GC (orphaned HTTPRoutes) and intra-group GC (stale routes/middlewares within an HTTPRoute). See `CONTROLLER_DECISIONS.md`.
- [x] **ReferenceGrant enforcement** — cross-namespace backendRefs verified via ReferenceGrant before reconciliation. See `CONTROLLER_DECISIONS.md`.
- [x] **Metrics wiring** — all 8 controller Prometheus metrics wired into the sync cycle.
- [x] **Dedup detector reset** — reset at the start of each sync cycle to avoid stale phantom entries.
- [x] **controller-runtime logr bridge** — `crlog.SetLogger` bridging slog to logr.
- [x] **Kube Agentic Networking** — XBackend → Destination + Route, XAccessPolicy → inlineAuthz (InlineTools) and extAuthz (ExternalAuth). SPIFFE/ServiceAccount identity via CEL. GC for orphaned agentic entities. Status writing. Config: `watch.xBackends`, `watch.xAccessPolicies`, `agentic.trustDomain`.
