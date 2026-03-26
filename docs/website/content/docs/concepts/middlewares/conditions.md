---
title: "Conditions (skipWhen / onlyWhen)"
weight: 8
---

Control when a middleware runs using CEL expressions. This avoids duplicating routes just to skip a middleware on certain paths, methods, or headers.

## Three controls

| Control | Meaning | Use case |
|---------|---------|----------|
| `skipWhen` | Skip if **any** expression matches | Skip JWT on `/health` |
| `onlyWhen` | Run only if **at least one** matches | Rate limit only on `/api` |
| `disabled` | Completely disable | Turn off middleware for one route |

`skipWhen` and `onlyWhen` are mutually exclusive on the same override.

## Where to use them

Conditions are set in `middlewareOverrides` on a route or group:

```json
{
  "middlewareOverrides": {
    "<middleware-name>": {
      "skipWhen": ["<CEL expression>"],
      "onlyWhen": ["<CEL expression>"],
      "disabled": false
    }
  }
}
```

## CEL variables

| Variable | Type | Example |
|----------|------|---------|
| `request.method` | string | `"GET"` |
| `request.path` | string | `"/api/v1/users"` |
| `request.host` | string | `"api.example.com"` |
| `request.scheme` | string | `"https"` |
| `request.headers` | map[string]string | `request.headers["authorization"]` |
| `request.queryParams` | map[string]string | `request.queryParams["token"]` |
| `request.clientIp` | string | `"10.0.0.1"` |
| `request.body.raw` | string | Raw request body (up to `celBodyMaxSize`) |
| `request.body.json` | map | Parsed JSON body (only when Content-Type is `application/json`) |
| `request.tls.peerCertificate.uris` | list(string) | URI SANs from client cert (includes SPIFFE IDs) |
| `request.tls.peerCertificate.dnsNames` | list(string) | DNS SANs from client cert |
| `request.tls.peerCertificate.subject` | string | Client cert Distinguished Name |

CEL expressions are compiled once at routing table build time — no per-request compile cost.

Body variables are lazy: the request body is only buffered when a CEL expression references `request.body`. Routes without body-referencing expressions have zero overhead. Use `has(request.body)` and `has(request.body.json)` to guard access.

TLS variables are only populated when the client presents a certificate via mTLS. Use `has(request.tls)` to guard access.

## Examples

### skipWhen — skip JWT on health/ready

```json
{
  "middlewareOverrides": {
    "jwt-auth": {
      "skipWhen": [
        "request.path == '/health'",
        "request.path == '/ready'"
      ]
    }
  }
}
```

JWT runs on all requests **except** `/health` and `/ready`. If any expression matches, the middleware is skipped.

### skipWhen — skip rate limit on GET

```json
{
  "middlewareOverrides": {
    "rate-limit": {
      "skipWhen": ["request.method == 'GET'"]
    }
  }
}
```

Rate limiting only applies to POST, PUT, DELETE, etc. GET requests are unlimited.

### onlyWhen — rate limit only on /api

```json
{
  "middlewareOverrides": {
    "rate-limit": {
      "onlyWhen": ["request.path.startsWith('/api')"]
    }
  }
}
```

Rate limiting only runs on `/api/*` paths. Static assets, health checks, etc. are not rate limited.

### onlyWhen — CORS only for browser origins

```json
{
  "middlewareOverrides": {
    "cors": {
      "onlyWhen": ["'origin' in request.headers"]
    }
  }
}
```

CORS middleware only runs when the request has an `Origin` header (browser requests). Server-to-server requests skip it.

### disabled — turn off for one route

```json
{
  "middlewareOverrides": {
    "cors": {"disabled": true}
  }
}
```

Completely disables CORS for this specific route, even if the group has it enabled.

### Complex conditions

Skip auth for internal IPs:

```json
{
  "middlewareOverrides": {
    "jwt-auth": {
      "skipWhen": ["request.clientIp.startsWith('10.0.')"]
    }
  }
}
```

Only run access logging for non-GET requests:

```json
{
  "middlewareOverrides": {
    "access-log": {
      "onlyWhen": ["request.method != 'GET'"]
    }
  }
}
```

Skip external auth for a specific tenant:

```json
{
  "middlewareOverrides": {
    "ext-auth": {
      "skipWhen": ["request.headers['x-tenant'] == 'internal'"]
    }
  }
}
```

### Multiple expressions (OR logic)

```json
{
  "skipWhen": [
    "request.path == '/health'",
    "request.path == '/ready'",
    "request.path == '/metrics'",
    "request.method == 'OPTIONS'"
  ]
}
```

Skip if the path is `/health` OR `/ready` OR `/metrics` OR the method is OPTIONS. Any match skips.

### skipWhen — skip auth for safe MCP methods (body inspection)

```json
{
  "middlewareOverrides": {
    "mcp-guard": {
      "skipWhen": [
        "has(request.body) && has(request.body.json) && request.body.json.method in ['initialize', 'notifications/initialized', 'tools/list']"
      ]
    }
  }
}
```

Skip the authorization middleware when the MCP request is a session initialization or tool listing. The body is only buffered when a condition references `request.body`.

### onlyWhen — only run auth for tools/call (body inspection)

```json
{
  "middlewareOverrides": {
    "tool-guard": {
      "onlyWhen": [
        "has(request.body) && has(request.body.json) && request.body.json.method == 'tools/call'"
      ]
    }
  }
}
```

The authorization middleware only runs when the request is a `tools/call`. All other MCP methods pass through without authorization.

## Precedence

- **Route overrides win** over group overrides entirely (no merge)
- If a route sets `middlewareOverrides.jwt-auth`, the group's override for `jwt-auth` is completely replaced
- To inherit the group override and extend it, you must copy the group conditions into the route override

## Override fields beyond conditions

The `middlewareOverrides` map also supports middleware-specific overrides:

| Field | Middleware | Description |
|-------|-----------|-------------|
| `headers` | headers | Override the header manipulation config |
| `extProc` | extProc | Override phases and error handling |
