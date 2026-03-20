---
title: "Middleware Inheritance"
weight: 2
---

Groups can attach middlewares that apply to all routes in the group. Routes can override or disable individual middlewares.

## How it works

1. Group lists middlewares in `middlewareIds` — they apply to every route in the group.
2. Routes can also list middlewares in their own `middlewareIds` — these are added (union).
3. If both group and route have an override for the same middleware, **the route wins entirely** (no merge).

## Examples

### Group applies JWT and CORS to all routes

```json
{
  "name": "store-api",
  "routeIds": ["<products>", "<orders>", "<health>"],
  "middlewareIds": ["jwt-auth", "cors"],
  "pathPrefix": "/api/v1"
}
```

All three routes get JWT and CORS applied. No configuration needed on individual routes.

### Group override: skip JWT on health endpoint

```json
{
  "name": "store-api",
  "routeIds": ["<products>", "<orders>", "<health>"],
  "middlewareIds": ["jwt-auth", "cors"],
  "middlewareOverrides": {
    "jwt-auth": {
      "skipWhen": ["request.path.endsWith('/health')"]
    }
  }
}
```

JWT is skipped when the path ends with `/health`. All other routes still get JWT.

### Route disables JWT entirely

Group:
```json
{
  "name": "store-api",
  "middlewareIds": ["jwt-auth", "cors"],
  "routeIds": ["<products>", "<public-catalog>"]
}
```

Route `public-catalog`:
```json
{
  "name": "public-catalog",
  "middlewareOverrides": {
    "jwt-auth": {"disabled": true}
  }
}
```

The `public-catalog` route doesn't run JWT. The `products` route still does.

### Route adds its own middleware

Group:
```json
{
  "name": "store-api",
  "middlewareIds": ["cors"],
  "routeIds": ["<products>", "<admin>"]
}
```

Route `admin`:
```json
{
  "name": "admin",
  "middlewareIds": ["jwt-auth", "rate-limit"]
}
```

The `admin` route gets: CORS (from group) + JWT + rate-limit (from route). The `products` route only gets CORS.

### Route overrides group override entirely

Group:
```json
{
  "name": "api",
  "middlewareIds": ["jwt-auth"],
  "middlewareOverrides": {
    "jwt-auth": {"skipWhen": ["request.path == '/health'"]}
  }
}
```

Route:
```json
{
  "name": "special",
  "middlewareOverrides": {
    "jwt-auth": {"onlyWhen": ["request.headers['x-internal'] == 'true'"]}
  }
}
```

The route's override **replaces** the group's override entirely. The `skipWhen` from the group is gone — only `onlyWhen` from the route applies.

## Override fields

| Field | Type | Description |
|-------|------|-------------|
| `disabled` | bool | Completely disable the middleware for this route |
| `skipWhen` | string[] | CEL expressions — skip if **any** matches |
| `onlyWhen` | string[] | CEL expressions — run only if **at least one** matches |
| `headers` | object | Override header manipulation config |
| `extProc` | object | Override external processor settings (phases, allowOnError) |

`skipWhen` and `onlyWhen` are mutually exclusive on the same override.

## Execution order

Middlewares execute in the order listed in `middlewareIds`. Group middlewares run first, then route middlewares:

```
Group middlewareIds: [A, B]    → A runs first, then B
Route middlewareIds: [C, D]    → then C, then D
```

If any middleware rejects the request, subsequent middlewares and the forward action are skipped.
