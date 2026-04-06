# Server TODO — Vrata

## Deferred / Future Epics

These features are conceptually large and have been deferred to avoid major architecture changes in v1:

- **Destination priority levels**: Upstream failover via priority levels on `DestinationRef`. Destinations with lower priority numbers are preferred.
- **Multi-value matchers on MatchRule**: Supporting arrays (`paths []string`, `pathPrefixes []string`, `pathRegexes []string`) with OR semantics would allow one Route to match multiple paths.
- **Proxy fleets**: A single control plane should be able to manage multiple independent proxy fleets, each with its own routing config.

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