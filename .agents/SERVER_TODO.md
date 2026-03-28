# Server TODO — Vrata

## Pending

### Housekeeping

- [ ] Add authentication to the REST API
- [ ] E2e tests for skipWhen/onlyWhen
- [ ] E2e test for assertClaims

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

### CEL body access (`request.body.raw` + `request.body.json`)

**Date**: 2026-03-25

Generic extension of the CEL evaluator. `request.body.raw` (string, always)
and `request.body.json` (map, only when Content-Type is application/json).
Lazy buffering: only triggered when a CEL program references `request.body`.
Configurable via `proxy.celBodyMaxSize` (default 64KB). Available in route
match CEL, skipWhen/onlyWhen, and inlineAuthz rules.

See `SERVER_DECISIONS.md` for design rationale.

**Tests**: 22 unit (body buffer + edge cases) + 3 router integration + 1 combined body+TLS + 2 e2e.

### mTLS client authentication on listeners

**Date**: 2026-03-25

`ListenerTLS.ClientAuth` with mode `none`/`optional`/`require` and `caFile`.
Client cert fields exposed in CEL: `request.tls.peerCertificate.{uris, dnsNames,
subject, serial}`. Automatic `X-Forwarded-Client-Cert` header injection with
spoof protection. API validation rejects unknown modes and missing caFile.

See `SERVER_DECISIONS.md` for design rationale.

**Tests**: 8 CEL cert + 4 XFCC + 6 listener diffing + 6 API validation.

### Inline authorization middleware (`inlineAuthz`)

**Date**: 2026-03-25

New middleware type. Symmetric pair of `extAuthz`: evaluates authorization
locally with ordered CEL rules. First-match-wins, allow/deny actions,
configurable denyStatus + denyBody. Lazy body buffering. Fault isolation
for bad CEL rules. API validation rejects empty rules, unknown actions,
and invalid CEL at creation time.

See `SERVER_DECISIONS.md` for design rationale.

**Tests**: 14 unit + 8 API validation + 5 router integration (MCP body rules,
skipWhen body, onlyWhen body, middleware disabled) + 2 e2e (full MCP scenario).
