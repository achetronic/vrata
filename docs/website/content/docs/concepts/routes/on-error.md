---
title: "Error Handling (onError)"
weight: 5
---

When the upstream is unreachable, Vrata evaluates `onError` rules to decide what to do instead of returning a generic 502.

## Configuration

```json
{
  "onError": [
    {
      "on": ["connection_refused", "timeout"],
      "forward": {"destinations": [{"destinationId": "fallback-svc", "weight": 100}]}
    },
    {
      "on": ["circuit_open"],
      "directResponse": {"status": 503, "body": "{\"retry_after\": 30}"}
    },
    {
      "on": ["infrastructure"],
      "redirect": {"url": "https://status.example.com", "code": 302}
    }
  ]
}
```

Rules are evaluated in order. The first match wins.

## Error types

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

## Wildcards

| Wildcard | Matches |
|----------|---------|
| `infrastructure` | All of the above |
| `all` | All of the above (forward-compatible) |

## Fallback forward headers

When `onError` triggers a `forward`, Vrata injects headers so the fallback service knows why:

| Header | Example |
|--------|---------|
| `X-Vrata-Error` | `connection_refused` |
| `X-Vrata-Error-Status` | `502` |
| `X-Vrata-Error-Destination` | `api-svc` |
| `X-Vrata-Error-Endpoint` | `10.0.1.14:8080` |
| `X-Vrata-Original-Path` | `/api/v1/orders` |

## Default behaviour

Without `onError`, Vrata returns a JSON error:

```json
{"error": "connection refused"}
```

With `Content-Type: application/json` and the appropriate status code (502, 503, or 504).
