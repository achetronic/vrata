---
title: "Metrics"
weight: 3
---

Enable Prometheus metrics on any listener. Each listener gets its own isolated registry and scrape endpoint, so different listeners can collect different dimensions.

## Configuration

```json
{
  "metrics": {
    "path": "/metrics",
    "collect": {
      "route": true,
      "destination": true,
      "endpoint": false,
      "middleware": true,
      "listener": true
    },
    "histograms": {
      "durationBuckets": [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10],
      "sizeBuckets": [100, 1000, 10000, 100000, 1000000]
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | `/metrics` | HTTP path for the Prometheus scrape endpoint |
| `collect` | object | all on except endpoint | Which metric dimensions to collect |
| `collect.route` | bool | `true` | Route-level metrics (7 metrics) |
| `collect.destination` | bool | `true` | Destination-level metrics (4 metrics) |
| `collect.endpoint` | bool | `false` | Per-pod metrics (4 metrics) — high cardinality |
| `collect.middleware` | bool | `true` | Middleware-level metrics (3 metrics) |
| `collect.listener` | bool | `true` | Listener-level metrics (3 metrics) |
| `histograms.durationBuckets` | float[] | Prometheus defaults | Custom boundaries for `_duration_seconds` histograms |
| `histograms.sizeBuckets` | float[] | Prometheus defaults | Custom boundaries for byte-counting histograms |

## Examples

### All dimensions (except per-pod)

```json
{
  "name": "production",
  "port": 8443,
  "metrics": {
    "path": "/metrics",
    "collect": {
      "route": true,
      "destination": true,
      "endpoint": false,
      "middleware": true,
      "listener": true
    }
  }
}
```

This is the recommended configuration. 21 metrics with manageable cardinality.

### Including per-pod metrics

```json
{
  "name": "debug",
  "port": 9090,
  "metrics": {
    "path": "/metrics",
    "collect": {
      "route": true,
      "destination": true,
      "endpoint": true,
      "middleware": true,
      "listener": true
    }
  }
}
```

All 22 metrics. Enable `endpoint` only when you need to debug individual pods — it generates one time series per pod × status code combination.

### Minimal (route metrics only)

```json
{
  "name": "minimal",
  "port": 8080,
  "metrics": {
    "path": "/metrics",
    "collect": {
      "route": true,
      "destination": false,
      "endpoint": false,
      "middleware": false,
      "listener": false
    }
  }
}
```

### Custom histogram buckets for low-latency APIs

```json
{
  "name": "fast-api",
  "port": 8080,
  "metrics": {
    "path": "/metrics",
    "histograms": {
      "durationBuckets": [0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1]
    }
  }
}
```

### Custom metrics path

```json
{
  "metrics": {
    "path": "/-/prometheus/metrics"
  }
}
```

### Two listeners, different metrics

```json
[
  {
    "name": "public",
    "port": 443,
    "metrics": {
      "path": "/metrics",
      "collect": {"route": true, "destination": true, "middleware": true, "listener": true}
    }
  },
  {
    "name": "internal",
    "port": 8080,
    "metrics": {
      "path": "/metrics",
      "collect": {"route": true, "destination": true, "endpoint": true, "middleware": true, "listener": true}
    }
  }
]
```

### No metrics

Omit the `metrics` field entirely. No Prometheus endpoint is served on this listener.

---

## Available metrics

### Route metrics

Enabled by default (`collect.route: true`).

| Metric | Type | Labels |
|--------|------|--------|
| `vrata_route_requests_total` | counter | route, group, method, status_code, status_class |
| `vrata_route_duration_seconds` | histogram | route, group, method |
| `vrata_route_request_bytes_total` | counter | route, group |
| `vrata_route_response_bytes_total` | counter | route, group |
| `vrata_route_inflight_requests` | gauge | route, group |
| `vrata_route_retries_total` | counter | route, group, attempt |
| `vrata_mirror_requests_total` | counter | route, destination |

```promql
# Error rate per route
sum(rate(vrata_route_requests_total{status_class="5xx"}[5m])) by (route)
/ sum(rate(vrata_route_requests_total[5m])) by (route)

# P99 latency
histogram_quantile(0.99, sum(rate(vrata_route_duration_seconds_bucket[5m])) by (route, le))

# Retry storm detection
sum(rate(vrata_route_retries_total[5m])) by (route)
```

---

### Destination metrics

Enabled by default (`collect.destination: true`).

| Metric | Type | Labels |
|--------|------|--------|
| `vrata_destination_requests_total` | counter | destination, status_code, status_class |
| `vrata_destination_duration_seconds` | histogram | destination |
| `vrata_destination_inflight_requests` | gauge | destination |
| `vrata_destination_circuit_breaker_state` | gauge | destination |

Circuit breaker state: `0` = closed, `1` = open, `2` = half-open.

```promql
# Canary error rate comparison
sum(rate(vrata_destination_requests_total{status_class="5xx", destination="canary"}[5m]))
/ sum(rate(vrata_destination_requests_total{destination="canary"}[5m]))

# Circuit breaker firing
vrata_destination_circuit_breaker_state == 1
```

---

### Endpoint metrics

**Off by default** (`collect.endpoint: false`) — high cardinality. Enable when you need per-pod visibility.

| Metric | Type | Labels |
|--------|------|--------|
| `vrata_endpoint_requests_total` | counter | destination, endpoint, status_code, status_class |
| `vrata_endpoint_duration_seconds` | histogram | destination, endpoint |
| `vrata_endpoint_healthy` | gauge | destination, endpoint |
| `vrata_endpoint_consecutive_5xx` | gauge | destination, endpoint |

With 100 destinations × 10 pods × 5 status codes = **5000 time series** just for `vrata_endpoint_requests_total`.

```promql
# Find the slow pod
histogram_quantile(0.99, rate(vrata_endpoint_duration_seconds_bucket{destination="api"}[5m])) by (endpoint)

# Unhealthy endpoints
vrata_endpoint_healthy == 0
```

---

### Middleware metrics

Enabled by default (`collect.middleware: true`).

| Metric | Type | Labels |
|--------|------|--------|
| `vrata_middleware_duration_seconds` | histogram | middleware, type |
| `vrata_middleware_rejections_total` | counter | middleware, type, status_code |
| `vrata_middleware_passed_total` | counter | middleware, type |

```promql
# JWT validation overhead
histogram_quantile(0.99, rate(vrata_middleware_duration_seconds_bucket{type="jwt"}[5m]))

# Rate limit effectiveness
rate(vrata_middleware_rejections_total{type="rateLimit", status_code="429"}[5m])

# Auth denial rate
sum(rate(vrata_middleware_rejections_total{type="jwt"}[5m]))
/ (sum(rate(vrata_middleware_rejections_total{type="jwt"}[5m])) + sum(rate(vrata_middleware_passed_total{type="jwt"}[5m])))
```

---

### Listener connection metrics

Enabled by default (`collect.listener: true`).

| Metric | Type | Labels |
|--------|------|--------|
| `vrata_listener_connections_total` | counter | listener, address |
| `vrata_listener_active_connections` | gauge | listener, address |
| `vrata_listener_tls_handshake_errors_total` | counter | listener, address |

```promql
# Connection rate
rate(vrata_listener_connections_total[5m])

# Certificate problems
rate(vrata_listener_tls_handshake_errors_total[5m])

# Connection saturation
vrata_listener_active_connections
```
