# Controller — TODO

## Pending

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **Regex overlap detection** — detect semantic overlaps when one of the paths is a RegularExpression. Currently regex paths are skipped by the dedup detector.
- [ ] **Agentic CRD pinning** — Kube Agentic Networking has no releases yet (v0alpha0 prototype). The Makefile pins to a commit on main. When the project publishes a release, switch `AGENTIC_NET_COMMIT` to a version variable and use the release asset URL, same pattern as Gateway API CRDs.

## Done

### Kube Agentic Networking support

**Date**: 2026-03-25

Watch `XBackend` and `XAccessPolicy` from `agentic.prototype.x-k8s.io/v0alpha0`
and map them to Vrata entities. Uses three generic proxy features implemented in the
server (CEL body access, mTLS client auth, and `inlineAuthz` middleware) for InlineTools
and SPIFFE/ServiceAccount identity. ExternalAuth and XBackend work with existing Vrata
entities without additional proxy changes.

See `AGENTIC_NETWORKING_REPORT.md` for the full spec analysis.
See `SERVER_DECISIONS.md` for the proxy feature design rationale.

- [x] **Go types** — `apis/agentic/types.go` with `XBackend`, `XBackendList`,
  `XAccessPolicy`, `XAccessPolicyList`, DeepCopy implementations. Registered in
  controller scheme.
- [x] **Informers** — XBackend and XAccessPolicy added to cache, gated by config flags.
- [x] **Config** — `watch.xBackends`, `watch.xAccessPolicies` (default false),
  `agentic.trustDomain` for SPIFFE ID generation. Accessor methods added.
- [x] **Naming convention** — `k8s:agentic:{namespace}/{name}` prefix.
  `IsAgenticOwned()` helper in mapper package.
- [x] **Mapper: XBackend → Destination + Route** — `mapper/xbackend.go`.
  ServiceName → FQDN, Hostname → TLS destination, default/custom MCP path.
  4 unit tests + 2 e2e.
- [x] **Mapper: XAccessPolicy InlineTools → inlineAuthz middleware** —
  `mapper/xaccess_policy.go`. Generates CEL rules: always-allow (GET/DELETE/
  initialize/tools/list) + per-source+tools rules. SPIFFE source → CEL on
  `request.tls.peerCertificate.uris`. ServiceAccount → SPIFFE ID conversion
  using configured trust domain. 7 unit tests + 2 e2e.
- [x] **Mapper: XAccessPolicy ExternalAuth → extAuthz middleware** — HTTP and
  GRPC modes, cross-namespace backendRef, ExternalAuth takes precedence over
  InlineTools when both present. 4 unit tests + 1 e2e.
- [x] **Mapper edge cases** — empty tools (deny all), no authorization (nil),
  multiple inline rules, mixed rules, empty backend, always-allow rule
  verification, default deny assertion. 3 unit tests.
- [x] **Sync cycle** — Phase 3b after Gateways: `syncAllXBackends` creates/
  updates destinations and routes, `syncAllXAccessPolicies` creates/updates
  middlewares. Dependency order: destinations before routes before middlewares.
- [x] **Garbage collection** — `gcAgenticEntities` deletes orphaned agentic
  routes and destinations. Middleware GC in `syncAllXAccessPolicies`. 1 e2e test.
- [x] **Status writing** — `SetXBackendAvailable` and `SetXAccessPolicyAccepted`
  methods on status.Writer. XBackend uses standard conditions. XAccessPolicy
  uses PolicyAncestorStatus with controllerName `vrata.io/controller`.
- [x] **Metrics** — Reuses existing `ReconcileTotal` and `ReconcileErrors`
  counters with `xbackend` and `xaccesspolicy` resource labels.
- [x] **Vrata client extension** — `Middleware` struct extended with `ExtAuthz`
  and `InlineAuthz` map fields. `Destination` extended with `Options` map for
  TLS config.
- [x] **Makefile** — `controller-deploy-agentic-crds` target installs XBackend
  and XAccessPolicy CRDs from the kube-agentic-networking repo.

**Tests**: 19 mapper unit tests + 5 e2e tests (XBackend service, XBackend hostname,
InlineTools, ExternalAuth, GC).

### Pre-existing completed items

- [x] **Batch snapshot coordination** — `vrata.io/batch` and `vrata.io/batch-size` annotations with FIFO work queue, idle timeout, and incomplete batch detection. See `CONTROLLER_DECISIONS.md`.
- [x] **Garbage collection** — inter-group GC (orphaned HTTPRoutes) and intra-group GC (stale routes/middlewares within an HTTPRoute). See `CONTROLLER_DECISIONS.md`.
- [x] **ReferenceGrant enforcement** — cross-namespace backendRefs verified via ReferenceGrant before reconciliation. See `CONTROLLER_DECISIONS.md`.
- [x] **Metrics wiring** — all 8 controller Prometheus metrics wired into the sync cycle.
- [x] **Dedup detector reset** — reset at the start of each sync cycle to avoid stale phantom entries.
- [x] **controller-runtime logr bridge** — `crlog.SetLogger` bridging slog to logr.
