---
title: "Overlap Detection"
weight: 6
---

The controller detects when two HTTPRoutes define paths that overlap on the same hostname.

## What counts as an overlap

| Route A | Route B | Overlap? | Why |
|---------|---------|----------|-----|
| `PathPrefix /api` | `PathPrefix /api` | ✅ | Identical |
| `PathPrefix /api` | `PathPrefix /api/users` | ✅ | `/api` covers `/api/users` |
| `PathPrefix /api` | `Exact /api/users` | ✅ | Prefix covers exact |
| `PathPrefix /api` | `PathPrefix /apikeys` | ❌ | No segment boundary |
| `PathPrefix /api/users` | `PathPrefix /api/admin` | ❌ | Siblings |
| `Exact /health` | `Exact /ready` | ❌ | Different paths |
| `PathPrefix /` | anything | ✅ | Root prefix covers everything |

Overlaps are only detected on the **same hostname**. Different hostnames never conflict.

Regex paths are skipped (detection is not yet implemented for regex).

## Modes

Configure in the controller config:

```yaml
duplicates:
  mode: "warn"   # off | warn | reject
```

### `off`

No detection. All routes are synced.

### `warn`

Overlaps are logged with full detail but the route is synced anyway:

```
WARN overlapping route detected
  incoming="PathPrefix /api on api.example.com (from prod/route-b)"
  existing="PathPrefix /api on api.example.com (from default/route-a)"
```

### `reject`

Overlaps are logged **and** the incoming route is skipped. The HTTPRoute status is set to `Accepted: False` with reason `OverlappingRoute` and a message listing all conflicts:

```
PathPrefix /api on api.example.com (from prod/route-b) overlaps with PathPrefix /api on api.example.com (from default/route-a)
```

The operator can see this via `kubectl get httproute -o yaml` in the status conditions.

## Same source is not a conflict

If the same HTTPRoute is re-processed (e.g. on update), it's not flagged as overlapping with itself.
