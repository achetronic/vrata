---
title: "Path Composition"
weight: 1
---

When a route belongs to a group, Vrata composes the group's path with the route's path to create the final match. The composition rules depend on the path types involved.

## Composition table

| Group path | Route path | Final match | Type |
|------------|-----------|-------------|------|
| `pathPrefix: /api/v1` | `pathPrefix: /products` | `pathPrefix: /api/v1/products` | Prefix + prefix |
| `pathPrefix: /api/v1` | `path: /health` | `path: /api/v1/health` | Prefix + exact |
| `pathPrefix: /api/v1` | `pathRegex: /[0-9]+` | `pathRegex: ^/api/v1/[0-9]+` | Prefix + regex |
| `pathPrefix: /api` | (no path) | `pathPrefix: /api` | Prefix only |
| `pathRegex: /(en\|es)` | `pathPrefix: /products` | `pathRegex: (?:/(en\|es))(?:/products)` | Regex + literal |
| `pathRegex: /(en\|es)` | `pathRegex: /[0-9]+` | `pathRegex: (?:/(en\|es))(?:/[0-9]+)` | Regex + regex |
| `pathRegex: /(en\|es)` | `path: /health` | `pathRegex: (?:/(en\|es))(?:/health)` | Regex + literal |
| `pathRegex: /(en\|es)` | (no path) | `pathRegex: /(en\|es)` | Regex only |

When a group uses `pathRegex` and a route uses a literal path (`path` or `pathPrefix`), the literal is escaped with `regexp.QuoteMeta` before composition. This prevents accidentally breaking the regex.

## Examples

### API versioning

Group:
```json
{"name": "api-v1", "pathPrefix": "/api/v1", "routeIds": ["products", "orders"]}
```

Route `products`:
```json
{"name": "products", "match": {"pathPrefix": "/products"}}
```

Route `orders`:
```json
{"name": "orders", "match": {"pathPrefix": "/orders"}}
```

Final routing table:
- `/api/v1/products` → products route
- `/api/v1/orders` → orders route

### Multi-language prefix

Group:
```json
{"name": "i18n", "pathRegex": "/(en|es|fr|de)", "routeIds": ["store", "checkout"]}
```

Route `store`:
```json
{"name": "store", "match": {"pathPrefix": "/store"}}
```

Final routing table:
- `/(en|es|fr|de)/store` → store route (matches `/en/store`, `/es/store`, etc.)

### Hostname + path grouping

Group:
```json
{
  "name": "public-api",
  "pathPrefix": "/api",
  "hostnames": ["api.example.com", "api.staging.example.com"],
  "routeIds": ["users", "health"]
}
```

Route `users`:
```json
{"name": "users", "match": {"pathPrefix": "/users", "methods": ["GET", "POST"]}}
```

Route `health`:
```json
{"name": "health", "match": {"path": "/health"}}
```

Final routing table:
- `GET/POST api.example.com/api/users` → users route
- `GET/POST api.staging.example.com/api/users` → users route
- `api.example.com/api/health` (exact) → health route
- `api.staging.example.com/api/health` (exact) → health route

### Header matching from group

```json
{
  "name": "v2-tenants",
  "pathPrefix": "/api",
  "headers": [{"name": "X-API-Version", "value": "2"}],
  "routeIds": ["users"]
}
```

Route `users` only matches when the group's `X-API-Version: 2` header is also present.

## Hostnames

Hostnames from the group and the route are merged (union). There's no limit on the number of hostnames.

```json
{"name": "group", "hostnames": ["a.example.com"], "routeIds": ["route"]}
```

Route:
```json
{"name": "route", "match": {"hostnames": ["b.example.com"]}}
```

Final match: hostnames `["a.example.com", "b.example.com"]`.

## Headers

Header matchers from the group and route are also merged. All headers must match.
