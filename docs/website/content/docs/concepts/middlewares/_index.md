---
title: "Middlewares"
weight: 5
---

A Middleware is a reusable behaviour that runs before (or around) the request being forwarded to the upstream. Middlewares handle cross-cutting concerns like authentication, rate limiting, CORS, and header manipulation — without modifying your backend code.

## What a middleware does

1. **Inspects the request** — checks tokens, headers, IP addresses, or delegates to an external service
2. **Decides allow or deny** — returns 401/403/429 if the request doesn't pass
3. **Mutates request/response** — adds headers, rewrites values, logs access
4. **Runs conditionally** — CEL expressions control when each middleware executes

Middlewares are independent entities managed via the API. You create them once and attach them to any number of routes or groups.

## Creating a middleware

```bash
curl -X POST localhost:8080/api/v1/middlewares \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-middleware",
    "type": "<type>",
    "<type>": { ... }
  }'
```

Set `type` to the middleware type and populate the matching field. See each type's sub-page for the full configuration.

## Structure

```json
{
  "name": "my-middleware",
  "type": "jwt",
  "jwt": { ... },
  "cors": { ... },
  "rateLimit": { ... },
  "extAuthz": { ... },
  "extProc": { ... },
  "headers": { ... },
  "accessLog": { ... },
  "inlineAuthz": { ... }
}
```

Set `type` and populate only the matching field. All other type fields are ignored.

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | required | Unique name |
| `type` | string | required | One of the types below |

## Available types

| Type value | Sub-page | What it does |
|------------|----------|-------------|
| `jwt` | [JWT Authentication]({{< relref "jwt" >}}) | Validate JWT tokens, assert claims, inject claims as headers |
| `cors` | [CORS]({{< relref "cors" >}}) | Cross-Origin Resource Sharing with preflight handling |
| `rateLimit` | [Rate Limiting]({{< relref "rate-limit" >}}) | Token bucket per client IP |
| `extAuthz` | [External Authorization]({{< relref "ext-authz" >}}) | Delegate auth decisions to an external HTTP/gRPC service |
| `extProc` | [External Processor]({{< relref "ext-proc" >}}) | Send request/response phases to a service for inspection and mutation |
| `headers` | [Header Manipulation]({{< relref "headers" >}}) | Add/remove request and response headers |
| `accessLog` | [Access Log]({{< relref "access-log" >}}) | Structured access logging per route |
| `inlineAuthz` | [Inline Authorization]({{< relref "inline-authz" >}}) | CEL-based authorization rules evaluated locally |

## Attaching to routes

Reference middlewares by ID in a route's `middlewareIds`:

```json
{
  "middlewareIds": ["jwt-auth", "cors", "rate-limit"]
}
```

Middlewares execute in the order listed. If any middleware rejects the request, subsequent middlewares and the forward action are skipped.

## Attaching to groups

When attached to a group, the middleware applies to all routes in that group:

```json
{
  "middlewareIds": ["jwt-auth", "cors"]
}
```

## Conditional execution

Every middleware supports `skipWhen`, `onlyWhen`, and `disabled` overrides via CEL expressions. See [Conditions]({{< relref "conditions" >}}) for full details.

| Control | Meaning |
|---------|---------|
| `skipWhen` | Skip if **any** expression matches |
| `onlyWhen` | Run only if **at least one** matches |
| `disabled` | Completely disable for this route |

Route overrides win over group overrides.
