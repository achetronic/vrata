---
title: "Forwarding"
weight: 1
---

Forward a matched request to one or more upstream destinations. This is the most common route action — the request travels through Vrata to a backend service and the response is returned to the client.

## Configuration

```json
{
  "name": "api-route",
  "match": {"pathPrefix": "/api/v1"},
  "forward": {
    "destinations": [{"destinationId": "<dest-id>", "weight": 100}]
  }
}
```

## All fields

| Field | Type | Default | Description | Details |
|-------|------|---------|-------------|---------|
| `destinations` | array | required | Upstream destinations with traffic weights | — |
| `destinationBalancing` | object | `WEIGHTED_RANDOM` | How to pick which destination gets each request | [Destination Balancing]({{< relref "destination-balancing" >}}) |
| `timeouts.request` | string | — | Total request deadline (overrides destination timeout) | — |
| `retry` | object | — | Automatic retry on failure | [Retry]({{< relref "retry" >}}) |
| `rewrite` | object | — | Transform URL before forwarding | [URL Rewrite]({{< relref "rewrite" >}}) |
| `mirror` | object | — | Copy traffic to a shadow destination | [Traffic Mirror]({{< relref "mirror" >}}) |
| `maxGrpcTimeout` | string | — | Cap the timeout a gRPC client can request via `grpc-timeout` | — |

## Destinations

Each entry in `destinations` references a [Destination]({{< relref "/docs/concepts/destinations" >}}) by ID and assigns a traffic weight.

```json
{
  "forward": {
    "destinations": [
      {"destinationId": "<stable-id>", "weight": 90},
      {"destinationId": "<canary-id>",  "weight": 10}
    ]
  }
}
```

When there are multiple destinations, weights must sum to 100. When there is only one destination, the weight field is ignored.

## Examples

### Single destination

```json
{
  "name": "users-service",
  "match": {"pathPrefix": "/users"},
  "forward": {
    "destinations": [{"destinationId": "<users-svc>", "weight": 100}]
  }
}
```

All traffic goes to one destination.

### Canary split (90/10)

```json
{
  "name": "api-canary",
  "match": {"pathPrefix": "/api"},
  "forward": {
    "destinations": [
      {"destinationId": "<stable>", "weight": 90},
      {"destinationId": "<canary>", "weight": 10}
    ]
  }
}
```

10% of traffic goes to the canary. No stickiness — each request is routed independently. Use [Destination Balancing]({{< relref "destination-balancing" >}}) to pin clients.

### Forward with request timeout

```json
{
  "name": "slow-upstream",
  "match": {"pathPrefix": "/reports"},
  "forward": {
    "destinations": [{"destinationId": "<reports-svc>", "weight": 100}],
    "timeouts": {"request": "30s"}
  }
}
```

Cancels the request if the upstream doesn't respond within 30 seconds and returns a `504 Gateway Timeout` JSON error. This timeout is the outer watchdog — the destination's own `options.timeouts.request` is the inner ceiling.

### Forward with retry and timeout

```json
{
  "name": "resilient-api",
  "match": {"pathPrefix": "/api"},
  "forward": {
    "destinations": [{"destinationId": "<api-svc>", "weight": 100}],
    "timeouts": {"request": "10s"},
    "retry": {
      "attempts": 3,
      "on": ["server-error", "connection-failure"],
      "backoff": {"base": "100ms", "max": "1s"}
    }
  }
}
```

Up to 3 retries on server errors or connection failures. The 10s watchdog covers the total including all retry attempts.

### gRPC forwarding with timeout cap

```json
{
  "name": "grpc-service",
  "match": {"pathPrefix": "/helloworld.Greeter", "grpc": true},
  "forward": {
    "destinations": [{"destinationId": "<grpc-svc>", "weight": 100}],
    "maxGrpcTimeout": "5s"
  }
}
```

Clamps any `grpc-timeout` header from the client to 5s maximum. The upstream never sees a timeout longer than 5s regardless of what the client requested.

## Request timeout

`forward.timeouts.request` is the outermost watchdog. If the total time — including retries and upstream round-trips — exceeds this value, Vrata cancels the request and returns a `504 Gateway Timeout` structured JSON error:

```json
{"error": "timeout", "status": 504, "message": "request timeout"}
```

The detail level of the error response is controlled by the listener's [`proxyErrors`]({{< relref "/docs/concepts/listeners/proxy-errors" >}}) setting.

If `forward.timeouts.request` is not set, Vrata falls back to the selected destination's `options.timeouts.request`. If neither is set, there is no outer watchdog and the request can run indefinitely (bounded only by destination-level transport timeouts).
