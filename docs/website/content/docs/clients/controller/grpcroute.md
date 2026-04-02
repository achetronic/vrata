---
title: "GRPCRoute Support"
weight: 4
---

The controller translates Gateway API GRPCRoutes into Vrata entities with gRPC-specific matching.

## Mapping

| Gateway API | Vrata entity |
|-------------|-------------|
| `GRPCRoute` | `RouteGroup` (1:1, carries hostnames) |
| `GRPCRoute.spec.rules[]` | `Route` (1 per rule) |
| `GRPCRoute.spec.rules[].matches[]` | Multiple `Route`s (1 per match, `grpc: true`) |
| `GRPCRoute.spec.rules[].backendRefs[]` | `Destination` (deduplicated by Service name+namespace+port) |

## Service/method matching

GRPCRoute matches by gRPC service and method name. The controller converts these to HTTP path matching since gRPC uses `/{service}/{method}` URL paths:

| GRPCRoute match | Vrata match | Description |
|-----------------|-------------|-------------|
| Service: `pkg.Svc`, Method: `Get` | `match.path: "/pkg.Svc/Get"` | Exact method match |
| Service: `pkg.Svc` (no method) | `match.pathPrefix: "/pkg.Svc/"` | All methods on a service |
| Empty (no service/method) | `match.pathPrefix: "/"` | All gRPC traffic |
| RegularExpression: `pkg\..*` | `match.pathRegex: "/pkg\\..*/"` | Regex service match |

All gRPC routes are created with `match.grpc: true` which restricts matching to requests with `Content-Type: application/grpc*`. The method is always set to `POST` (gRPC protocol requirement).

## Supported match types

| Type | Example | Description |
|------|---------|-------------|
| Exact (default) | Service: `pkg.Svc`, Method: `Get` | Exact gRPC service/method |
| RegularExpression | Service: `pkg\..*` | Regex pattern on service name |
| Header (exact) | `x-version: v2` | Header condition |
| Header (regex) | `x-version: v[0-9]+` | Regex header condition |

## Supported filters

| Filter | Vrata mapping |
|--------|---------------|
| `RequestHeaderModifier` | Middleware type `headers` |
| `ResponseHeaderModifier` | Middleware type `headers` |

GRPCRoute does not support `RequestRedirect` or `URLRewrite` filters (per the Gateway API spec).

## Ownership

Entities created from GRPCRoutes follow the same naming convention as HTTPRoutes:

- **Group**: `k8s:{namespace}/{grpcroute-name}`
- **Routes**: `k8s:{namespace}/{grpcroute-name}/rule-{index}/match-{index}`
- **Destinations**: `k8s:{namespace}/{service-name}:{port}`

Destinations are shared across HTTPRoutes and GRPCRoutes — the same backend Service referenced by both types produces a single Destination entity.

## Configuration

GRPCRoute watching is enabled by default:

```yaml
watch:
  grpcRoutes: true   # default
```

Set to `false` to disable GRPCRoute reconciliation.

## ReferenceGrant

Cross-namespace backendRefs in GRPCRoutes are validated via ReferenceGrants, the same as HTTPRoutes. A ReferenceGrant with `from.kind: GRPCRoute` in the target namespace is required.

## Status

The controller writes the following conditions on GRPCRoute resources:

| Condition | Status | When |
|-----------|--------|------|
| `Accepted: True` | Route synced to Vrata | Reason: `Synced` |
| `Accepted: False` | Sync failed | Reason: `SyncFailed` |
| `ResolvedRefs: False` | Cross-namespace ref denied | Reason: `BackendNotFound` |
