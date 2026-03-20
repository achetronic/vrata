---
title: "Retry"
weight: 2
---

Retries automatically resend failed requests to the upstream. Useful for transient failures like connection resets, temporary 503s, or backend deploys that cause brief unavailability.

## Configuration

Retry lives inside `forward`:

```json
{
  "forward": {
    "destinations": [{"destinationId": "<id>", "weight": 100}],
    "retry": {
      "attempts": 3,
      "perAttemptTimeout": "5s",
      "on": ["server-error", "connection-failure"],
      "backoff": {"base": "100ms", "max": "1s"}
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `attempts` | number | required | Max retry attempts (the original request doesn't count) |
| `perAttemptTimeout` | string | — | Deadline for each individual attempt |
| `on` | string[] | `["server-error", "connection-failure"]` | Conditions that trigger a retry |
| `retriableCodes` | number[] | `[]` | HTTP codes that trigger retry (only when `on` includes `retriable-codes`) |
| `backoff.base` | string | `100ms` | Initial backoff delay |
| `backoff.max` | string | `1s` | Maximum backoff delay |

## Retry conditions

| Condition | When it triggers |
|-----------|-----------------|
| `server-error` | Upstream returns any 5xx status code |
| `connection-failure` | TCP connect fails, connection reset, or DNS failure |
| `gateway-error` | Upstream returns 502, 503, or 504 specifically |
| `retriable-codes` | Upstream returns a status code listed in `retriableCodes` |

## Examples

### Basic retry (server errors + connection failures)

```json
{
  "forward": {
    "retry": {
      "attempts": 3,
      "on": ["server-error", "connection-failure"]
    }
  }
}
```

Retries up to 3 times on any 5xx or connection failure. Default backoff (100ms base, 1s max).

### Retry only on gateway errors

```json
{
  "forward": {
    "retry": {
      "attempts": 2,
      "on": ["gateway-error"]
    }
  }
}
```

Only retries on 502, 503, 504 — not on application-level 500 errors. Use when 500 means "bad request" but 502/503/504 means "backend temporarily unavailable".

### Retry specific status codes

```json
{
  "forward": {
    "retry": {
      "attempts": 3,
      "on": ["retriable-codes", "connection-failure"],
      "retriableCodes": [429, 503]
    }
  }
}
```

Retries on 429 (rate limited) and 503 (service unavailable), plus connection failures. Does not retry on 500 or 502.

### Aggressive retry with short timeout

```json
{
  "forward": {
    "retry": {
      "attempts": 5,
      "perAttemptTimeout": "2s",
      "on": ["server-error", "connection-failure"],
      "backoff": {"base": "50ms", "max": "500ms"}
    }
  }
}
```

5 fast attempts with 2s per-attempt timeout and short backoff. Total worst case: 5 × 2s + backoff ≈ 11s. Use for idempotent, latency-sensitive endpoints.

### Conservative retry with long timeout

```json
{
  "forward": {
    "retry": {
      "attempts": 2,
      "perAttemptTimeout": "10s",
      "on": ["connection-failure"],
      "backoff": {"base": "1s", "max": "5s"}
    }
  }
}
```

Only retries on connection failures (not server errors). Longer timeouts and backoff. Use for non-idempotent operations where you only want to retry infrastructure failures.

### No retry

Omit the `retry` field entirely:

```json
{
  "forward": {
    "destinations": [{"destinationId": "<id>", "weight": 100}]
  }
}
```

## Backoff

Exponential backoff with jitter: `base × 2^attempt`, capped at `max`, with 50-100% random jitter.

```
Attempt 1: 100ms × 1 ± jitter → 50-200ms
Attempt 2: 100ms × 2 ± jitter → 100-400ms
Attempt 3: 100ms × 4 ± jitter → 200-800ms (capped at max)
```

Jitter prevents retry storms when multiple requests fail simultaneously.

## Interaction with timeouts

Each attempt respects `perAttemptTimeout`. The route-level `forward.timeouts.request` (or the destination's `options.timeouts.request`) is the overall ceiling:

```
Total time ≤ forward.timeouts.request (30s)
  Attempt 1: perAttemptTimeout (5s) + backoff
  Attempt 2: perAttemptTimeout (5s) + backoff
  Attempt 3: perAttemptTimeout (5s)
```

If the total exceeds `forward.timeouts.request`, the current attempt is cancelled. No more retries.

## Group-level default

Set a default retry for all routes in a group:

```json
{
  "name": "store-api",
  "routeIds": ["<route-1>", "<route-2>"],
  "retryDefault": {
    "attempts": 2,
    "on": ["server-error"],
    "backoff": {"base": "100ms", "max": "1s"}
  },
  "includeAttemptCount": true
}
```

When `includeAttemptCount` is true, Vrata adds `X-Vrata-Attempt-Count` to the upstream request so the backend knows it's a retry.

Individual routes override this by setting their own `forward.retry`.

## Monitoring

`vrata_route_retries_total` (counter, with `attempt` label) tracks retry activity:

```
rate(vrata_route_retries_total{route="api-route"}[5m])
```

A spike in retries usually indicates upstream instability.
