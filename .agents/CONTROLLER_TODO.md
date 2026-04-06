# Controller — TODO

## Deferred / Future Epics

These features are conceptually large and have been deferred to avoid major architecture changes in v1:

- **AllowedRoutes enforcement**: respect `listener.allowedRoutes.namespaces` (Same/All/Selector) and `allowedRoutes.kinds`.
- **Listener hostname binding**: extracted but never used for route-to-listener binding.
- **ParentRefs sectionName**: routes don't bind to specific listener sections.
- **Regex overlap detection**: detect semantic overlaps when one path is a RegularExpression.
- **Per-backendRef filters**: `backendRefs[].filters[]` never read (only rule-level filters).

## Done

### Gateway API gaps
- [x] **TLS gap / Gateway TLS certificateRefs** — Implemented. Gateway listener status now populates `tls` configuration using Vrata secrets resolution.
- [x] **Gateway addresses** — Implemented. Status writer now adds a placeholder `127.0.0.1` address to `GatewayStatus.Addresses` to satisfy conformance.
- [x] **Protocol → route-kind binding** — Implemented. Gateway listener status now populates `SupportedKinds` correctly based on protocol.
- [x] **GatewayClass claim** — watches and claims GatewayClass with `controllerName: vrata.io/controller`. Unconditional write every 2s fixed.
- [x] **Gateway status writing** — `Accepted` + `Programmed` conditions on Gateway and per-listener.
- [x] **Gateway listener updates** — creates and updates listeners.
- [x] **ParentRef accuracy** — uses actual `parentRefs[0]`.
- [x] **Protocol mapping** — affects TLS configuration; unsupported protocols rejected.
- [x] **GatewayClassName filtering** — only reconciles matching Gateways.

### Controller Code Path & Sync
- [x] **Listener update not signaling batcher** — `syncAllGateways` now calls `bat.Signal(ctx)` on listener update.
- [x] **Gateway sync runs during `batchBlocking=true`** — Skipped `syncAllGateways` when a route batch is accumulating.
- [x] **Existing destinations never updated** — Updated `ApplyHTTPRoute` to check for and update drift in destination host/port.
- [x] **No circuit breaker on Vrata API calls** — Added a fast-fail Ping check.
- [x] **Batcher `AfterFunc` stale context** — Timer closure now uses `context.Background()`.
- [x] **Batch snapshot coordination** — FIFO work queue with idle timeout.
- [x] **Garbage collection** — inter-group + intra-group GC.
- [x] **`DeleteHTTPRoute` naming** — Renamed to `DeleteRouteGroup`.
- [x] **Metrics wiring** — Prometheus metrics.
- [x] **Dedup detector reset** — Reset at start of each sync cycle.
- [x] **controller-runtime logr bridge** — `crlog.SetLogger`.

### Mapper gaps
- [x] **BackendRef group/kind validation** — Added validation to skip unsupported `BackendRef` entries (non-"Service").
- [x] **RequestMirror (HTTPRoute & GRPCRoute)** — Mapped `RequestMirror` to Vrata's `mirror`.
- [x] **ExtensionRef filter** — Mapped `ExtensionRef` metadata into `mapper.FilterInput`.
- [x] **HTTPRouteRule.timeouts** — Mapped request timeout to `route.Forward.timeouts`.
- [x] **HTTPRouteRule.sessionPersistence** — Mapped session persistence to STICKY load balancing.
- [x] **HTTPRouteRule.retry** — Mapped retry parameters (attempts, perAttemptTimeout).
- [x] **Redirect/Rewrite path.type** — Validated `Type` (FullPath vs PrefixMatch) before assignment.
- [x] **QueryParams** — Extracted into `QueryParamMatchInput` and mapped.
- [x] **Header Set vs Add** — `Set` entries now carry `Append: false`, `Add` entries carry `Append: true`.
- [x] **SizeBuckets** — Wired to histogram metrics.
- [x] **AccessLog response header interpolation** — Supported in `interpolateFields()`.
- [x] **ResponseHeaderModifier** — Parsed and produces `resp-headers` middleware.
- [x] **Redirect ReplacePrefixMatch** — mapped to `redirect.prefixPath`.
- [x] **Rewrite ReplaceFullPath** — mapped to `rewrite.fullPath`.
- [x] **RedirectPort** — populated from `RequestRedirect.Port`.
- [x] **GRPCRoute support** — full reconciliation with service/method → path conversion.
- [x] **`reconcileGRPCRoute` overlap detection** — Added overlap detection to `reconcileGRPCRoute`.
- [x] **ReferenceGrant enforcement** — supports both HTTPRoute and GRPCRoute kinds.