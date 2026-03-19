---
title: "External Authorization"
weight: 4
---

Delegate authorization decisions to an external service. Vrata sends a check request before each proxied request, and the external service decides allow or deny.

## Configuration

```json
{
  "name": "auth",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-service>",
    "mode": "http",
    "path": "/authorize",
    "decisionTimeout": "5s",
    "failureModeAllow": false,
    "includeBody": false,
    "onCheck": {
      "forwardHeaders": ["Authorization", "Cookie"],
      "injectHeaders": [{"key": "X-Original-URI", "value": "${request.path}"}]
    },
    "onAllow": {
      "copyToUpstream": ["x-auth-request-*"]
    },
    "onDeny": {
      "copyToClient": ["www-authenticate", "location"]
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `destinationId` | string | required | Destination hosting the auth service |
| `mode` | string | `http` | `http` or `grpc` |
| `path` | string | `/` | HTTP path for the check request (HTTP mode only) |
| `decisionTimeout` | string | `5s` | Max time to wait for auth decision |
| `failureModeAllow` | bool | `false` | Allow requests when auth service is unreachable |
| `includeBody` | bool | `false` | Send the request body to the auth service |
| `onCheck.forwardHeaders` | string[] | — | Client headers forwarded to auth service |
| `onCheck.injectHeaders` | array | — | Extra headers added to the auth check request |
| `onAllow.copyToUpstream` | string[] | — | Auth response headers copied to the upstream request |
| `onDeny.copyToClient` | string[] | — | Auth response headers copied to the client response |

## Examples

### Basic HTTP auth check

```json
{
  "name": "auth",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-service>",
    "mode": "http",
    "path": "/authorize",
    "onCheck": {
      "forwardHeaders": ["Authorization"]
    }
  }
}
```

Vrata sends `POST <auth-service>/authorize` with the client's `Authorization` header. The auth service returns 200 (allow) or non-200 (deny).

### gRPC auth check

```json
{
  "name": "grpc-auth",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-grpc-service>",
    "mode": "grpc",
    "decisionTimeout": "3s"
  }
}
```

Vrata sends a gRPC `CheckRequest` to the auth service. The service returns `OK` (allow) or a non-OK status (deny).

### Pass auth context to upstream

```json
{
  "name": "auth-with-context",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-service>",
    "path": "/authorize",
    "onCheck": {
      "forwardHeaders": ["Authorization", "Cookie"]
    },
    "onAllow": {
      "copyToUpstream": ["x-auth-user-id", "x-auth-roles", "x-auth-org"]
    }
  }
}
```

When the auth service allows the request, it can set response headers like `x-auth-user-id: 42`. Vrata copies these to the upstream request, so your backend receives the authenticated user identity without re-validating the token.

### Redirect to login on deny

```json
{
  "name": "auth-redirect",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-service>",
    "path": "/authorize",
    "onDeny": {
      "copyToClient": ["location", "set-cookie"]
    }
  }
}
```

When the auth service denies with a 302, its `Location` and `Set-Cookie` headers are forwarded to the client, triggering a redirect to the login page.

### Header interpolation

```json
{
  "onCheck": {
    "forwardHeaders": ["Authorization", "Cookie"],
    "injectHeaders": [
      {"key": "X-Original-URI", "value": "${request.path}"},
      {"key": "X-Original-Method", "value": "${request.method}"},
      {"key": "X-Original-Host", "value": "${request.host}"},
      {"key": "X-Forwarded-Proto", "value": "${request.scheme}"},
      {"key": "X-Client-IP", "value": "${request.header.X-Forwarded-For}"}
    ]
  }
}
```

Supported interpolation variables:
- `${request.host}`, `${request.path}`, `${request.method}`
- `${request.scheme}`, `${request.authority}`
- `${request.header.<NAME>}` — any request header

### Fail open (non-critical auth)

```json
{
  "name": "soft-auth",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-service>",
    "path": "/check",
    "failureModeAllow": true,
    "decisionTimeout": "2s"
  }
}
```

If the auth service is down or times out, the request is allowed through. Use for non-critical checks like feature flags or optional enrichment.

### Include request body

```json
{
  "name": "body-auth",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "<auth-service>",
    "path": "/authorize",
    "includeBody": true
  }
}
```

The entire request body is forwarded to the auth service. Use when authorization depends on the request payload (e.g. checking resource ownership in a POST body).

## Failure modes

| `failureModeAllow` | Auth service unreachable | Result |
|----|---|---|
| `false` (default) | timeout / connection error | Request rejected (fail closed) |
| `true` | timeout / connection error | Request allowed through (fail open) |

## Auth service protocol (HTTP mode)

Vrata sends:
```
POST <destination><path>
Content-Type: application/json
[forwarded headers]
[injected headers]
```

Expected responses:
- **2xx** → allow (headers in `onAllow.copyToUpstream` are copied to upstream)
- **Non-2xx** → deny (status code and headers in `onDeny.copyToClient` are returned to client)
