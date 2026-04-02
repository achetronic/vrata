# Controller — TODO

## Pending

### Gateway API gaps

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **AllowedRoutes enforcement** — respect `listener.allowedRoutes.namespaces` (Same/All/Selector) and `allowedRoutes.kinds` (HTTPRoute, GRPCRoute) from Gateway spec.
- [ ] **Gateway addresses** — populate `GatewayStatus.Addresses` from the Vrata listener state or from the Kubernetes Service.
- [ ] **Listener hostname binding** — `GatewayListenerInput.Hostname` is extracted but never used for route-to-listener binding.
- [ ] **Protocol → route-kind binding** — a `GRPC` protocol listener should only accept GRPCRoutes. Currently no filtering.
- [ ] **Gateway TLS certificateRefs** — `spec.listeners[].tls` (certificateRefs, mode, options) completely ignored.
- [ ] **ParentRefs sectionName** — routes don't bind to specific listener sections. All routes attach to all listeners.

### Mapper gaps

- [ ] **QueryParams** — `HTTPRouteMatch.queryParams` never extracted. Vrata model supports `match.queryParams`.
- [ ] **Header Set vs Add conflation** — both `RequestHeaderModifier.Set` and `.Add` are appended to the same `HeadersToAdd` list. Gateway API specifies `Set` overwrites while `Add` appends. Same for `ResponseHeaderModifier`.
- [ ] **BackendRef group/kind validation** — `backendRef.group` and `backendRef.kind` never checked. Non-Service backends silently treated as Service.
- [ ] **Per-backendRef filters** — `backendRefs[].filters[]` never read (only rule-level filters).
- [ ] **RequestMirror (HTTPRoute)** — `HTTPRouteFilterRequestMirror` has no `case` in the switch.
- [ ] **RequestMirror (GRPCRoute)** — `GRPCRouteFilterRequestMirror` has no handling.
- [ ] **ExtensionRef filter** — `HTTPRouteFilterExtensionRef` and `GRPCRouteFilterExtensionRef` unhandled.
- [ ] **HTTPRouteRule.timeouts** — `HTTPRouteTimeouts` never read.
- [ ] **HTTPRouteRule.sessionPersistence** — never read.
- [ ] **HTTPRouteRule.retry** — `HTTPRouteRetry` never read.
- [ ] **Redirect/Rewrite path.type** — `HTTPPathModifier.Type` not checked; both `ReplaceFullPath` and `ReplacePrefixMatch` read unconditionally (works by coincidence).
- [ ] **Regex overlap detection** — detect semantic overlaps when one path is a RegularExpression.

### .agents/ documentation fixes

- [ ] **CONTROLLER_DESIGN.md component tree** — lists nonexistent files (`reconciler/gateway.go`, `reconciler/httproute.go`, `reconciler/refcount.go`, `Makefile`, `config/crd/superhttproute.yaml`). Missing real packages (`workqueue/`, `dedup/`, `metrics/`, `refgrant/`).

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
