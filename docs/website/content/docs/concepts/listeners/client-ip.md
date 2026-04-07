---
title: "Client IP Resolution"
weight: 10
---

The `clientIp` field on a Listener configures how the proxy determines the real client IP from incoming requests. The resolved IP is stored in the request context **before route matching**, making it available to:

- CEL route matching (`request.clientIp` in `match.cel`)
- Inline authorization (`request.clientIp` in `inlineAuthz` rules)
- Access logging (`${request.clientIp}`)
- Any middleware that reads the context

Without this configuration, `request.clientIp` trusts the leftmost `X-Forwarded-For` entry unconditionally — which any client can spoof.

## Hot-reload

Changes to `clientIp` are hot-reloaded via atomic pointer swap — the listener's TCP server is **not** restarted. Changing trusted CIDRs or switching strategies takes effect immediately on the next request after snapshot activation.

## Configuration

```json
{
  "name": "my-listener",
  "port": 8443,
  "clientIp": {
    "source": "xff",
    "trustedCidrs": ["10.0.0.0/8", "172.16.0.0/12"]
  }
}
```

## Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `source` | string | required | Resolution strategy: `"direct"`, `"xff"`, or `"header"` |
| `trustedCidrs` | string[] | `[]` | CIDR ranges to skip when walking XFF (only with `source: "xff"`) |
| `numTrustedHops` | int | `0` | Number of rightmost XFF entries to skip (only with `source: "xff"`) |
| `header` | string | — | Header name to read (only with `source: "header"`) |

## Source strategies

### `direct`

Always uses the TCP peer address (`r.RemoteAddr`). Ignores `X-Forwarded-For` completely.

**Use when**: the proxy is directly exposed to clients with no reverse proxy in front.

```json
{ "source": "direct" }
```

### `xff`

Walks the `X-Forwarded-For` header from right to left, skipping trusted entries. The first untrusted entry is the client IP. Two modes (mutually exclusive):

- **`trustedCidrs`**: skip entries whose IP falls within any listed CIDR range
- **`numTrustedHops`**: skip the N rightmost entries

When neither is set, uses the leftmost (first) entry — the legacy unsafe behaviour.

#### Example: trusted CIDRs

```json
{
  "source": "xff",
  "trustedCidrs": ["10.0.0.0/8", "172.16.0.0/12"]
}
```

With `X-Forwarded-For: 203.0.113.50, 10.0.0.1`:
- `10.0.0.1` → trusted → skip
- `203.0.113.50` → not trusted → **client IP**

#### Example: trusted hops

```json
{
  "source": "xff",
  "numTrustedHops": 2
}
```

With `X-Forwarded-For: 203.0.113.50, 10.0.0.5, 10.0.0.1`:
- Skip 2 rightmost → `203.0.113.50` is the **client IP**

### `header`

Reads the client IP from a specific request header. Falls back to the TCP peer address if the header is absent.

**Use when**: a trusted load balancer injects a single-value header like `X-Real-IP` or `CF-Connecting-IP`.

```json
{
  "source": "header",
  "header": "CF-Connecting-IP"
}
```

## Full example

A listener behind Cloudflare that resolves the client IP from `CF-Connecting-IP` and uses inline authorization to restrict access:

```bash
# 1. Create the listener with client IP resolution
curl -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "public",
    "port": 8443,
    "clientIp": {
      "source": "header",
      "header": "CF-Connecting-IP"
    }
  }'

# 2. Create an inlineAuthz middleware that checks the resolved IP
curl -X POST localhost:8080/api/v1/middlewares \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "internal-only",
    "type": "inlineAuthz",
    "inlineAuthz": {
      "rules": [
        {"cel": "request.clientIp.startsWith(\"10.\")", "action": "allow"},
        {"cel": "request.clientIp.startsWith(\"192.168.\")", "action": "allow"}
      ],
      "defaultAction": "deny"
    }
  }'

# 3. Use in a route — request.clientIp is available everywhere
curl -X POST localhost:8080/api/v1/routes \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "internal-api",
    "match": {
      "pathPrefix": "/internal",
      "cel": "request.clientIp.startsWith(\"10.\")"
    },
    "directResponse": {"status": 200, "body": "ok"},
    "middlewareIds": ["<internal-only-id>"]
  }'
```

> **Note**: The rate limiter middleware has its own `trustedProxies` field that is independent of the listener's `clientIp` configuration. This is intentional — they may need different trust policies.
