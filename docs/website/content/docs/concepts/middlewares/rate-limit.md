---
title: "Rate Limiting"
weight: 3
---

Limit requests per client IP using a token bucket algorithm. Protects your backend from abuse, scraping, and accidental traffic spikes.

## Configuration

```json
{
  "name": "rate-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 100,
    "burst": 200,
    "trustedProxies": ["10.0.0.0/8", "172.16.0.0/12"]
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `requestsPerSecond` | number | `10` | Sustained rate per client IP |
| `burst` | number | same as RPS | Maximum burst above sustained rate |
| `trustedProxies` | string[] | `[]` | CIDR ranges from which `X-Forwarded-For` is trusted |

## Examples

### Basic rate limit (10 RPS)

```json
{
  "name": "basic-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 10
  }
}
```

Each client IP can make 10 requests per second sustained. Burst defaults to 10 (no burst above sustained rate).

### API rate limit with burst

```json
{
  "name": "api-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 100,
    "burst": 500
  }
}
```

100 requests per second sustained. A client can send 500 requests instantly (burst), then must wait for tokens to refill at 100/s. Good for bursty API clients (page loads that fire many parallel requests).

### Strict limit (no burst)

```json
{
  "name": "strict-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 50,
    "burst": 50
  }
}
```

Setting `burst` equal to `requestsPerSecond` means no burst above the sustained rate.

### Behind AWS ALB / GCP GCLB

```json
{
  "name": "cloud-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 100,
    "burst": 200,
    "trustedProxies": [
      "10.0.0.0/8",
      "172.16.0.0/12",
      "192.168.0.0/16"
    ]
  }
}
```

When Vrata is behind a load balancer, the TCP remote address is the LB's IP — not the real client. `trustedProxies` tells Vrata to extract the real client IP from `X-Forwarded-For`, skipping trusted proxy addresses.

### Behind Cloudflare

```json
{
  "name": "cf-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 50,
    "burst": 100,
    "trustedProxies": [
      "173.245.48.0/20",
      "103.21.244.0/22",
      "103.22.200.0/22",
      "103.31.4.0/22",
      "141.101.64.0/18",
      "108.162.192.0/18",
      "190.93.240.0/20",
      "188.114.96.0/20",
      "197.234.240.0/22",
      "198.41.128.0/17",
      "162.158.0.0/15",
      "104.16.0.0/13",
      "104.24.0.0/14",
      "172.64.0.0/13",
      "131.0.72.0/22"
    ]
  }
}
```

### Very aggressive (login endpoint)

```json
{
  "name": "login-limit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 5,
    "burst": 10
  }
}
```

5 requests per second per IP on the login endpoint. Attach to specific routes via middleware overrides.

## How it works

Each client IP gets a token bucket:
- Bucket starts with `burst` tokens
- Each request consumes one token
- Tokens refill at `requestsPerSecond` per second
- When the bucket is empty → `429 Too Many Requests`

```
burst = 200, requestsPerSecond = 100

t=0:  200 tokens available (burst)
t=0:  Client sends 200 requests → 0 tokens left
t=1:  100 tokens refilled
t=1:  Client sends 100 requests → 0 tokens left
t=1:  Client sends request 101 → 429 Too Many Requests
```

## Client IP resolution

1. If `trustedProxies` is empty → use TCP remote address
2. If `trustedProxies` is set → walk `X-Forwarded-For` from right to left, skip trusted IPs, use the first non-trusted IP

This rightmost-non-trusted approach is secure against header spoofing.

## Cleanup

Stale client buckets are evicted every 60 seconds. When a new snapshot is activated and the routing table is swapped, the eviction goroutine from the old table is stopped — no leaked goroutines.

## Response headers

When rate limited, Vrata returns:

```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 1

{"error": "rate limit exceeded"}
```

## Monitoring

`vrata_middleware_rejections_total{type="rateLimit",status_code="429"}` counts rate-limited requests.
