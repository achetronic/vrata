---
title: "Routes"
weight: 3
---

A Route defines how to match incoming HTTP requests and what to do with them. It's the core of Vrata's traffic management — every request that hits the proxy is evaluated against the route table to find a match.

## What a route does

1. **Matches requests** — by path, headers, methods, hostnames, query params, gRPC flag, or CEL expressions
2. **Decides what action to take** — forward to a backend, redirect, or return a fixed response
3. **Applies middlewares** — any number of middlewares in order
4. **Handles errors** — onError rules define fallback actions when the upstream fails

Each route operates in exactly **one** of three modes: `forward`, `redirect`, or `directResponse`.

## Creating a route

```bash
curl -X POST localhost:8080/api/v1/routes \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "api-route",
    "match": {"pathPrefix": "/api/v1"},
    "forward": {
      "destinations": [{"destinationId": "<dest-id>", "weight": 100}]
    }
  }'
```

## Structure

```json
{
  "name": "api-route",
  "match": { ... },
  "forward": {
    "destinations": [{"destinationId": "<id>", "weight": 100}],
    "destinationBalancing": { ... },
    "timeouts": { ... },
    "retry": { ... },
    "rewrite": { ... },
    "mirror": { ... },
    "maxGrpcTimeout": "0s"
  },
  "redirect": { ... },
  "directResponse": { ... },
  "middlewareIds": ["jwt-auth", "cors"],
  "middlewareOverrides": { ... },
  "onError": [{ ... }]
}
```

Only one of `forward`, `redirect`, or `directResponse` should be set.

## All fields

| Field | Type | Description | Details |
|-------|------|-------------|---------|
| `name` | string | Unique name | — |
| `match` | object | Request matching rules | [Matching]({{< relref "matching" >}}) |
| `forward` | object | Forward to upstream destinations | See forward fields below |
| `redirect` | object | Return HTTP redirect (`url`, `scheme`, `host`, `path`, `stripQuery`, `code`) | — |
| `directResponse` | object | Return fixed response (`status`, `body`) | — |
| `middlewareIds` | array | Middleware IDs to apply (in order) | — |
| `middlewareOverrides` | map | Per-middleware overrides (skipWhen, onlyWhen, disabled) | — |
| `onError` | array | Fallback rules when forward fails | [Error Handling]({{< relref "on-error" >}}) |

## Forward fields

| Field | Type | Description | Details |
|-------|------|-------------|---------|
| `destinations` | array | Destination refs with weights | — |
| `destinationBalancing` | object | How to pick which destination gets each request | [Destination Balancing]({{< relref "destination-balancing" >}}) |
| `timeouts` | object | Route-level request timeout (`request` field, overrides destination) | — |
| `retry` | object | Automatic retry on failure | [Retry]({{< relref "retry" >}}) |
| `rewrite` | object | Transform URL before forwarding | [URL Rewrite]({{< relref "rewrite" >}}) |
| `mirror` | object | Copy traffic to a shadow destination | [Traffic Mirror]({{< relref "mirror" >}}) |
| `maxGrpcTimeout` | string | Cap gRPC timeout from `grpc-timeout` header | — |

## Route priority

Routes are evaluated in this order:

1. Exact path matches first (`path`)
2. Then prefix matches, longest prefix first (`pathPrefix`)
3. Then regex matches (`pathRegex`)
4. Within the same path type, more specific matchers (more headers, hostnames, etc.) win

The first matching route handles the request. If no route matches, Vrata returns `404 Not Found`.
