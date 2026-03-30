---
title: "Circuit Breaker"
weight: 3
---

The circuit breaker protects your upstream from being overwhelmed when it's failing. When consecutive errors cross a threshold, Vrata stops sending traffic — giving the backend time to recover instead of hammering it with requests that will also fail.

## How it works

The circuit breaker has three states:

1. **Closed** (normal) — all requests pass through. Failures are counted.
2. **Open** — all requests are immediately rejected with `503 Service Unavailable`. No traffic reaches the backend.
3. **Half-open** — after `openDuration` expires, a single probe request is allowed through. If it succeeds, the circuit closes. If it fails, the circuit reopens for another `openDuration`.

```
Closed ──[failureThreshold consecutive 5xx]──► Open
                                                  │
                                          [openDuration expires]
                                                  │
                                                  ▼
                                              Half-open
                                              ├─ success → Closed
                                              └─ failure → Open
```

## Configuration

```json
{
  "options": {
    "circuitBreaker": {
      "maxConnections": 100,
      "maxPendingRequests": 50,
      "maxRequests": 200,
      "maxRetries": 3,
      "failureThreshold": 5,
      "openDuration": "30s"
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `maxConnections` | number | `1024` | Max concurrent TCP connections to this destination |
| `maxPendingRequests` | number | `1024` | Max requests waiting for a connection from the pool |
| `maxRequests` | number | `1024` | Max concurrent HTTP requests in flight |
| `maxRetries` | number | `3` | Max concurrent retries in flight |
| `failureThreshold` | number | `5` | Consecutive 5xx responses that trip the circuit |
| `openDuration` | string | `30s` | How long the circuit stays open before probing |

## Examples

### Sensitive backend (trip quickly, recover slowly)

```json
{
  "options": {
    "circuitBreaker": {
      "failureThreshold": 3,
      "openDuration": "60s"
    }
  }
}
```

3 consecutive 5xx errors open the circuit for 60 seconds. Use for backends where continued traffic during failure makes things worse (e.g. databases behind an API).

### Resilient backend (trip slowly, recover quickly)

```json
{
  "options": {
    "circuitBreaker": {
      "failureThreshold": 10,
      "openDuration": "10s"
    }
  }
}
```

Tolerates more errors before tripping. Recovers quickly. Use for backends with occasional transient errors that self-heal.

### Connection limits only (no failure-based tripping)

```json
{
  "options": {
    "circuitBreaker": {
      "maxConnections": 50,
      "maxPendingRequests": 100,
      "maxRequests": 200,
      "failureThreshold": 0
    }
  }
}
```

Setting `failureThreshold: 0` disables the error-based circuit. The breaker only enforces connection and request limits. Requests exceeding the limits receive `503`.

### Full protection

```json
{
  "options": {
    "circuitBreaker": {
      "maxConnections": 100,
      "maxPendingRequests": 50,
      "maxRequests": 200,
      "maxRetries": 3,
      "failureThreshold": 5,
      "openDuration": "30s"
    }
  }
}
```

Limits connections, pending requests, in-flight requests, and concurrent retries. Plus error-based tripping at 5 consecutive 5xx.

## Interaction with proxy error responses

When the circuit is open, Vrata classifies this as `circuit_open` and returns a structured JSON error with HTTP 503. The detail level depends on the listener's `proxyErrors.detail` setting:

```json
{"error": "circuit_open", "status": 503, "message": "circuit breaker open"}
```

## Monitoring

The `vrata_destination_circuit_breaker_state` gauge shows the current state:

| Value | State |
|-------|-------|
| `0` | Closed (normal) |
| `1` | Open (rejecting) |
| `2` | Half-open (probing) |

Alert on `vrata_destination_circuit_breaker_state == 1` to know when a backend is being protected.
