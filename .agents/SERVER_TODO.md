# Server TODO — Vrata

## Pending

### Housekeeping

- [ ] Add authentication to the REST API
- [ ] E2e tests for skipWhen/onlyWhen
- [ ] E2e test for assertClaims

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
an HTTPRoute rule with 3 matches becomes 1 Route instead of 3. Impact:
- `model/group.go` MatchRule struct changes
- `proxy/router.go` compiledRoute.match() iterates arrays
- `proxy/table.go` compileRoute handles group+route composition per array entry
- ~38% fewer routes for real-world HTTPRoute workloads (6961 → ~4310)

### Proxy fleets — single control plane, multiple fleets

A single control plane should be able to manage multiple independent proxy
fleets, each with its own routing config. A fleet identifier (e.g. a label
or a path parameter) distinguishes which config a proxy receives when it
connects via SSE. This allows one control plane cluster to serve staging,
production, and canary fleets without separate deployments.

## Done

- [x] **onError removed + proxyErrors** — removed `Route.OnError`, `OnErrorRule`,
  `RouteAction`, and all onError dispatch logic. Proxy error responses are now
  structured JSON with configurable detail level (`minimal`/`standard`/`full`)
  per listener via `listener.proxyErrors.detail`. See `SERVER_DECISIONS.md`.
- [x] **RouteAction refactor** (superseded) — extracted then removed when onError was deleted.

---

### CEL body access (`request.body.raw` + `request.body.json`)

Generic extension of the CEL evaluator. Two new fields in the `request` map:

- `request.body.raw` — `string`, always populated (up to `celBodyMaxSize`)
  when a CEL program in the request path references `request.body`. Raw bytes
  of the request body. Works for any content type.
- `request.body.json` — `map(string, dyn)`, only populated when `Content-Type`
  is `application/json` and the parse succeeds. Field does not exist otherwise
  (`has(request.body.json)` returns false).

See `SERVER_DECISIONS.md` for the full design rationale and constraints.

#### Body buffering

- [x] **Body buffer utility** — in `proxy/celeval/cel.go`: `BufferBody()` reads
  `r.Body` into buffer, replaces with `io.NopCloser`, stores in request context.
  Configurable max size (default 64KB). Truncates raw, skips json on exceed.
  Fail-open with `slog.Warn`.
  - **Lazy**: only triggered when a CEL program references `request.body`.
    `needsBody bool` flag on compiled route/middleware determined at build time.
  - Buffer cached in request context — re-used across route match +
    skipWhen/onlyWhen + inlineAuthz rules.

#### CEL evaluator changes

- [x] **New CEL variables** — `request.body.raw` (string) and
  `request.body.json` (map, conditional on Content-Type).
- [x] **JSON parsing** — conditional on `Content-Type: application/json`.
  `json.Number` for numeric precision. Parse error → json absent, raw present.
- [x] **needsBody flag** — `Program.NeedsBody()` checks expression for
  `request.body` reference. Set on compiled route and used at request time.
- [x] **Integration with route matching** — `compiledRoute.match()` calls
  `BufferBody()` when `needsBody` is true. Returns updated `*http.Request`.
- [x] **Integration with middleware conditions** — `skipWhen`/`onlyWhen` in
  `wrapWithConditions()` detects `NeedsBody()` and buffers before evaluation.
  `inlineAuthz` rules buffer independently. All share the same context cache.

#### Configuration

- [x] **Config field** — `proxy.celBodyMaxSize` (int, default 65536) in
  `ProxyConfig`. Threaded through gateway/sync Dependencies → BuildTable →
  compileRoute → compiledRoute.celBodyMaxSize.

#### Testing

- [x] **Unit tests** (18): buffer + re-read, exceed max size, empty body,
  non-JSON body, invalid JSON, cached on second call, JSON with charset,
  numeric precision, NeedsBody flag, JSON field access, nested access,
  in-list, missing field, raw contains, without buffering, no-body-no-read,
  BodyFromCtx nil, NoBody.
- [x] **Router integration tests** (3): CEL body JSON match, CEL body raw
  match, non-body CEL does not buffer.
- [x] **E2e tests** (2): CEL body JSON route matching, CEL body raw matching.

**Files**: `proxy/celeval/cel.go`, `proxy/router.go`, `proxy/handler.go`,
`proxy/table.go`, `internal/config/config.go`

---

### mTLS client authentication on listeners

Generic extension of the existing `ListenerTLS` model. Listeners can require
or optionally request client certificates. When a client cert is verified, its
metadata is exposed in the CEL evaluation context and injected as a request
header for downstream consumption.

See `SERVER_DECISIONS.md` for the full design rationale and constraints.

#### Model

- [x] **New field on `ListenerTLS`** — `ClientAuth *ListenerClientAuth` with
  `Mode` (none/optional/require) and `CAFile`.
- [x] **API validation** in `api/handlers/listener.go`: rejects unknown modes,
  requires `CAFile` when mode is `optional` or `require`. 6 unit tests.

#### Listener wiring

- [x] **TLS config** — `proxy/listener.go` configures `tls.Config.ClientAuth`
  and loads `ClientCAs` from `CAFile`. Rejects unknown modes at startup.
- [x] **Listener diffing** — `sameTLS()` extended with `sameClientAuth()` so
  listeners restart when client auth config changes.

#### CEL variables

- [x] **New CEL variables** — `request.tls.peerCertificate.{uris, dnsNames,
  subject, serial}` populated from `r.TLS.PeerCertificates[0]`. Empty when
  no cert. Available in all CEL contexts (route match, skipWhen, onlyWhen,
  inlineAuthz).

#### XFCC header injection

- [x] **X-Forwarded-Client-Cert** — injected in `forwardHandler` with URI SANs
  (semicolon-separated).
- [x] **Strip incoming XFCC** — `r.Header.Del("X-Forwarded-Client-Cert")`
  before injection to prevent spoofing.

#### Testing

- [x] **Unit tests for CEL cert** (7): SPIFFE URI match, DNS SAN match,
  subject contains, serial match, multiple URIs, no cert, non-TLS.
- [x] **Unit tests for XFCC** (4): injected, spoofed stripped, no cert no
  header, multiple URIs semicolon-separated.
- [x] **Unit tests for listener diffing** (6): both nil, one nil, equal,
  different mode, different CA, sameTLS with clientAuth.
- [ ] **E2e test**: mTLS listener + CEL expression matching SPIFFE URI
  (requires real cert generation in test — deferred to controller e2e)

**Files**: `model/listener.go`, `proxy/listener.go`, `proxy/celeval/cel.go`,
`proxy/handler.go`

---

### Inline authorization middleware (`inlineAuthz`)

New middleware type. Symmetric pair of `extAuthz`: evaluates authorization
locally with ordered CEL rules instead of delegating to an external service.

See `SERVER_DECISIONS.md` for the full design rationale and constraints.

#### Model

- [x] **New fields on `Middleware`** — `InlineAuthz *InlineAuthzConfig` in
  `model/middleware.go`. `InlineAuthzConfig` and `InlineAuthzRule` in
  `model/inlineauthz.go`.
- [x] **`"inlineAuthz"` added to `MiddlewareType` enum**.
- [x] **API validation** in `api/handlers/middleware.go`: rejects missing config,
  empty rules, unknown actions, empty CEL, invalid CEL (compile-checked at
  creation time), unknown defaultAction. 8 unit tests.

#### Middleware implementation

- [x] **`proxy/middlewares/inlineauthz.go`** — compile CEL at build time,
  first-match-wins evaluation, allow/deny actions, configurable denyStatus +
  denyBody (written directly, not double-wrapped). Lazy body buffering when
  any rule references `request.body`. Fault isolation: bad CEL rule skipped.
- [x] **`InlineAuthzNeedsBody()`** helper for table build.

#### Table build integration

- [x] **`buildMiddleware()`** in `handler.go` handles `"inlineAuthz"` type,
  passes `celBodyMaxSize`.

#### Testing

- [x] **Unit tests** (14): single allow, single deny, first-match-wins,
  default allow, default deny, custom denyStatus + denyBody, body JSON rule,
  full MCP scenario (7 subtests), CEL compile error skipped, nil config,
  NeedsBody detection, TLS cert rule.
- [x] **E2e tests** (2): MCP full scenario (8 subtests: GET/DELETE/initialize/
  tools-list/add/subtract allowed, evil/unknown denied), default allow.

**Files**: `model/middleware.go`, `model/inlineauthz.go`, 
`proxy/middlewares/inlineauthz.go`, `proxy/handler.go`
