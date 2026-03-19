---
title: "Traffic Mirror"
weight: 4
---

Send a copy of live traffic to an additional destination for testing or observability. The mirrored request is fire-and-forget — its response is discarded and never affects the client.

## Configuration

Mirror lives inside `forward`:

```json
{
  "forward": {
    "destinations": [{"destinationId": "<primary>", "weight": 100}],
    "mirror": {
      "destinationId": "<shadow-service>",
      "percentage": 10
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `destinationId` | string | required | Destination that receives the mirrored copy |
| `percentage` | number | `100` | Fraction of requests to mirror (0-100) |

## Examples

### Mirror all traffic to a shadow service

```json
{
  "forward": {
    "destinations": [{"destinationId": "<primary>", "weight": 100}],
    "mirror": {
      "destinationId": "<shadow>",
      "percentage": 100
    }
  }
}
```

Every request is copied to the shadow service. The shadow receives the same method, path, headers, and body. Its response is discarded.

### Mirror 10% for canary validation

```json
{
  "forward": {
    "destinations": [{"destinationId": "<stable>", "weight": 100}],
    "mirror": {
      "destinationId": "<canary-v2>",
      "percentage": 10
    }
  }
}
```

10% of requests are copied to the new version. Compare error rates and latency between the primary and shadow in your metrics. The canary never affects real users.

### Mirror to an analytics service

```json
{
  "forward": {
    "destinations": [{"destinationId": "<api>", "weight": 100}],
    "mirror": {
      "destinationId": "<analytics>",
      "percentage": 100
    }
  }
}
```

Every request is mirrored to an analytics pipeline. The analytics service sees real production traffic patterns without being in the critical path.

### Mirror to a load testing target

```json
{
  "forward": {
    "destinations": [{"destinationId": "<production>", "weight": 100}],
    "mirror": {
      "destinationId": "<staging>",
      "percentage": 5
    }
  }
}
```

5% of real production traffic hits the staging environment. Better than synthetic load tests because it's real user behavior.

## How it works

1. Vrata receives the request and buffers the body in memory.
2. The original request proceeds to the primary destination(s) normally.
3. A copy of the request (method, path, headers, body) is sent to the mirror destination in a background goroutine.
4. The mirror's response is discarded — it never touches the client.
5. If the mirror destination is down or slow, it doesn't affect the primary request.

## Important notes

- **Body is buffered** — the entire request body is held in memory to send to both destinations. For very large uploads, this doubles memory usage.
- **No response comparison** — Vrata doesn't compare primary vs mirror responses. Use your metrics and logging to compare.
- **Timeouts apply** — the mirror request uses the mirror destination's configured timeouts. Slow mirrors are eventually abandoned.

## Monitoring

The `vrata_mirror_requests_total` counter (with `route` and `destination` labels) tracks how many mirror requests were sent:

```
rate(vrata_mirror_requests_total{destination="shadow-v2"}[5m])
```
