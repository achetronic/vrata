---
title: "Destinations"
weight: 2
---

A Destination represents a backend service that Vrata can forward traffic to. It's the bridge between your proxy and your upstream applications.

## What a destination does

1. **Identifies the backend** — by hostname and port, or by an explicit list of endpoints
2. **Manages connections** — connection pooling, timeouts, TLS to upstream
3. **Balances across endpoints** — when a destination has multiple pods/IPs, picks which one handles each request
4. **Protects the backend** — circuit breakers, health checks, outlier detection
5. **Discovers pods** — optional Kubernetes EndpointSlice watching for automatic pod discovery

Destinations are independent entities. Multiple routes can forward to the same destination. When you update a destination, every route that references it benefits automatically.

## Creating a destination

```bash
curl -X POST localhost:8080/api/v1/destinations \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "api-service",
    "host": "api-svc.default.svc.cluster.local",
    "port": 80
  }'
```

## Structure

```json
{
  "name": "api-service",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80,
  "endpoints": [
    {"host": "10.0.1.10", "port": 8080}
  ],
  "options": {
    "timeouts": { ... },
    "endpointBalancing": { ... },
    "circuitBreaker": { ... },
    "healthCheck": { ... },
    "outlierDetection": { ... },
    "tls": { ... },
    "discovery": { ... },
    "http2": false,
    "maxRequestsPerConnection": 0
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | required | Unique name |
| `host` | string | required | Backend hostname or IP |
| `port` | number | required | Backend port |
| `endpoints` | array | — | Explicit endpoint list (`host` + `port` per entry) |
| `options` | object | — | All advanced options (see below) |

## All options

| Option | Type | Description | Details |
|--------|------|-------------|---------|
| `timeouts` | object | 7 granular timeouts for every stage of the upstream connection | [Timeouts]({{< relref "timeouts" >}}) |
| `endpointBalancing` | object | 6 algorithms for picking which pod handles each request | [Endpoint Balancing]({{< relref "endpoint-balancing" >}}) |
| `circuitBreaker` | object | Stop sending traffic when consecutive errors cross a threshold | [Circuit Breaker]({{< relref "circuit-breaker" >}}) |
| `healthCheck` | object | Active HTTP probes to detect unhealthy endpoints | [Health Checks]({{< relref "health-checks" >}}) |
| `outlierDetection` | object | Passive ejection based on real traffic errors | [Outlier Detection]({{< relref "outlier-detection" >}}) |
| `tls` | object | Encrypt connections to backends (TLS, mTLS) | [TLS to Upstream]({{< relref "tls-upstream" >}}) |
| `discovery` | object | Auto-discover pod IPs via EndpointSlice watches | [Kubernetes Discovery]({{< relref "kubernetes-discovery" >}}) |
| `http2` | bool | Use HTTP/2 to upstream (required for gRPC backends) | — |
| `maxRequestsPerConnection` | number | Close connection after N requests (0 = unlimited) | — |

## How routes reference destinations

Routes don't connect to backends directly. They reference destinations by ID with a weight:

```json
{
  "forward": {
    "destinations": [
      {"destinationId": "<destination-id>", "weight": 100}
    ]
  }
}
```

A single route can reference multiple destinations with weights for canary deploys or A/B testing.
