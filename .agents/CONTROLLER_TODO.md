# Controller — TODO

## Pending

### Gateway API gaps

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths.
- [ ] **AllowedRoutes enforcement** — respect `listener.allowedRoutes.namespaces` (Same/All/Selector) and `allowedRoutes.kinds`.
- [ ] **Gateway addresses** — populate `GatewayStatus.Addresses`.
- [ ] **Listener hostname binding** — extracted but never used for route-to-listener binding.
- [ ] **Protocol → route-kind binding** — `GRPC` listener should only accept GRPCRoutes.
- [ ] **Gateway TLS certificateRefs** — `spec.listeners[].tls` completely ignored.
- [ ] **ParentRefs sectionName** — routes don't bind to specific listener sections.

### Audit 10 findings (controller code path analysis)

- [x] ~~**Listener update not signaling batcher**~~ — `syncAllGateways` now calls `bat.Signal(ctx)` on listener update (not just create).
- [x] ~~**`claimGatewayClass` unused `className` param**~~ — removed; function now filters only on `ControllerName`.
- [x] ~~**`claimGatewayClass` unconditional write every 2s**~~ — Function now checks if the class is already accepted before writing.
- [ ] **`reconcileGRPCRoute` never runs overlap detection** — accepts `detector`/`dupMode` params but never calls `detector.Check()`. GRPCRoutes skip overlap detection entirely.
- [ ] **Gateway sync runs during `batchBlocking=true`** — listener creates can trigger premature snapshot while a route batch is still accumulating.
- [x] ~~**`DeleteHTTPRoute` naming**~~ — renamed to `DeleteRouteGroup` for clarity.
- [ ] **Existing destinations never updated** — if Vrata state drifts (manual edit), `ApplyHTTPRoute` creates new destinations but never updates existing ones with different host/port.
- [ ] **No circuit breaker on Vrata API calls** — unreachable Vrata causes O(M×N) failing 30s HTTP calls per 2s cycle with no backoff.
- [x] ~~**Batcher `AfterFunc` stale context**~~ — timer closure now uses `context.Background()` instead of capturing `ctx` from `Signal()`.

### Mapper gaps

- [ ] **BackendRef group/kind validation** — `backendRef.group` and `backendRef.kind` never checked. Non-Service backends silently treated as Service.
- [ ] **Per-backendRef filters** — `backendRefs[].filters[]` never read (only rule-level filters).
- [ ] **RequestMirror (HTTPRoute)** — `HTTPRouteFilterRequestMirror` has no `case` in the switch.
- [ ] **RequestMirror (GRPCRoute)** — `GRPCRouteFilterRequestMirror` has no handling.
- [ ] **ExtensionRef filter** — `HTTPRouteFilterExtensionRef` and `GRPCRouteFilterExtensionRef` unhandled.
- [ ] **HTTPRouteRule.timeouts** — `HTTPRouteTimeouts` never read.
- [ ] **HTTPRouteRule.sessionPersistence** — never read.
- [ ] **HTTPRouteRule.retry** — `HTTPRouteRetry` never read.
- [ ] **Redirect/Rewrite path.type** — `HTTPPathModifier.Type` not checked; works by coincidence.

## Done

- [x] **QueryParams** — `HTTPRouteMatch.queryParams` extracted into `QueryParamMatchInput` and mapped to `match.queryParams` with exact/regex support.
- [x] **Header Set vs Add** — `Set` entries now carry `Append: false`, `Add` entries carry `Append: true`. The Vrata `HeaderValue.Append` field controls overwrite vs append semantics.
- [x] **SizeBuckets** — `ResolvedSizeBuckets()` now wired to `routeRequestSizeHist` and `routeResponseSizeHist` histogram metrics.
- [x] **AccessLog response header interpolation** — `${response.header.NAME}` now supported in `interpolateFields()`.
- [x] **ResponseHeaderModifier (HTTPRoute + GRPCRoute)** — parsed and produces `resp-headers` middleware.
- [ ] **HTTPRouteRule.sessionPersistence** — never read.
- [ ] **HTTPRouteRule.retry** — `HTTPRouteRetry` never read.
- [ ] **Redirect/Rewrite path.type** — `HTTPPathModifier.Type` not checked; both `ReplaceFullPath` and `ReplacePrefixMatch` read unconditionally (works by coincidence).
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
