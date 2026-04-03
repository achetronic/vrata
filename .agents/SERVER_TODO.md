# Server TODO — Vrata

## Pending

### Housekeeping

- [x] ~~**`SizeBuckets` not wired**~~ — Now wired as `routeRequestSizeHist` and `routeResponseSizeHist` histogram metrics using `ResolvedSizeBuckets()`.
- [x] ~~**Access log `${response.header.NAME}` interpolation**~~ — Now implemented in `interpolateFields()` with response header loop.
- [x] ~~**`mirrorRequest` goroutine leak**~~ — Now uses `context.WithTimeout(30s)` to prevent indefinite hangs.
- [x] ~~**Silent error swallowing (proxy)**~~ — All error discards now have explicit `_ =` assignment with justification comments.
- [x] ~~**CORS invalid regex silent drop**~~ — Now logs `slog.Error` with pattern and error message.
- [x] ~~**HeaderValue.Append default doc**~~ — Fixed doc: default is `false` (replace), matching Go zero value.
- [x] ~~**`regexCache` global state**~~ — Documented with justification comment. Low priority.
- [x] ~~**File naming violations (proxy)**~~ — Renamed `extauthz.go` → `ext_authz.go`, etc. to follow `snake_case` convention.
- [x] ~~**Handler naming violations**~~ — Renamed `VerbResource` → `HandleVerbResource` across 37 handlers. Breaking rename complete.

### Audit 9 findings (server internal model→consumer)

- [x] ~~**`ErrDuplicateRoute` and `ErrDuplicateGroup` dead code**~~ — removed unused error definitions from `model/errors.go`.
- [x] ~~**API validation gaps**~~ — Added validation to destinations, groups, listeners, and all middleware types (e.g. `jwt` issuer, `extAuthz` destinationId).
- [x] ~~**`sameMetrics()` shallow comparison**~~ — Updated `sameMetrics` to compare `Collect` and `Histograms` deeply.
- [x] ~~**Bolt store always emits `EventCreated`**~~ — Updated `Save` methods in `bolt.go` to emit `EventUpdated` when the entity already exists.
- [ ] **`RouteRewrite.Path` replaces full path, not prefix** — doc says "replaces the matched path prefix" but implementation does `r.URL.Path = rw.Path`. A request to `/api/v1/users` with rewrite `/internal` becomes `/internal`, not `/internal/users`.
- [ ] **PathRegex group + PathPrefix route composition** — produces exact-suffix match instead of prefix match. Requests beyond the prefix won't match.

### Audit 11 findings (middleware config field trace)

- [x] ~~**ExtAuthz gRPC: `OnCheck.InjectHeaders` not wired**~~ — Wired gRPC mode to inject headers.
- [x] ~~**ExtAuthz gRPC: `OnAllow.CopyToUpstream` not wired**~~ — Wired gRPC mode to filter upstream headers.
- [x] ~~**ExtAuthz gRPC: `OnDeny.CopyToClient` not wired**~~ — Wired gRPC mode to filter deny headers.

### Audit 12 findings (config cross-reference)

- [x] ~~**No reference `server/config.yaml`**~~ — Created `server/config.yaml` reference file with all available options documented.
- [x] ~~**`proxy.celBodyMaxSize`**~~ — Added to Helm `values.yaml`.
- [x] ~~**`sessionStore.*`**~~ — Added to Helm `values.yaml`.
- [x] ~~**File naming violations (proxy)**~~ — Renamed `extauthz.go` → `ext_authz.go`, etc. to follow `snake_case` convention.
- [x] ~~**Handler naming violations**~~ — Renamed `VerbResource` → `HandleVerbResource` across 37 handlers. Breaking rename complete.
- [x] ~~**Timeout naming convention migration**~~ — Decision status updated to Implemented.

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
