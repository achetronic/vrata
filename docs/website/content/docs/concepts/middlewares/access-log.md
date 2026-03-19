---
title: "Access Log"
weight: 7
---

Structured access logging per route or group. Log request and response data in JSON or key=value format to stdout, stderr, or a file.

## Configuration

```json
{
  "name": "access-log",
  "type": "accessLog",
  "accessLog": {
    "path": "/dev/stdout",
    "json": true,
    "onRequest": {
      "fields": {
        "id": "${id}",
        "method": "${request.method}",
        "path": "${request.path}",
        "clientIp": "${request.clientIp}"
      }
    },
    "onResponse": {
      "fields": {
        "id": "${id}",
        "status": "${response.status}",
        "bytes": "${response.bytes}",
        "duration_ms": "${duration.ms}"
      }
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | required | Output path: `stdout`, `stderr`, `/dev/stdout`, `/dev/stderr`, or a file path |
| `json` | bool | `false` | `true` = JSON objects, `false` = key=value pairs |
| `onRequest` | object | — | Fields logged when the request arrives |
| `onRequest.fields` | map | — | Key-value pairs with interpolation variables |
| `onResponse` | object | — | Fields logged when the response completes |
| `onResponse.fields` | map | — | Key-value pairs with interpolation variables |

## All interpolation variables

| Variable | Available in | Description | Example |
|----------|-------------|-------------|---------|
| `${id}` | both | Auto-generated UUID (same for request and response) | `550e8400-e29b-41d4-a716-446655440000` |
| `${request.method}` | both | HTTP method | `GET` |
| `${request.path}` | both | Original URL path (before rewrite) | `/api/v1/users` |
| `${request.host}` | both | Hostname without port | `api.example.com` |
| `${request.authority}` | both | Full Host header | `api.example.com:8443` |
| `${request.scheme}` | both | Protocol | `https` |
| `${request.clientIp}` | both | Client IP (respects X-Forwarded-For) | `10.0.0.1` |
| `${request.header.<NAME>}` | both | Any request header | `${request.header.Authorization}` |
| `${response.status}` | onResponse | HTTP status code | `200` |
| `${response.bytes}` | onResponse | Bytes written to client | `1234` |
| `${response.header.<NAME>}` | onResponse | Any response header | `${response.header.Content-Type}` |
| `${duration.ms}` | onResponse | Duration in milliseconds | `42` |
| `${duration.us}` | onResponse | Duration in microseconds | `42000` |
| `${duration.s}` | onResponse | Duration in seconds (float) | `0.042` |

## Examples

### JSON access log to stdout

```json
{
  "name": "json-log",
  "type": "accessLog",
  "accessLog": {
    "path": "stdout",
    "json": true,
    "onResponse": {
      "fields": {
        "id": "${id}",
        "method": "${request.method}",
        "path": "${request.path}",
        "host": "${request.host}",
        "status": "${response.status}",
        "bytes": "${response.bytes}",
        "duration_ms": "${duration.ms}",
        "client_ip": "${request.clientIp}"
      }
    }
  }
}
```

Output:
```json
{"id":"abc123","method":"GET","path":"/api/v1/users","host":"api.example.com","status":"200","bytes":"1234","duration_ms":"42","client_ip":"10.0.0.1"}
```

### Request + response logging (correlation)

```json
{
  "name": "full-log",
  "type": "accessLog",
  "accessLog": {
    "path": "stdout",
    "json": true,
    "onRequest": {
      "fields": {
        "event": "request",
        "id": "${id}",
        "method": "${request.method}",
        "path": "${request.path}",
        "client_ip": "${request.clientIp}",
        "user_agent": "${request.header.User-Agent}"
      }
    },
    "onResponse": {
      "fields": {
        "event": "response",
        "id": "${id}",
        "status": "${response.status}",
        "bytes": "${response.bytes}",
        "duration_ms": "${duration.ms}",
        "content_type": "${response.header.Content-Type}"
      }
    }
  }
}
```

The `${id}` is the same UUID for both the request and response log entries, so you can correlate them.

### Key=value format (logfmt)

```json
{
  "name": "logfmt-log",
  "type": "accessLog",
  "accessLog": {
    "path": "stdout",
    "json": false,
    "onResponse": {
      "fields": {
        "method": "${request.method}",
        "path": "${request.path}",
        "status": "${response.status}",
        "duration_ms": "${duration.ms}"
      }
    }
  }
}
```

Output:
```
method=GET path=/api/v1/users status=200 duration_ms=42
```

### Log to a file

```json
{
  "name": "file-log",
  "type": "accessLog",
  "accessLog": {
    "path": "/var/log/vrata/access.log",
    "json": true,
    "onResponse": {
      "fields": {
        "method": "${request.method}",
        "path": "${request.path}",
        "status": "${response.status}",
        "duration_ms": "${duration.ms}"
      }
    }
  }
}
```

The file is created if it doesn't exist.

### Minimal (response only, essentials)

```json
{
  "name": "minimal-log",
  "type": "accessLog",
  "accessLog": {
    "path": "stdout",
    "json": true,
    "onResponse": {
      "fields": {
        "status": "${response.status}",
        "duration_ms": "${duration.ms}",
        "path": "${request.path}"
      }
    }
  }
}
```

### Include auth headers for audit

```json
{
  "name": "audit-log",
  "type": "accessLog",
  "accessLog": {
    "path": "stdout",
    "json": true,
    "onRequest": {
      "fields": {
        "id": "${id}",
        "method": "${request.method}",
        "path": "${request.path}",
        "user_id": "${request.header.X-User-ID}",
        "tenant": "${request.header.X-Tenant}"
      }
    },
    "onResponse": {
      "fields": {
        "id": "${id}",
        "status": "${response.status}",
        "duration_ms": "${duration.ms}"
      }
    }
  }
}
```

Works well with JWT middleware's `claimToHeaders` — JWT extracts claims into headers, access log captures them.

## No access log

Access logging is opt-in. If no route or group references an access log middleware, nothing is logged.
