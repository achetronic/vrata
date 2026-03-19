---
title: "Overlap Detection"
weight: 6
---

The controller detects when two HTTPRoutes define matchers that target the same traffic on the same hostname.

## What the detector checks

Overlap detection considers four dimensions for each match rule:

1. **Hostname** — routes on different hostnames never conflict
2. **Path** — exact match, prefix coverage, and segment-boundary awareness
3. **Header matchers** — routes with different header matchers target different traffic and don't conflict, even if their hostname and path are identical
4. **HTTP method** — routes restricted to different methods (e.g. GET vs POST) target different traffic and don't conflict

This means you can safely have two routes with the same path on the same hostname as long as they differ in header matchers or HTTP method — a common pattern for routing by environment, tenant, API version, or separating read/write traffic.

## Path overlap rules

| Route A | Route B | Overlap? | Why |
|---------|---------|----------|-----|
| `PathPrefix /api` | `PathPrefix /api` | ✅ | Identical |
| `PathPrefix /api` | `PathPrefix /api/users` | ✅ | `/api` covers `/api/users` |
| `PathPrefix /api` | `Exact /api/users` | ✅ | Prefix covers exact |
| `PathPrefix /api` | `PathPrefix /apikeys` | ❌ | No segment boundary |
| `PathPrefix /api/users` | `PathPrefix /api/admin` | ❌ | Siblings |
| `Exact /health` | `Exact /ready` | ❌ | Different paths |
| `PathPrefix /` | anything | ✅ | Root prefix covers everything |

Regex paths are skipped — overlap detection is not feasible for arbitrary regular expressions.

## Method disambiguation

Two routes with overlapping paths are **not** considered overlapping if they restrict to different HTTP methods. An empty method means "all methods" and overlaps with everything.

| Route A method | Route B method | Same traffic? |
|----------------|----------------|---------------|
| (none) | (none) | ✅ Yes — both accept all methods |
| GET | POST | ❌ No — different methods |
| GET | GET | ✅ Yes — same method |
| GET | (none) | ✅ Yes — "all" includes GET |
| DELETE | PUT | ❌ No — different methods |

### Example: read/write split

```yaml
# read-route.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api-read
spec:
  hostnames: ["api.example.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
          method: GET
      backendRefs:
        - name: read-replicas
          port: 80
---
# write-route.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api-write
spec:
  hostnames: ["api.example.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
          method: POST
      backendRefs:
        - name: write-primary
          port: 80
```

These two routes will **not** trigger an overlap warning — GET and POST are different traffic.

## Header disambiguation

Two routes with overlapping paths are **not** considered overlapping if their header matchers differ. Headers are compared as a set — order doesn't matter, and header names are case-insensitive per HTTP semantics.

| Route A headers | Route B headers | Same traffic? |
|-----------------|-----------------|---------------|
| (none) | (none) | ✅ Yes — both match all requests |
| `X-Env: sandbox` | `X-Env: production` | ❌ No — different values |
| `X-Env: prod` | (none) | ❌ No — one is restricted, the other isn't |
| `X-Env: prod, X-Region: eu` | `X-Env: prod, X-Region: us` | ❌ No — one header differs |
| `X-Env: prod, X-Region: eu` | `X-Region: eu, X-Env: prod` | ✅ Yes — same set, different order |
| `X-Env: prod` | `x-env: prod` | ✅ Yes — header names are case-insensitive |

### Example: environment routing

Two HTTPRoutes on the same hostname and path, differentiated by a header:

```yaml
# sandbox-route.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api-sandbox
spec:
  hostnames: ["api.example.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
          headers:
            - name: X-Env
              value: sandbox
      backendRefs:
        - name: sandbox-svc
          port: 80
---
# production-route.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api-production
spec:
  hostnames: ["api.example.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
          headers:
            - name: X-Env
              value: production
      backendRefs:
        - name: production-svc
          port: 80
```

These two routes will **not** trigger an overlap warning — the `X-Env` header disambiguates them completely.

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
  incoming="api.example.com PathPrefix /api (from prod/route-b)"
  existing="api.example.com PathPrefix /api (from default/route-a)"
```

When methods or header matchers are involved, they're included in the log:

```
WARN overlapping route detected
  incoming="api.example.com PathPrefix /api (from prod/route-b) [method: GET; headers: X-Env=prod]"
  existing="api.example.com PathPrefix /api (from default/route-a) [method: GET; headers: X-Env=prod]"
```

### `reject`

Overlaps are logged **and** the incoming route is skipped. The HTTPRoute status is set to `Accepted: False` with reason `OverlappingRoute` and a message listing all conflicts.

The operator can see this via `kubectl get httproute -o yaml` in the status conditions.

## Same source is not a conflict

If the same HTTPRoute is re-processed (e.g. on update), it's not flagged as overlapping with itself.
