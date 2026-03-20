---
title: "Header Manipulation"
weight: 6
---

Add or remove request and response headers. Supports variable interpolation for dynamic values.

## Configuration

```json
{
  "name": "custom-headers",
  "type": "headers",
  "headers": {
    "requestHeadersToAdd": [
      {"key": "X-Source", "value": "vrata", "append": true},
      {"key": "X-Request-Start", "value": "${request.header.X-Start}"}
    ],
    "requestHeadersToRemove": ["X-Internal"],
    "responseHeadersToAdd": [
      {"key": "X-Powered-By", "value": "Vrata"}
    ],
    "responseHeadersToRemove": ["Server"]
  }
}
```

## All fields

| Field | Type | Description |
|-------|------|-------------|
| `requestHeadersToAdd` | array | Headers added to the request before forwarding |
| `requestHeadersToAdd[].key` | string | Header name |
| `requestHeadersToAdd[].value` | string | Header value (supports interpolation) |
| `requestHeadersToAdd[].append` | bool | `true` = append, `false` = replace existing (default: `true`) |
| `requestHeadersToRemove` | string[] | Header names removed from the request |
| `responseHeadersToAdd` | array | Headers added to the response before sending to client |
| `responseHeadersToAdd[].key` | string | Header name |
| `responseHeadersToAdd[].value` | string | Header value (supports interpolation) |
| `responseHeadersToAdd[].append` | bool | `true` = append, `false` = replace existing (default: `true`) |
| `responseHeadersToRemove` | string[] | Header names removed from the response |

## Examples

### Add a static request header

```json
{
  "name": "add-source",
  "type": "headers",
  "headers": {
    "requestHeadersToAdd": [
      {"key": "X-Source", "value": "vrata-proxy"}
    ]
  }
}
```

Every request to the upstream gets `X-Source: vrata-proxy`.

### Remove the Server header from responses

```json
{
  "name": "hide-server",
  "type": "headers",
  "headers": {
    "responseHeadersToRemove": ["Server", "X-Powered-By"]
  }
}
```

Removes backend identity headers. Common security hardening.

### Add security response headers

```json
{
  "name": "security-headers",
  "type": "headers",
  "headers": {
    "responseHeadersToAdd": [
      {"key": "X-Frame-Options", "value": "DENY", "append": false},
      {"key": "X-Content-Type-Options", "value": "nosniff", "append": false},
      {"key": "Strict-Transport-Security", "value": "max-age=63072000; includeSubDomains", "append": false},
      {"key": "X-XSS-Protection", "value": "1; mode=block", "append": false},
      {"key": "Referrer-Policy", "value": "strict-origin-when-cross-origin", "append": false}
    ]
  }
}
```

### Request header interpolation

```json
{
  "headers": {
    "requestHeadersToAdd": [
      {"key": "X-Original-Host", "value": "${request.host}"},
      {"key": "X-Original-Path", "value": "${request.path}"},
      {"key": "X-Original-Method", "value": "${request.method}"},
      {"key": "X-Forwarded-Proto", "value": "${request.scheme}"},
      {"key": "X-Real-IP", "value": "${request.header.X-Forwarded-For}"}
    ]
  }
}
```

### Available interpolation variables

| Variable | Description | Example |
|----------|-------------|---------|
| `${request.host}` | Hostname without port | `api.example.com` |
| `${request.path}` | Request path | `/api/v1/users` |
| `${request.method}` | HTTP method | `GET` |
| `${request.scheme}` | Protocol scheme | `https` |
| `${request.authority}` | Full Host header | `api.example.com:8443` |
| `${request.header.<NAME>}` | Any request header | `${request.header.X-Tenant}` |

### Replace vs append

```json
{
  "headers": {
    "requestHeadersToAdd": [
      {"key": "X-Version", "value": "2", "append": false}
    ]
  }
}
```

- `append: true` (default) — adds the header alongside any existing values. The upstream sees both.
- `append: false` — replaces any existing value. The upstream sees only the new value.

### Remove sensitive headers before forwarding

```json
{
  "name": "strip-internal",
  "type": "headers",
  "headers": {
    "requestHeadersToRemove": ["X-Internal-Auth", "X-Debug-Token", "Cookie"]
  }
}
```

Strip internal or sensitive headers before the request reaches the upstream. Useful when the proxy adds auth context and you don't want the client's original values.

### Combine everything

```json
{
  "name": "full-headers",
  "type": "headers",
  "headers": {
    "requestHeadersToAdd": [
      {"key": "X-Proxy", "value": "vrata"},
      {"key": "X-Original-Host", "value": "${request.host}"}
    ],
    "requestHeadersToRemove": ["X-Debug"],
    "responseHeadersToAdd": [
      {"key": "X-Frame-Options", "value": "DENY", "append": false}
    ],
    "responseHeadersToRemove": ["Server", "X-Powered-By"]
  }
}
```
