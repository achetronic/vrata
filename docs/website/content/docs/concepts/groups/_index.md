---
title: "Groups"
weight: 4
---

A RouteGroup is a logical namespace that bundles routes together and injects shared configuration — path prefixes, hostnames, headers, and middlewares. Instead of repeating the same config on 20 routes, you define it once on the group.

## What a group does

1. **Composes paths** — prepends a path prefix (or regex) to every route in the group
2. **Adds hostnames** — merges group hostnames with route hostnames
3. **Adds headers** — merges group header matchers with route header matchers
4. **Attaches middlewares** — applies middlewares to all routes, with per-route override capability
5. **Sets retry defaults** — default retry policy for all routes in the group

## Creating a group

```bash
curl -X POST localhost:8080/api/v1/groups \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "store-api",
    "routeIds": ["<route-products-id>", "<route-orders-id>"],
    "pathPrefix": "/api/v1",
    "hostnames": ["api.example.com"]
  }'
```

## Structure

```json
{
  "name": "store-api",
  "routeIds": ["<route-id-1>", "<route-id-2>"],
  "pathPrefix": "/api/v1",
  "pathRegex": "",
  "hostnames": ["api.example.com"],
  "headers": [{"name": "X-Tenant", "value": "acme"}],
  "middlewareIds": ["jwt-auth", "cors"],
  "middlewareOverrides": { ... },
  "retryDefault": { ... },
  "includeAttemptCount": false
}
```

## All fields

| Field | Type | Default | Description | Details |
|-------|------|---------|-------------|---------|
| `name` | string | required | Unique name | — |
| `routeIds` | array | required | Route IDs in this group | — |
| `pathPrefix` | string | — | Path prefix prepended to all routes | [Path Composition]({{< relref "path-composition" >}}) |
| `pathRegex` | string | — | Regex path prepended to all routes | [Path Composition]({{< relref "path-composition" >}}) |
| `hostnames` | array | — | Hostnames merged with route hostnames (no limit) | — |
| `headers` | array | — | Header matchers merged with route headers | — |
| `middlewareIds` | array | — | Middlewares applied to all routes in order | [Middleware Inheritance]({{< relref "middleware-inheritance" >}}) |
| `middlewareOverrides` | map | — | Per-middleware overrides (skipWhen, onlyWhen, disabled) | [Middleware Inheritance]({{< relref "middleware-inheritance" >}}) |
| `retryDefault` | object | — | Default retry policy for all routes | — |
| `includeAttemptCount` | bool | `false` | Add `X-Vrata-Attempt-Count` header to upstream | — |

## Routes without groups

Routes don't need to belong to a group. A standalone route with its own `match`, `middlewareIds`, and `forward` works perfectly. Groups are useful when you have multiple routes that share common config.

A route can belong to multiple groups. The group configuration is merged at routing table build time — each group+route combination creates a separate entry in the routing table.
