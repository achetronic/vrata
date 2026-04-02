# Server TODO — Vrata

## Pending

### Housekeeping

- [ ] **`SizeBuckets` not wired** — `ListenerMetrics.Histograms.SizeBuckets` defined in model, `ResolvedSizeBuckets()` helper exists, but `proxy/metrics.go` never uses them. Request/response sizes are counters, not histograms. Either wire as histograms or remove from model.
- [ ] **Access log `${response.header.NAME}` interpolation** — documented in `AccessLogEntry` docstring as supported but `middlewares/accesslog.go:interpolateFields()` only handles `${request.header.NAME}`. Response header interpolation missing.
- [ ] **`regexCache` global state** — `proxy/handler.go:855` has `var regexCache sync.Map`. Convention forbids package-level mutable state. Should be moved to a `RoutingTable` field or passed via Dependencies.
- [ ] **`mirrorRequest` goroutine leak** — `proxy/handler.go:788-791` fires a goroutine with no cleanup, no timeout, no stop function. Hung mirror upstream leaks the goroutine indefinitely.
- [ ] **Silent error swallowing (proxy)** — `BufferBody` errors discarded at `handler.go:192` and `router.go:258` without comment. `srv.ServeTLS/Serve` return errors discarded at `listener.go:273-275`. `srv.Shutdown` error discarded at `listener.go:269`.
- [ ] **File naming violations (proxy)** — `extauthz.go` → `ext_authz.go`, `extproc.go` → `ext_proc.go`, `accesslog.go` → `access_log.go`, `inlineauthz.go` → `inline_authz.go`, `headermatch.go` → `header_match.go`. Model: `inlineauthz.go` → `inline_authz.go`, `accesslog.go` → `access_log.go`.
- [ ] **Handler naming violations** — all 37 handlers in `api/handlers/` use `VerbResource` instead of `HandleVerbResource` (e.g. `ListRoutes` → `HandleListRoutes`). This is a large breaking rename affecting API router, tests, and swagger annotations.
- [ ] **Timeout naming convention migration** — `SERVER_DECISIONS.md` documents semantic timeout names as "Decided — not yet implemented". The model already uses the semantic names, but the decision entry status is misleading. Either mark as implemented or document the remaining migration gap.

### Proxy: not-wired features

(All previously listed items are now wired — see Done section.)

### Destination priority levels

Upstream failover via priority levels on `DestinationRef`. Destinations with
lower priority numbers are preferred; higher-priority destinations are only
used when all lower-priority destinations are unhealthy. Weights only compete
within the same priority level. Binary semantics (no spillover) for v1.
See discussion in `SERVER_DECISIONS.md` (onError removal rationale).

### Multi-value matchers on MatchRule

`MatchRule` currently accepts a single `path`, `pathPrefix`, or `pathRegex` (mutually
exclusive). Supporting arrays (`paths []string`, `pathPrefixes []string`,
`pathRegexes []string`) with OR semantics would allow one Route to match
multiple paths. This reduces the number of entities the controller creates —
an HTTPRoute rule with 3 matches becomes 1 Route instead of 3.

### Proxy fleets — single control plane, multiple fleets

A single control plane should be able to manage multiple independent proxy
fleets, each with its own routing config. A fleet identifier (e.g. a label
or a path parameter) distinguishes which config a proxy receives when it
connects via SSE.

## Done

- [x] **sameListener Timeouts comparison** — `sameListener()` now compares `Timeouts`.
- [x] **CircuitBreaker.MaxPendingRequests** — `AllowPending()`/`OnPending()`/`OnPendingComplete()` wired.
- [x] **CircuitBreaker.MaxRetries** — `AllowRetry()`/`OnRetry()`/`OnRetryComplete()` wired into `retryTransport`.
- [x] **LeastRequest.ChoiceCount** — power-of-two-choices via `sampleDests()`.
- [x] **OutlierDetection.Interval** — `resolveInterval()` reads config, default 10s.
- [x] **OutlierDetection.MaxEjectionPercent** — `maxEjectionReached()` caps ejections per destination.
- [x] **MiddlewareOverride.Headers** — `buildMiddleware()` merges header overrides via `mergeHeadersConfig()`.
- [x] **MiddlewareOverride.ExtProc** — `buildMiddleware()` merges ExtProc phases and allowOnError.
- [x] **ExtProcConfig.MetricsPrefix** — used as metrics label name when present.
- [x] **h2c downstream + upstream + streaming flush** — see `SERVER_DECISIONS.md`.
- [x] **onError removed + proxyErrors** — see `SERVER_DECISIONS.md`.
- [x] **CEL body access** — see `SERVER_DECISIONS.md`.
- [x] **mTLS client authentication** — see `SERVER_DECISIONS.md`.
- [x] **Inline authorization middleware** — see `SERVER_DECISIONS.md`.
- [x] **Control plane security** — see `SERVER_DECISIONS.md`.
