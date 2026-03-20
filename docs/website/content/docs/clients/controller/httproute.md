---
title: "HTTPRoute Support"
weight: 3
---

The controller translates Gateway API HTTPRoutes into Vrata entities.

## Mapping

| Gateway API | Vrata entity |
|-------------|-------------|
| `HTTPRoute` | `RouteGroup` (1:1, carries hostnames) |
| `HTTPRoute.spec.rules[]` | `Route` (1 per rule) |
| `HTTPRoute.spec.rules[].matches[]` | Multiple `Route`s (1 per match) |
| `HTTPRoute.spec.rules[].backendRefs[]` | `Destination` (deduplicated by Service name+namespace+port) |
| `Gateway` | `Listener` (1 per gateway listener) |

## Supported match types

| Type | Example | Vrata field |
|------|---------|-------------|
| PathPrefix | `/api` | `match.pathPrefix` |
| Exact | `/health` | `match.path` |
| RegularExpression | `/users/[0-9]+` | `match.pathRegex` |
| Method | `POST` | `match.methods` |
| Header (exact) | `X-Tenant: acme` | `match.headers` |
| Header (regex) | `X-Version: v[0-9]+` | `match.headers` with `regex: true` |

## Supported filters

| Filter | Vrata action |
|--------|-------------|
| `RequestRedirect` | `route.redirect` |
| `URLRewrite` | `route.forward.rewrite` |
| `RequestHeaderModifier` | `Middleware` type=headers |

## Ownership

Every entity the controller creates has a `k8s:` prefix in its name:

- Routes: `k8s:{namespace}/{httproute-name}/rule-{i}/match-{j}`
- Groups: `k8s:{namespace}/{httproute-name}`
- Destinations: `k8s:{namespace}/{service-name}:{port}`

The controller only touches entities it owns. Manual API entities are never modified.

## Shared destinations

Multiple routes can reference the same Kubernetes Service. The controller maintains a reference count per destination. A destination is only deleted when no routes reference it anymore.

The refcount is rebuilt from the Vrata API on startup — no persistent storage needed.
