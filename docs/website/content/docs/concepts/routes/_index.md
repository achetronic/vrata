---
title: "Routes"
weight: 3
---

A Route defines how to match incoming HTTP requests and what to do with them. It's the core of Vrata's traffic management — every request that hits the proxy is evaluated against the route table to find a match.

## What a route does

1. **Matches requests** — by path, headers, methods, hostnames, query params, gRPC flag, or CEL expressions
2. **Decides what action to take** — forward to a backend, redirect, or return a fixed response
3. **Applies middlewares** — any number of middlewares in order

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
  "middlewareOverrides": { ... }
}
```

Only one of `forward`, `redirect`, or `directResponse` should be set.

## All fields

| Field | Type | Description | Details |
|-------|------|-------------|---------|
| `name` | string | Unique name | — |
| `match` | object | Request matching rules | [Matching]({{< relref "matching" >}}) |
| `forward` | object | Forward to upstream destinations | [Forwarding]({{< relref "forwarding" >}}) |
| `redirect` | object | Return HTTP redirect (`url`, `scheme`, `host`, `path`, `stripQuery`, `code`) | — |
| `directResponse` | object | Return fixed response (`status`, `body`) | [Direct Response]({{< relref "direct-response" >}}) |
| `middlewareIds` | array | Middleware IDs to apply (in order) | — |
| `middlewareOverrides` | map | Per-middleware overrides (skipWhen, onlyWhen, disabled) | — |

## Route priority

Routes are evaluated in this order:

1. Exact path matches first (`path`)
2. Then prefix matches, longest prefix first (`pathPrefix`)
3. Then regex matches (`pathRegex`)
4. Within the same path type, more specific matchers (more headers, hostnames, etc.) win

The first matching route handles the request. If no route matches, Vrata returns a structured JSON error. The detail level is controlled by the listener's [`proxyErrors`]({{< relref "/docs/concepts/listeners/proxy-errors" >}}) setting.
