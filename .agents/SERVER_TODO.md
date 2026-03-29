# Server TODO — Vrata (xDS Branch)

## Pending

### xDS Translation Gaps

- [ ] **ExtProc native Envoy filter** — translate `extProc` middleware to `envoy.filters.http.ext_proc`. The model is ready, the xDS translation is missing.
- [ ] **CEL route matching** — `MatchRule.CEL` is in the model but not translated to xDS. Options: `envoy.filters.http.rbac` with CEL conditions, or a custom Go plugin that evaluates CEL.
- [ ] **Query param matchers** — `MatchRule.QueryParams` not translated to Envoy `QueryParameterMatcher`.
- [ ] **Port matchers** — `MatchRule.Ports` not translated.
- [ ] **gRPC content-type match** — `MatchRule.GRPC` not translated.
- [ ] **Listener timeouts in HCM** — `ListenerTimeouts` fields exist in the model but not mapped to Envoy HCM settings.
- [ ] **Listener.GroupIDs redesign** — owner doesn't like GroupIDs on Listener because routes are first-class citizens. Needs rethinking before touching code.

### Sticky sessions

- [ ] **Sticky response-side pinning** — `EncodeHeaders` in `extensions/sticky/filter.go` has a TODO: needs to extract upstream host from filter callbacks and write session→destination to Redis on first response.
- [ ] **Sticky Go plugin injection in HCM** — the sticky Go plugin should be automatically injected into the HCM filter chain when any route uses STICKY destination balancing. Currently tracked but not injected.

### Extensions

- [ ] **go mod tidy + go.sum** — all three extension modules need `go mod tidy` (requires network access to resolve deps).

### Housekeeping

- [ ] **Add authentication to the REST API**
- [ ] **xDS translator unit tests** — all translation functions lack tests (clusters, routes, listeners, middlewares, helpers).
- [ ] **E2E tests with Envoy** — no e2e test infrastructure that starts Envoy + control plane together.
- [ ] **Metrics on the control plane** — Envoy exports its own metrics, but the control plane itself has no Prometheus endpoint for xDS push counts, errors, latency.

### Multi-value matchers on MatchRule

`MatchRule` currently accepts a single `path`, `pathPrefix`, or `pathRegex` (mutually
exclusive). Supporting arrays with OR semantics would reduce entity count for the
controller. Impact: model changes + xDS translator changes.

### Proxy fleets — single control plane, multiple fleets

A single control plane should be able to manage multiple independent Envoy
fleets, each with its own routing config. A fleet identifier distinguishes
which config an Envoy receives when it connects via xDS. This allows one
control plane cluster to serve staging, production, and canary fleets.

## Done

- [x] **xDS ADS server** — gRPC server on :18000, ADS with snapshot cache
- [x] **Gateway → xDS push** — store events trigger full snapshot rebuild
- [x] **Cluster builder** — LB policy, circuit breaker, outlier detection, health checks, connect timeout, upstream TLS/mTLS, HTTP/2, max requests per connection, ring hash/maglev config
- [x] **Route builder** — forward (single/weighted), redirect, direct response, timeout, retry, rewrite (prefix/regex/host), mirror, hash policy (WCH/STICKY)
- [x] **Listener builder** — TLS termination, mTLS client auth, HCM with RDS, GroupIDs → selective VirtualHost
- [x] **CORS filter** — native `envoy.filters.http.cors`
- [x] **JWT filter** — native `envoy.filters.http.jwt_authn` (local + remote JWKS)
- [x] **ExtAuthz filter** — native `envoy.filters.http.ext_authz` (HTTP + gRPC)
- [x] **RateLimit filter** — native `envoy.filters.http.local_ratelimit`
- [x] **Headers filter** — native `envoy.filters.http.header_mutation`
- [x] **AccessLog** — HCM `access_log` with file logger, JSON/text, Vrata→Envoy variable mapping
- [x] **InlineAuthz Go plugin** — CEL evaluation, header + body access, lazy body buffering
- [x] **XFCC Go plugin** — strip + inject, auto on mTLS
- [x] **Sticky Go plugin** — request-side Redis lookup + header injection
- [x] **Envoy bootstrap example** — `extensions/ENVOY_BOOTSTRAP.md`
