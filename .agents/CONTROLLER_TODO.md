# Controller — TODO

## Pending

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **Regex overlap detection** — detect semantic overlaps when one of the paths is a RegularExpression. Currently regex paths are skipped by the dedup detector.
- [ ] **AllowedRoutes enforcement** — respect `listener.allowedRoutes.namespaces` (Same/All/Selector) and `allowedRoutes.kinds` (HTTPRoute, GRPCRoute) from Gateway spec.
- [ ] **Gateway addresses** — populate `GatewayStatus.Addresses` from the Vrata listener state or from the Kubernetes Service.

## Done

- [x] **GRPCRoute support** — full reconciliation: GRPCRoute → Vrata Routes with `grpc: true` + path matching from `/{service}/{method}`. Filters, status writing, ReferenceGrant enforcement. See mapper `MapGRPCRoute`.
- [x] **GatewayClass claim** — controller watches GatewayClass, claims those with `controllerName: vrata.io/controller`, writes `Accepted: True`.
- [x] **Gateway status writing** — `Accepted` + `Programmed` conditions on Gateway. Per-listener `Accepted` + `Programmed` conditions. Protocol validation.
- [x] **Gateway listener updates** — `syncAllGateways` now updates existing listeners (not just create). Detects port/protocol/TLS changes.
- [x] **ParentRef accuracy** — HTTPRoute and GRPCRoute status uses the actual `parentRefs[0]` from the route spec instead of hardcoded `"controller"`.
- [x] **Protocol mapping** — listener protocol (`HTTP`, `HTTPS`, `GRPC`, `GRPCS`) affects TLS configuration. Unsupported protocols (`TCP`, `UDP`) rejected with status.
- [x] **GatewayClassName filtering** — controller only reconciles Gateways matching the configured `gatewayClassName` (default: `vrata`).
- [x] **Batch snapshot coordination** — `vrata.io/batch` and `vrata.io/batch-size` annotations with FIFO work queue, idle timeout, and incomplete batch detection. See `CONTROLLER_DECISIONS.md`.
- [x] **Garbage collection** — inter-group GC (orphaned HTTPRoutes/GRPCRoutes) and intra-group GC (stale routes/middlewares within an HTTPRoute). See `CONTROLLER_DECISIONS.md`.
- [x] **ReferenceGrant enforcement** — cross-namespace backendRefs verified via ReferenceGrant before reconciliation. Now supports both `HTTPRoute` and `GRPCRoute` kinds. See `CONTROLLER_DECISIONS.md`.
- [x] **Metrics wiring** — all 8 controller Prometheus metrics wired into the sync cycle.
- [x] **Dedup detector reset** — reset at the start of each sync cycle to avoid stale phantom entries.
- [x] **controller-runtime logr bridge** — `crlog.SetLogger` bridging slog to logr.
