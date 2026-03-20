---
title: "Destination Balancing"
weight: 6
---

When a route forwards to multiple destinations (e.g. canary deploys), Vrata picks which destination gets each request. This is level-1 balancing.

## Algorithms

### `WEIGHTED_RANDOM` (default)

Pick a destination by weighted random. No stickiness — each request is independent.

```json
{
  "forward": {
    "destinations": [
      {"destinationId": "stable", "weight": 80},
      {"destinationId": "canary", "weight": 20}
    ]
  }
}
```

### `WEIGHTED_CONSISTENT_HASH`

Pin clients to destinations using a session cookie and consistent hash ring. Disruption is proportional to weight changes.

```json
{
  "forward": {
    "destinationBalancing": {
      "algorithm": "WEIGHTED_CONSISTENT_HASH",
      "weightedConsistentHash": {
        "cookie": {"name": "_vrata_pin", "ttl": "1h"}
      }
    }
  }
}
```

### `STICKY`

Zero-disruption pinning via Redis. Existing clients always return to the same destination even when weights change. New clients follow the new distribution.

```json
{
  "forward": {
    "destinationBalancing": {
      "algorithm": "STICKY",
      "sticky": {
        "cookie": {"name": "_vrata_pin", "ttl": "1h"}
      }
    }
  }
}
```

Requires a session store (Redis) in the server config. Falls back to `WEIGHTED_CONSISTENT_HASH` if Redis is unavailable.

## vs Endpoint Balancing

| | Destination Balancing | Endpoint Balancing |
|---|---|---|
| **Scope** | Which service? | Which pod within that service? |
| **Configured on** | Route (`forward.destinationBalancing`) | Destination (`options.endpointBalancing`) |
| **Algorithms** | WEIGHTED_RANDOM, WCH, STICKY | ROUND_ROBIN, RANDOM, LEAST_REQUEST, RING_HASH, MAGLEV, STICKY |
