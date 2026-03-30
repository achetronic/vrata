---
title: "Proxy Error Responses"
weight: 5
---

Control how Vrata formats its own error responses — infrastructure failures, no matching route, circuit breaker open, etc. Each listener can expose a different level of detail, so a public listener hides internals while an internal listener shows full debugging context.

## Configuration

```json
{
  "proxyErrors": {
    "detail": "standard"
  }
}
```

The only field is `detail`. When `proxyErrors` is absent or `detail` is empty, `standard` is used.

## Detail levels

| Level | Fields | Use case |
|-------|--------|----------|
| `minimal` | `error`, `status` | Public-facing listeners — hide internals |
| `standard` | `error`, `status`, `message` | Default — enough for debugging without exposing infrastructure |
| `full` | `error`, `status`, `message`, `destination`, `endpoint`, `timestamp` | Internal listeners — full debugging context |

## Example responses

**minimal:**
```json
{"error": "connection_refused", "status": 502}
```

**standard** (default):
```json
{"error": "connection_refused", "status": 502, "message": "upstream connection refused"}
```

**full:**
```json
{
  "error": "connection_refused",
  "status": 502,
  "message": "upstream connection refused",
  "destination": "api-svc",
  "endpoint": "10.0.1.14:8080",
  "timestamp": "2026-03-30T14:22:01Z"
}
```

## Error types

Every proxy-generated error carries a classified `error` field:

| Type | When it fires |
|------|---------------|
| `connection_refused` | TCP connect refused (backend down) |
| `connection_reset` | Connection established but cut by backend |
| `dns_failure` | Hostname can't be resolved |
| `timeout` | Request or per-attempt timeout expired |
| `tls_handshake_failure` | TLS handshake failed |
| `circuit_open` | Circuit breaker prevented the attempt |
| `no_destination` | No destination has healthy endpoints |
| `no_endpoint` | Destination exists but all endpoints are down |
| `no_route` | No route matched the request |
| `request_headers_too_large` | Request headers exceeded `maxRequestHeadersKB` |

## Status codes

| Error type | HTTP status |
|------------|-------------|
| `circuit_open` | 503 |
| `timeout` | 504 |
| `no_route` | 404 |
| `request_headers_too_large` | 431 |
| Everything else | 502 |

## Per-listener example

A common setup: public listener with `minimal`, internal with `full`.

```bash
# Public — clients see only the error type
curl -X POST localhost:8080/api/v1/listeners -d '{
  "name": "public",
  "port": 443,
  "proxyErrors": {"detail": "minimal"}
}'

# Internal — operators see full context
curl -X POST localhost:8080/api/v1/listeners -d '{
  "name": "internal",
  "port": 8081,
  "proxyErrors": {"detail": "full"}
}'
```
