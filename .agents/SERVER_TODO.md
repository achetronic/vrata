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
