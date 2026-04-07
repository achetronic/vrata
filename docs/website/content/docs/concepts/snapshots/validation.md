---
title: "Validation"
weight: 3
---

When you create a snapshot, the control plane checks the captured configuration for structural issues. Problems are returned as **warnings** in the response — the snapshot is still created, but you know what the proxy will skip when it applies it.

## Warnings, not errors

Snapshots always succeed. Warnings are informational — you decide what to do with them.

This matches how the proxy works: it skips broken entities and keeps running with everything else. Validation just moves that feedback earlier, to snapshot creation time instead of proxy logs.

## What gets checked

The control plane validates things that would **deterministically fail** on any proxy. Three categories:

### Compilation

Expressions and patterns are compiled to verify they're syntactically valid:

```json
{
  "entity": "route",
  "id": "r-broken",
  "name": "bad-regex-route",
  "message": "match.pathRegex does not compile: missing closing ]"
}
```

This covers regex patterns (paths, headers, query params, rewrites, CORS origins) and CEL expressions (route matching, middleware conditions, JWT claim assertions).

### TLS certificates

PEM-encoded certificates and keys are parsed to catch encoding errors before they reach the proxy:

```json
{
  "entity": "listener",
  "id": "l-secure",
  "name": "https",
  "message": "tls cert/key pair is invalid: x509: malformed certificate"
}
```

### Referential integrity

References between entities are verified — a route pointing to a destination that doesn't exist in the snapshot, a group listing a route ID that was deleted, a middleware referencing a missing service:

```json
{
  "entity": "route",
  "id": "r-orphan",
  "name": "api-route",
  "message": "forward.destinations references unknown destination \"d-deleted\""
}
```

## What is NOT checked

Anything that depends on the runtime environment:

- DNS resolution, upstream reachability, port availability
- External service connectivity (Redis, Kubernetes, JWKS endpoints)
- Network topology between proxy and backends

These can only be verified by the proxy itself at apply time.

## Example

Create a snapshot — warnings come back in the same response:

```bash
curl -X POST localhost:8080/api/v1/snapshots \
  -H 'Content-Type: application/json' \
  -d '{"name": "v2.0"}'
```

```json
{
  "id": "abc-123",
  "name": "v2.0",
  "createdAt": "2026-03-31T12:00:00Z",
  "snapshot": { "..." },
  "warnings": [
    {
      "entity": "route",
      "id": "r-broken",
      "name": "bad-regex-route",
      "message": "match.pathRegex does not compile: missing closing ]"
    }
  ]
}
```

An empty `warnings` array means the snapshot is clean and will compile fully on every proxy.
