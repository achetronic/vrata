# Server TODO — Vrata

## Deferred / Future Epics

These features are conceptually large and have been deferred to avoid major architecture changes in v1:

- **Destination priority levels**: Upstream failover via priority levels on `DestinationRef`. Destinations with lower priority numbers are preferred.
- **Multi-value matchers on MatchRule**: Supporting arrays (`paths []string`, `pathPrefixes []string`, `pathRegexes []string`) with OR semantics would allow one Route to match multiple paths.
- **Proxy fleets**: A single control plane should be able to manage multiple independent proxy fleets, each with its own routing config.

## Open

### Security

- [x] **`clientIp` trusts `X-Forwarded-For` unconditionally** — Fixed: `clientIp` field on `Listener` with `source` (direct/xff/header), `trustedCidrs`, `numTrustedHops`, and `header` fields. Resolved IP stored in context via atomic pre-processor (hot-swap, no listener restart). Available to CEL route matching, inlineAuthz, access log. Unit tests (20) + e2e tests (7 scenarios including hot-reload and validation).

### Hardening

- [ ] **Proxy mode has no admin HTTP server** — no readiness/liveness endpoint for load balancers. A health endpoint on a configurable admin port would be useful.
- [ ] **No readiness gate on control plane startup** — the REST API starts listening before the gateway completes its first rebuild. Clients could hit the API before the routing table is populated.
- [x] **`HandleUpdateSecret` missing input validation** — Fixed: added name/value validation to `HandleUpdateSecret`, matching `HandleCreateSecret` checks.
- [ ] **Memory store `publish()` under lock** — `publish()` called while holding `s.mu.Lock()`. Potential deadlock if a subscriber synchronously calls a read method.
- [ ] **K8s watcher `buildEndpoints` first-port bias** — Always takes the first port from an EndpointSlice, ignoring `destPort` matching for multi-port Services.
- [ ] **Raft write-forwarding has no retry** — Single HTTP call to leader; fails without retry on leader election change.
- [x] **Bolt `Restore()` does not restore the `meta` bucket** — Fixed: `bucketMeta` now included in `dataBuckets` list. Event type changed from `EventCreated` to `EventUpdated`.
- [x] **Missing `yaml` struct tags on `destination.go` types** — Fixed: all types now have both `json` and `yaml` tags.

## Done

### Housekeeping & Bugs

- [x] **`RouteRewrite.Path` replaces full path, not prefix** — Fixed `applyRewrite` to replace only the matched prefix.
- [x] **PathRegex group + PathPrefix route composition** — Fixed regex composition to correctly escape prefix and prevent exact-suffix matching.
- [x] **`SizeBuckets` not wired** — Wired as `routeRequestSizeHist` and `routeResponseSizeHist`.
- [x] **Access log interpolation** — Implemented in `interpolateFields()`.
- [x] **`mirrorRequest` goroutine leak** — Now uses `context.WithTimeout(30s)`.
- [x] **Silent error swallowing (proxy)** — All error discards now have explicit `_ =` assignment.
- [x] **CORS invalid regex silent drop** — Now logs `slog.Error`.
- [x] **HeaderValue.Append default doc** — Fixed doc to state default is `false`.
- [x] **`regexCache` global state** — Documented with justification comment.
- [x] **File naming violations (proxy)** — Renamed `extauthz.go` → `ext_authz.go`, etc.
- [x] **Handler naming violations** — Renamed `VerbResource` → `HandleVerbResource` across 37 handlers.

### Audit findings

- [x] **`ErrDuplicateRoute` and `ErrDuplicateGroup` dead code** — Removed unused error definitions.
- [x] **API validation gaps** — Added validation to destinations, groups, listeners, and all middleware types.
- [x] **`sameMetrics()` shallow comparison** — Updated `sameMetrics` to compare `Collect` and `Histograms` deeply.
- [x] **Bolt store always emits `EventCreated`** — Updated `Save` methods to emit `EventUpdated` when appropriate.
- [x] **ExtAuthz gRPC wiring** — Wired `OnCheck.InjectHeaders`, `OnAllow.CopyToUpstream`, and `OnDeny.CopyToClient`.
- [x] **No reference `server/config.yaml`** — Created `server/config.yaml`.
- [x] **`proxy.celBodyMaxSize` & `sessionStore.*`** — Added to Helm `values.yaml`.
- [x] **Timeout naming convention migration** — Implemented.

### General proxy features

- [x] **sameListener Timeouts comparison** — Wired.
- [x] **CircuitBreaker MaxPendingRequests & MaxRetries** — Wired.
- [x] **LeastRequest ChoiceCount** — Wired.
- [x] **OutlierDetection Interval & MaxEjectionPercent** — Wired.
- [x] **MiddlewareOverride Headers & ExtProc** — Wired.
- [x] **ExtProcConfig.MetricsPrefix** — Wired.
- [x] **h2c downstream + upstream + streaming flush** — Implemented.
- [x] **onError removed + proxyErrors** — Implemented.
- [x] **CEL body access & mTLS client authentication** — Implemented.
- [x] **Inline authorization middleware** — Implemented.
- [x] **Control plane security** — Implemented.

### Audit 2 findings (2026-04-03)

- [x] **CEL body truncation corrupts upstream request** — `BufferBody` now reads the full body, uses a truncated copy for CEL, and replaces `r.Body` with the complete original. On read error, `r.Body` is replaced with an empty reader.
- [x] **`ClaimsStringProgram.Eval` returns `"<nil>"`** — Added nil check before `fmt.Sprintf`; now returns empty string on nil result.
- [x] **Middleware `*WithStop` returns nil stop function** — JWT, ExtProc, RateLimit, and AccessLog now return `func(){}` instead of `nil` on early-return paths, consistent with ExtAuthz.
- [x] **`err.Error()` leaked to client in API responses** — All 9 handlers that appended `err.Error()` to 400 messages now use static strings. The snapshot 500 now logs the error server-side and returns a generic message.
- [x] **`DestinationLBPolicy` godoc fragment** — Fixed to proper `// DestinationLBPolicy controls...` format.
- [x] **`SERVER_DECISIONS.md` Middleware → Listener reference** — Corrected: middlewares are referenced by Route and RouteGroup, not by Listener.

### Audit 5 findings (2026-03-31)

- [x] **h2c upstream was not real cleartext HTTP/2** — `http2.ConfigureTransport` does not enable h2c. Replaced with `http2.Transport{AllowHTTP: true, DialTLSContext: plaintext dialer}`. Added `RoundTripper` field to `Endpoint`.
- [x] **`classifyError` fallback misclassified unknown errors** — Catch-all returned `connection_refused` instead of `unknown`. Added `ProxyErrUnknown` constant.
- [x] **Proxy error types `no_route` and `request_headers_too_large` were string literals** — Added `ProxyErrNoRoute` and `ProxyErrRequestHeadersTooLarge` constants to model. Replaced inline strings.
