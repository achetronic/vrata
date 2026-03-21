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

## Garbage collection

The controller automatically cleans up Vrata when Kubernetes resources change:

- **Deleted HTTPRoute** — if you delete an HTTPRoute from Kubernetes, the controller removes its group, routes, middlewares, and any destinations that are no longer referenced by other routes.
- **Removed matches or rules** — if you edit an HTTPRoute to remove a match or an entire rule, the controller deletes the stale routes and middlewares from Vrata on the next sync cycle.
- **Renamed HTTPRoute** — if you delete an HTTPRoute and create a new one with a different name, the old entities are cleaned up and the new ones are created.

Destination cleanup is always safe: a destination is only deleted when no route references it anymore. If two HTTPRoutes point to the same Service, deleting one won't affect the other.

## Cross-namespace references

If an HTTPRoute references a Service in a different namespace, the controller checks for a `ReferenceGrant` in the target namespace. If no matching grant exists, the entire HTTPRoute is skipped and its status is set to `ResolvedRefs: False`.

Same-namespace references always work without any ReferenceGrant.

## Batch deployments

For large Helm releases that create many HTTPRoutes at once, you can group them into a single atomic snapshot using the `vrata.io/batch` annotation. See [Batch Deployments]({{< relref "batch-deployments" >}}).

## Shared destinations

Multiple routes can reference the same Kubernetes Service. The controller maintains a reference count per destination. A destination is only deleted when no routes reference it anymore.

The refcount is rebuilt from the Vrata API on startup — no persistent storage needed.
