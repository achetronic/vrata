---
title: "Health Checks"
weight: 4
---

Active health checks send periodic HTTP probes to each endpoint. Unhealthy endpoints are removed from the load balancing pool until they recover. This prevents traffic from being sent to pods that are crashed, starting up, or otherwise unable to serve requests.

## Configuration

```json
{
  "options": {
    "healthCheck": {
      "path": "/health",
      "interval": "10s",
      "timeout": "5s",
      "unhealthyThreshold": 3,
      "healthyThreshold": 2
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | required | HTTP path for the probe (`GET` request) |
| `interval` | string | `10s` | How often each endpoint is probed |
| `timeout` | string | `5s` | Max time to wait for a probe response |
| `unhealthyThreshold` | number | `3` | Consecutive failures before marking unhealthy |
| `healthyThreshold` | number | `2` | Consecutive successes before marking healthy again |

A response with status 200-399 is a success. Anything else (including timeouts and connection failures) is a failure.

## Examples

### Basic health check

```json
{
  "options": {
    "healthCheck": {
      "path": "/health"
    }
  }
}
```

Probes every 10s, marks unhealthy after 3 failures, marks healthy after 2 successes. Good defaults for most services.

### Fast failure detection

```json
{
  "options": {
    "healthCheck": {
      "path": "/health",
      "interval": "3s",
      "timeout": "2s",
      "unhealthyThreshold": 2,
      "healthyThreshold": 1
    }
  }
}
```

Detects failures within 6 seconds (2 failed probes × 3s interval). Good for critical services where you need fast failover.

### Slow warm-up services

```json
{
  "options": {
    "healthCheck": {
      "path": "/ready",
      "interval": "5s",
      "timeout": "10s",
      "unhealthyThreshold": 6,
      "healthyThreshold": 3
    }
  }
}
```

Tolerates longer startup times. Uses `/ready` instead of `/health` to check actual readiness (loaded caches, connected to DB, etc.).

### External service (longer intervals)

```json
{
  "options": {
    "healthCheck": {
      "path": "/ping",
      "interval": "30s",
      "timeout": "10s",
      "unhealthyThreshold": 3,
      "healthyThreshold": 2
    }
  }
}
```

For third-party APIs where you don't control the backend and don't want to send too many probes.

## How it works

Vrata sends `GET <scheme>://<endpoint-host>:<endpoint-port><path>` at the configured interval. If the destination uses TLS to upstream, the probe also uses TLS.

The health check runs independently per endpoint. An endpoint is removed from the pool after `unhealthyThreshold` consecutive failures and restored after `healthyThreshold` consecutive successes. The hysteresis prevents flapping — a single failed probe doesn't remove an endpoint.

## vs Outlier Detection

| | Health Checks | Outlier Detection |
|---|---|---|
| **How** | Active probes (extra HTTP requests) | Passive (watches real traffic responses) |
| **Detects** | Total failures (crashed, unreachable) | Degraded performance (slow responses, 5xx errors) |
| **Cost** | Extra HTTP requests per endpoint per interval | Zero overhead |
| **Best for** | Backend may be silently broken | Backend returns errors under load |

Use both for defense in depth: health checks catch silent failures (backend accepting connections but not serving), outlier detection catches degraded performance in real traffic.

## Monitoring

The `vrata_endpoint_healthy` gauge (requires `collect.endpoint: true`) shows the current health state:

| Value | Meaning |
|-------|---------|
| `1` | Healthy (in the pool) |
| `0` | Unhealthy (ejected) |
