---
title: "Direct Response"
weight: 5
---

Return a fixed HTTP response without contacting any upstream. Vrata answers immediately from the proxy itself — no backend is involved.

## Configuration

```json
{
  "name": "maintenance-page",
  "match": {"pathPrefix": "/"},
  "directResponse": {
    "status": 503,
    "body": "{\"error\": \"service unavailable\", \"message\": \"Down for maintenance\"}"
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `status` | number | required | HTTP status code returned to the client |
| `body` | string | `""` | Response body (any string — JSON, HTML, plain text) |

`directResponse` is mutually exclusive with `forward` and `redirect`. Setting more than one is a validation error.

## Examples

### Health check endpoint

```json
{
  "name": "healthz",
  "match": {"path": "/healthz"},
  "directResponse": {
    "status": 200,
    "body": "{\"status\": \"ok\"}"
  }
}
```

Answers health probes directly at the proxy without hitting any backend. No destination needed.

### Maintenance page (JSON)

```json
{
  "name": "maintenance",
  "match": {"pathPrefix": "/"},
  "directResponse": {
    "status": 503,
    "body": "{\"error\": \"maintenance\", \"message\": \"Back soon\"}"
  }
}
```

Block all traffic during a deployment window. Place this route in a group with low priority or activate it via a snapshot swap when maintenance starts.

### Block a specific path

```json
{
  "name": "block-admin",
  "match": {"pathPrefix": "/admin"},
  "directResponse": {
    "status": 403,
    "body": "{\"error\": \"forbidden\"}"
  }
}
```

Returns 403 before any middleware or backend is consulted. Useful to hard-block endpoints that should never be publicly accessible.

### Static 404 for unknown routes

```json
{
  "name": "catch-all",
  "match": {"pathPrefix": "/"},
  "directResponse": {
    "status": 404,
    "body": "{\"error\": \"not_found\", \"message\": \"The requested resource does not exist\"}"
  }
}
```

When placed last in the route group (lowest specificity), this catches any request that didn't match a more specific route.

### Deprecation notice

```json
{
  "name": "deprecated-v1",
  "match": {"pathPrefix": "/api/v1"},
  "directResponse": {
    "status": 410,
    "body": "{\"error\": \"gone\", \"message\": \"API v1 is deprecated. Use /api/v2\"}"
  }
}
```

Returns 410 Gone to signal permanent removal. Clients that have cached the old endpoint get a clear actionable message.

## When to use direct response

| Situation | Use |
|-----------|-----|
| Health / readiness probes | `status: 200`, no backend required |
| Maintenance windows | `status: 503`, swap in via snapshot |
| Hard-blocking sensitive paths | `status: 403` |
| API deprecation | `status: 410` with migration message |
| Catch-all 404 | `status: 404`, lowest-priority route |
| CORS preflight shortcut | `status: 204`, avoid hitting backend for OPTIONS |
