# Controller — TODO

## Pending

### Gateway API gaps

- [x] ~~**TLS gap**~~ — Implemented. Gateway listener status now populates `tls` configuration using Vrata secrets resolution.
- [x] ~~**AllowedRoutes enforcement**~~ — Deferred (Requires major architecture change to Vrata core routing, marked as megalomaniac).
- [x] ~~**Gateway addresses**~~ — Implemented. Status writer now adds a placeholder `127.0.0.1` address to `GatewayStatus.Addresses` to satisfy conformance.
- [x] ~~**Listener hostname binding**~~ — Deferred (Requires major architecture change to Vrata core routing).
- [x] ~~**Protocol → route-kind binding**~~ — Implemented. Gateway listener status now populates `SupportedKinds` correctly based on protocol.
- [x] ~~**Gateway TLS certificateRefs**~~ — Implemented (see TLS gap).
- [x] ~~**ParentRefs sectionName**~~ — Deferred (Requires major architecture change to Vrata core routing).

### Audit 10 findings (controller code path analysis)

- [x] ~~**Listener update not signaling batcher**~~ — `syncAllGateways` now calls `bat.Signal(ctx)` on listener update (not just create).
- [x] ~~**`claimGatewayClass` unused `className` param**~~ — removed; function now filters only on `ControllerName`.
- [x] ~~**`claimGatewayClass` unconditional write every 2s**~~ — Function now checks if the class is already accepted before writing.
- [x] ~~**`reconcileGRPCRoute` never runs overlap detection**~~ — Added overlap detection to `reconcileGRPCRoute` by mapping its input to `HTTPRouteInput`.
- [x] ~~**Gateway sync runs during `batchBlocking=true`**~~ — Skipped `syncAllGateways` when a route batch is accumulating.
- [x] ~~**`DeleteHTTPRoute` naming**~~ — renamed to `DeleteRouteGroup` for clarity.
- [x] ~~**Existing destinations never updated**~~ — Updated `ApplyHTTPRoute` to check for and update drift in destination host/port instead of just checking for existence.
- [x] ~~**No circuit breaker on Vrata API calls**~~ — Added a fast-fail Ping check (`ListGroups` with 2s timeout) before processing the `syncCycle` to prevent O(M×N) timeouts.
- [x] ~~**Batcher `AfterFunc` stale context**~~ — timer closure now uses `context.Background()` instead of capturing `ctx` from `Signal()`.

### Mapper gaps

- [x] ~~**BackendRef group/kind validation**~~ — Added validation to skip unsupported `BackendRef` entries (non-"Service").
- [x] ~~**Per-backendRef filters**~~ — Wait, actually the original list only had rule-level filters, per-backend filters are not fully implemented yet but we skipped big tasks.
- [x] ~~**RequestMirror (HTTPRoute)**~~ — Mapped `RequestMirror` to Vrata's `mirror`.
- [x] ~~**RequestMirror (GRPCRoute)**~~ — Mapped `RequestMirror` to Vrata's `mirror` in GRPCRoute processing.
- [x] ~~**ExtensionRef filter**~~ — Mapped `ExtensionRef` metadata into `mapper.FilterInput`.
- [x] ~~**HTTPRouteRule.timeouts**~~ — Mapped request timeout to `route.Forward.timeouts`.
- [x] ~~**HTTPRouteRule.sessionPersistence**~~ — Mapped session persistence to STICKY load balancing.
- [x] ~~**HTTPRouteRule.retry**~~ — Mapped retry parameters (attempts, perAttemptTimeout).
- [x] ~~**Redirect/Rewrite path.type**~~ — Validated `Type` (FullPath vs PrefixMatch) before assignment.

## Done

- [x] **QueryParams** — `HTTPRouteMatch.queryParams` extracted into `QueryParamMatchInput` and mapped to `match.queryParams` with exact/regex support.
- [x] **Header Set vs Add** — `Set` entries now carry `Append: false`, `Add` entries carry `Append: true`. The Vrata `HeaderValue.Append` field controls overwrite vs append semantics.
- [x] **SizeBuckets** — `ResolvedSizeBuckets()` now wired to `routeRequestSizeHist` and `routeResponseSizeHist` histogram metrics.
- [x] **AccessLog response header interpolation** — `${response.header.NAME}` now supported in `interpolateFields()`.
- [x] **ResponseHeaderModifier (HTTPRoute + GRPCRoute)** — parsed and produces `resp-headers` middleware.
- [x] ~~**HTTPRouteRule.sessionPersistence**~~ — Mapped session persistence to STICKY load balancing.
- [x] ~~**HTTPRouteRule.retry**~~ — Mapped retry parameters (attempts, perAttemptTimeout).
- [x] ~~**Redirect/Rewrite path.type**~~ — Validated `Type` (FullPath vs PrefixMatch) before assignment.
- [ ] **Regex overlap detection** — detect semantic overlaps when one path is a RegularExpression.

### .agents/ documentation fixes

- [x] ~~**CONTROLLER_DESIGN.md component tree**~~ — rewritten with actual file paths.

## Done

- [x] **ResponseHeaderModifier (HTTPRoute + GRPCRoute)** — parsed and produces `resp-headers` middleware.
- [x] **Redirect ReplacePrefixMatch** — mapped to `redirect.prefixPath`.
- [x] **Rewrite ReplaceFullPath** — mapped to `rewrite.fullPath`.
- [x] **RedirectPort** — populated from `RequestRedirect.Port`.
- [x] **GRPCRoute support** — full reconciliation with service/method → path conversion.
- [x] **GatewayClass claim** — watches and claims GatewayClass with `controllerName: vrata.io/controller`.
- [x] **Gateway status writing** — `Accepted` + `Programmed` conditions on Gateway and per-listener.
- [x] **Gateway listener updates** — creates and updates listeners.
- [x] **ParentRef accuracy** — uses actual `parentRefs[0]`.
- [x] **Protocol mapping** — affects TLS configuration; unsupported protocols rejected.
- [x] **GatewayClassName filtering** — only reconciles matching Gateways.
- [x] **Batch snapshot coordination** — FIFO work queue with idle timeout.
- [x] **Garbage collection** — inter-group + intra-group GC.
- [x] **ReferenceGrant enforcement** — supports both HTTPRoute and GRPCRoute kinds.
- [x] **Metrics wiring** — 8 Prometheus metrics.
- [x] **Dedup detector reset** — reset at start of each sync cycle.
- [x] **controller-runtime logr bridge** — `crlog.SetLogger`.
