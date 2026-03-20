---
title: "Outlier Detection"
weight: 5
---

Outlier detection automatically ejects endpoints that return consecutive errors — without requiring active health probes. It watches real traffic and reacts to actual failures, removing bad pods from the pool before they cause user-visible impact.

## Configuration

```json
{
  "options": {
    "outlierDetection": {
      "consecutive5xx": 5,
      "consecutiveGatewayErrors": 3,
      "interval": "10s",
      "baseEjectionTime": "30s",
      "maxEjectionPercent": 10
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `consecutive5xx` | number | `5` | Consecutive 5xx responses to trigger ejection |
| `consecutiveGatewayErrors` | number | `0` | Consecutive 502/503/504 to trigger ejection (0 = disabled) |
| `interval` | string | `10s` | How often ejection conditions are evaluated |
| `baseEjectionTime` | string | `30s` | How long an endpoint stays ejected (first ejection) |
| `maxEjectionPercent` | number | `10` | Max percentage of endpoints that can be ejected simultaneously |

## Examples

### Basic outlier detection

```json
{
  "options": {
    "outlierDetection": {
      "consecutive5xx": 5,
      "baseEjectionTime": "30s"
    }
  }
}
```

Ejects endpoints after 5 consecutive 5xx errors. Ejected for 30s, then restored.

### Aggressive detection (critical path)

```json
{
  "options": {
    "outlierDetection": {
      "consecutive5xx": 3,
      "consecutiveGatewayErrors": 2,
      "interval": "5s",
      "baseEjectionTime": "60s",
      "maxEjectionPercent": 30
    }
  }
}
```

Faster detection (check every 5s, trip at 3 errors). Allows ejecting up to 30% of endpoints. Longer ejection time. Use for critical payment or auth paths where a bad pod is worse than reduced capacity.

### Conservative detection (large pool)

```json
{
  "options": {
    "outlierDetection": {
      "consecutive5xx": 10,
      "interval": "30s",
      "baseEjectionTime": "15s",
      "maxEjectionPercent": 5
    }
  }
}
```

Tolerates more errors before ejecting. Short ejection time gives pods a chance to recover quickly. Low max ejection percentage protects large pools from cascading ejections.

### Gateway errors only

```json
{
  "options": {
    "outlierDetection": {
      "consecutive5xx": 0,
      "consecutiveGatewayErrors": 3,
      "baseEjectionTime": "30s"
    }
  }
}
```

Only ejects on 502/503/504 — ignores application-level 500 errors. Useful when your backend returns 500 for business logic errors that shouldn't trigger ejection.

## How ejection works

When an endpoint accumulates `consecutive5xx` errors in a row (checked every `interval`), it's ejected. The ejection duration increases with each consecutive ejection:

```
First ejection:  baseEjectionTime × 1 = 30s
Second ejection: baseEjectionTime × 2 = 60s
Third ejection:  baseEjectionTime × 3 = 90s
```

After the ejection expires, the endpoint is restored to the pool. If it fails again, it's ejected for longer. A single successful response resets the consecutive error counter.

The `maxEjectionPercent` is a safety valve — at least `100 - maxEjectionPercent` percent of the pool always stays active, even if all endpoints are failing.

## vs Health Checks

| | Health Checks | Outlier Detection |
|---|---|---|
| **How** | Active probes (extra HTTP requests) | Passive (watches real traffic responses) |
| **Detects** | Total failures (crashed, unreachable) | Degraded performance (errors under load) |
| **Cost** | Extra HTTP requests per endpoint per interval | Zero — piggybacks on real traffic |
| **Best for** | Backend may be silently broken | Backend returns errors under load |

Use both for defense in depth.

## Monitoring

With `collect.endpoint: true`:

- `vrata_endpoint_healthy` — `0` when ejected, `1` when healthy
- `vrata_endpoint_consecutive_5xx` — current error streak length per endpoint
