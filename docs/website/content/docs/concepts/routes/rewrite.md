---
title: "URL Rewrite"
weight: 3
---

Transform the request URL before forwarding to the upstream. Rewrites change what the backend sees without affecting what the client sees.

## Configuration

Rewrite lives inside `forward`:

```json
{
  "forward": {
    "destinations": [{"destinationId": "<id>", "weight": 100}],
    "rewrite": {
      "path": "/internal",
      "host": "backend.internal"
    }
  }
}
```

## All fields

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Replace the matched path with this value |
| `pathRegex` | object | Regex-based path replacement |
| `pathRegex.pattern` | string | RE2 regex to match against the path |
| `pathRegex.substitution` | string | Replacement string (supports capture groups `\1`, `\2`, etc.) |
| `host` | string | Override the Host header sent to upstream |
| `hostFromHeader` | string | Set the Host header from a request header value |
| `autoHost` | bool | Use the destination's hostname as the Host header |

Only one path rewrite method can be used at a time (`path` or `pathRegex`). Only one host rewrite method can be used at a time (`host`, `hostFromHeader`, or `autoHost`).

## Examples

### Prefix replacement

Route matches `/api/v1`, rewrite to `/internal`:

```json
{
  "name": "api-route",
  "match": {"pathPrefix": "/api/v1"},
  "forward": {
    "destinations": [{"destinationId": "<id>", "weight": 100}],
    "rewrite": {"path": "/internal"}
  }
}
```

| Client request | Backend sees |
|---|---|
| `GET /api/v1/users` | `GET /internal/users` |
| `GET /api/v1/orders/123` | `GET /internal/orders/123` |
| `GET /api/v1` | `GET /internal` |

The matched prefix is replaced. The remainder of the path is appended.

### Strip prefix

```json
{
  "forward": {
    "rewrite": {"path": "/"}
  }
}
```

| Client request | Backend sees |
|---|---|
| `GET /api/v1/users` | `GET /users` |
| `GET /api/v1` | `GET /` |

### Regex rewrite with capture groups

Restructure URLs using regex groups:

```json
{
  "forward": {
    "rewrite": {
      "pathRegex": {
        "pattern": "/users/([0-9]+)/orders/([0-9]+)",
        "substitution": "/v2/orders/\\2?user=\\1"
      }
    }
  }
}
```

| Client request | Backend sees |
|---|---|
| `GET /users/42/orders/99` | `GET /v2/orders/99?user=42` |

### Regex version extraction

```json
{
  "forward": {
    "rewrite": {
      "pathRegex": {
        "pattern": "/api/(v[0-9]+)/(.*)",
        "substitution": "/\\2"
      }
    }
  }
}
```

| Client request | Backend sees |
|---|---|
| `GET /api/v2/users` | `GET /users` |
| `GET /api/v1/health` | `GET /health` |

Strips the version prefix. The version could be used for routing to different destinations.

### Host rewrite (fixed)

```json
{
  "forward": {
    "rewrite": {"host": "internal-api.svc.cluster.local"}
  }
}
```

The upstream sees `Host: internal-api.svc.cluster.local` regardless of what the client sent. Use for backends that route based on the Host header.

### Host from a request header

```json
{
  "forward": {
    "rewrite": {"hostFromHeader": "X-Original-Host"}
  }
}
```

If the client sends `X-Original-Host: app.example.com`, the upstream sees `Host: app.example.com`. Use behind CDNs that overwrite the Host header.

### Auto-host (destination hostname)

```json
{
  "forward": {
    "rewrite": {"autoHost": true}
  }
}
```

The Host header is set to the destination's `host` field. Use when proxying to external SaaS APIs that validate the Host header.

### Combined path + host rewrite

```json
{
  "forward": {
    "rewrite": {
      "path": "/",
      "autoHost": true
    }
  }
}
```

Strip the prefix AND set the Host to the destination hostname. Common when using Vrata as a gateway to external APIs.
