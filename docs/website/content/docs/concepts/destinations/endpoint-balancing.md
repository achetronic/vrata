---
title: "Endpoint Balancing"
weight: 2
---

When a destination has multiple endpoints (pods), Vrata needs to choose which one handles each request. This is level-2 load balancing — it happens after the destination is already selected.

## Why it matters

Round-robin works for stateless APIs. But if your service caches data per-user in memory, you want the same user hitting the same pod. If one pod is overloaded, you want least-request routing. If you need deterministic sharding, you want consistent hashing.

## Algorithms

### `ROUND_ROBIN` (default)

Cycles through endpoints in order. Every endpoint gets equal traffic.

```json
{
  "options": {
    "endpointBalancing": {
      "algorithm": "ROUND_ROBIN"
    }
  }
}
```

**Best for:** Stateless services with similar capacity across pods.

### `RANDOM`

Picks a random endpoint each time. Statistically similar to round-robin but without ordering guarantees.

**Best for:** Large endpoint pools where perfect distribution doesn't matter.

### `LEAST_REQUEST`

Picks the endpoint with the fewest active (in-flight) requests. Uses power-of-two-choices: randomly sample 2 endpoints, pick the one with fewer active requests.

```json
{
  "options": {
    "endpointBalancing": {
      "algorithm": "LEAST_REQUEST",
      "leastRequest": {
        "choiceCount": 2
      }
    }
  }
}
```

**Best for:** Endpoints with variable response times. Prevents slow pods from accumulating requests.

### `RING_HASH`

Consistent hashing with a virtual node ring. The hash key is computed from request data (header, cookie, or source IP). The same key always maps to the same endpoint — unless endpoints are added/removed.

```json
{
  "options": {
    "endpointBalancing": {
      "algorithm": "RING_HASH",
      "ringHash": {
        "ringSize": {"min": 1024, "max": 8388608},
        "hashPolicy": [
          {"header": {"name": "X-User-ID"}},
          {"cookie": {"name": "_session", "ttl": "1h"}},
          {"sourceIP": {"enabled": true}}
        ]
      }
    }
  }
}
```

Hash policies are evaluated in order. The first one that produces a value wins.

**Best for:** Session affinity, in-memory caches, sharded databases.

### `MAGLEV`

Google's Maglev consistent hash. Similar to ring hash but with better distribution uniformity and minimal disruption when endpoints change.

```json
{
  "options": {
    "endpointBalancing": {
      "algorithm": "MAGLEV",
      "maglev": {
        "tableSize": 65537,
        "hashPolicy": [
          {"header": {"name": "X-Tenant-ID"}}
        ]
      }
    }
  }
}
```

`tableSize` must be a prime number. Default: 65537.

**Best for:** Same as ring hash, with better uniformity at scale.

### `STICKY`

Redis-backed zero-disruption endpoint pinning. New clients get a random endpoint. Existing clients always return to the same endpoint — even if endpoints are added or removed.

```json
{
  "options": {
    "endpointBalancing": {
      "algorithm": "STICKY",
      "sticky": {
        "cookie": {"name": "_vrata_ep", "ttl": "1h"}
      }
    }
  }
}
```

Requires a session store (Redis) configured in the server config.

**Best for:** Stateful services where any disruption causes user-visible impact (e.g. WebSocket servers, in-memory session stores).

## Hash policies

For `RING_HASH` and `MAGLEV`, you define what request data feeds the hash:

| Policy | What it hashes | Auto-generates? |
|--------|---------------|-----------------|
| `header` | Named request header value | No |
| `cookie` | Named cookie value | Yes — sets cookie if missing |
| `sourceIP` | Client IP address | No |

Policies are evaluated in order. The first one that produces a value wins.

Each hash includes the destination ID as salt — the same cookie value produces different hashes for different destinations, preventing cross-destination correlation.
