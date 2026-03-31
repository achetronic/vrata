---
title: "Matching"
weight: 1
---

A route matches requests based on path, headers, methods, query parameters, hostnames, gRPC content-type, or CEL expressions. All specified matchers must pass (AND logic) for the route to match.

## All match fields

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Exact path match |
| `pathPrefix` | string | Path prefix match |
| `pathRegex` | string | RE2 regex match |
| `methods` | string[] | HTTP methods (empty = all) |
| `hostnames` | string[] | Virtual host names (any must match) |
| `headers` | object[] | Header matchers (all must match) |
| `queryParams` | object[] | Query parameter matchers (all must match) |
| `grpc` | bool | Restrict to gRPC content-type |
| `cel` | string | CEL expression for complex logic |

Only one of `path`, `pathPrefix`, or `pathRegex` should be set.

## Path matching

### Exact path

```json
{"match": {"path": "/health"}}
```

Matches only `GET /health`. Does not match `/health/` or `/health/deep`.

### Path prefix

```json
{"match": {"pathPrefix": "/api/v1"}}
```

Matches `/api/v1`, `/api/v1/users`, `/api/v1/orders/123`, etc.

### Path regex

```json
{"match": {"pathRegex": "/users/[0-9]+"}}
```

Matches `/users/42`, `/users/999`. Does not match `/users/abc`. Regexes use RE2 syntax and are compiled once at routing table build time — zero per-request cost.

### Complex regex

```json
{"match": {"pathRegex": "^/api/(v[12])/users/[a-f0-9-]{36}$"}}
```

Matches `/api/v1/users/550e8400-e29b-41d4-a716-446655440000` and `/api/v2/users/...`.

## Method matching

```json
{"match": {"methods": ["GET", "POST"]}}
```

Only matches GET and POST requests. Empty array or omitted = matches all methods.

```json
{"match": {"methods": ["DELETE"], "pathPrefix": "/api/admin"}}
```

Only matches DELETE requests to `/api/admin/*`.

## Header matching

### Exact header value

```json
{"match": {"headers": [{"name": "X-Tenant", "value": "acme"}]}}
```

Matches requests with `X-Tenant: acme`.

### Regex header value

```json
{"match": {"headers": [{"name": "X-Version", "value": "v[0-9]+", "regex": true}]}}
```

Matches `X-Version: v1`, `X-Version: v2`, etc.

### Multiple headers (AND)

```json
{
  "match": {
    "headers": [
      {"name": "X-Tenant", "value": "acme"},
      {"name": "X-Env", "value": "production"}
    ]
  }
}
```

Both headers must be present and match.

### Header presence (any value)

```json
{"match": {"headers": [{"name": "Authorization"}]}}
```

Matches any request with an `Authorization` header, regardless of value.

## Query parameter matching

### Exact value

```json
{"match": {"queryParams": [{"name": "format", "value": "json"}]}}
```

Matches `?format=json`.

### Regex value

```json
{"match": {"queryParams": [{"name": "version", "value": "[0-9]+", "regex": true}]}}
```

Matches `?version=1`, `?version=42`, etc.

### Multiple params (AND)

```json
{
  "match": {
    "queryParams": [
      {"name": "format", "value": "json"},
      {"name": "page", "value": "[0-9]+", "regex": true}
    ]
  }
}
```

Both query params must be present and match.

## Hostname matching

```json
{"match": {"hostnames": ["api.example.com", "api.staging.example.com"]}}
```

The request's `Host` header must match **any one** of the listed hostnames (OR logic). No wildcard support — use separate routes or CEL for complex hostname matching.

## gRPC

```json
{"match": {"grpc": true}}
```

Restricts the match to requests with `Content-Type: application/grpc`. Combine with other matchers:

```json
{
  "match": {
    "grpc": true,
    "pathPrefix": "/mypackage.MyService"
  }
}
```

## CEL expressions

For logic that static matchers can't express:

```json
{"match": {"cel": "request.path.startsWith('/api') && 'admin' in request.headers['x-role'] && request.method != 'DELETE'"}}
```

CEL is evaluated **after** all static matchers pass (it's the most expensive check).

### Available variables

| Variable | Type | Description |
|----------|------|-------------|
| `request.method` | string | HTTP method (`"GET"`, `"POST"`, etc.) |
| `request.path` | string | URL path |
| `request.host` | string | Hostname without port |
| `request.scheme` | string | `"http"` or `"https"` |
| `request.headers` | map | Request headers (lowercase keys) |
| `request.queryParams` | map | Query parameters |
| `request.clientIp` | string | Client IP address |
| `request.body.raw` | string | Raw request body (up to `celBodyMaxSize`, default 64KB) |
| `request.body.json` | map | Parsed JSON body (only when Content-Type is `application/json`) |
| `request.tls.peerCertificate.uris` | list | URI SANs from client cert (SPIFFE IDs live here) |
| `request.tls.peerCertificate.dnsNames` | list | DNS SANs from client cert |
| `request.tls.peerCertificate.subject` | string | Certificate subject DN |
| `request.tls.peerCertificate.serial` | string | Certificate serial number (hex) |

`request.body` is only present when a CEL expression in the matched route references it — zero overhead for routes that don't inspect the body. `request.tls` fields are only present when the client presented a certificate via [mTLS]({{< relref "../listeners/mtls" >}}). Always guard access with `has()`.

### CEL examples

Complex role check:

```json
{"match": {"cel": "request.headers['x-role'] in ['admin', 'superadmin'] && request.method == 'DELETE'"}}
```

IP-based routing:

```json
{"match": {"cel": "request.clientIp.startsWith('10.0.')"}}
```

Route by JSON body content (e.g. MCP tool calls):

```json
{"match": {"cel": "has(request.body) && has(request.body.json) && request.body.json.method == 'tools/call'"}}
```

Route by raw body content:

```json
{"match": {"cel": "has(request.body) && request.body.raw.contains('CRITICAL')"}}
```

Match requests from a specific SPIFFE identity:

```json
{"match": {"cel": "has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == 'spiffe://cluster.local/ns/default/sa/agent')"}}
```

## Combining matchers

All specified matchers are AND-ed together:

```json
{
  "match": {
    "pathPrefix": "/api/v1",
    "methods": ["GET", "POST"],
    "hostnames": ["api.example.com"],
    "headers": [{"name": "X-Tenant", "value": "acme"}],
    "queryParams": [{"name": "format", "value": "json"}]
  }
}
```

This matches: GET or POST requests to `/api/v1/*` on `api.example.com` with header `X-Tenant: acme` and query param `format=json`.

## Route priority

Routes are evaluated in this order:

1. Exact path (`path`) — highest priority
2. Longest prefix first (`pathPrefix`) — `/api/v1/users` wins over `/api/v1`
3. Regex (`pathRegex`)
4. Within the same path type, more specific matchers (more headers, hostnames, etc.) win
