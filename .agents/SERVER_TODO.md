# Server TODO — Vrata

## Pending

### Housekeeping

- [ ] Add authentication to the REST API

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
- [x] **CEL body access** — `request.body.raw` and `request.body.json` available in all CEL contexts (route matching, skipWhen/onlyWhen, inlineAuthz). Lazy buffering, configurable max size. See `SERVER_DECISIONS.md`.
- [x] **mTLS client authentication** — `clientAuth` on ListenerTLS with modes none/optional/require. Client cert metadata exposed in CEL (`request.tls.peerCertificate.*`). XFCC header injection with spoof protection. See `SERVER_DECISIONS.md`.
- [x] **Inline authorization middleware** — `inlineAuthz` type with ordered CEL rules, first-match-wins semantics, configurable deny response. See `SERVER_DECISIONS.md`.
