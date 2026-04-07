---
title: "PROXY Protocol"
weight: 11
---

The `proxyProtocol` field on a Listener enables PROXY protocol (v1 text and v2 binary) parsing. When a load balancer (AWS NLB, HAProxy, etc.) sits in front of Vrata and sends a PROXY protocol header with each TCP connection, Vrata reads it and replaces `r.RemoteAddr` with the real client address — transparently for the entire stack.

## Why PROXY protocol?

When a Layer 4 (TCP) load balancer forwards connections to Vrata, the proxy sees the LB's IP as the client — not the real user. PROXY protocol solves this at the transport layer: the LB prepends a small header with the original client IP before the HTTP data.

Unlike `X-Forwarded-For` (which is an HTTP header that can be spoofed by any client), PROXY protocol is injected by the load balancer at the TCP level and cannot be forged by the application-layer client — as long as you restrict which IPs are allowed to send it.

## Configuration

```json
{
  "name": "behind-nlb",
  "port": 8443,
  "proxyProtocol": {
    "trustedCidrs": ["10.0.0.0/8", "172.16.0.0/12"]
  }
}
```

## Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `trustedCidrs` | string[] | required | CIDR ranges allowed to send PROXY protocol headers |

`trustedCidrs` is **required**. Without it, any client could send a PROXY protocol header and spoof their IP.

## Trust policy

| Connection source | PP header present? | Behaviour |
|---|---|---|
| Trusted CIDR | Yes | **USE** — parse header, apply real client IP |
| Trusted CIDR | No | **USE** — wait for header (required from trusted sources) |
| Untrusted IP | Any | **IGNORE** — treat as plain TCP, no PP parsing |

This means untrusted clients can still connect normally — they just cannot inject a PROXY protocol header to change their address.

## Listener restart

Unlike [`clientIp`]({{< relref "client-ip" >}}) (which is hot-swappable), changing `proxyProtocol` **triggers a listener restart**. PROXY protocol operates at the TCP transport layer — the wrapper goes on the `net.Listener` and cannot be swapped without closing and re-opening the socket.

## Integration with clientIp

PROXY protocol and `clientIp` work together:

1. **PROXY protocol** rewrites `r.RemoteAddr` at the TCP level (real client from LB)
2. **clientIp** resolver runs on top of the rewritten `r.RemoteAddr`

Common combinations:

| Setup | proxyProtocol | clientIp | Result |
|---|---|---|---|
| Direct (no LB) | — | `source: "direct"` | TCP peer = client |
| NLB only | trusted CIDRs | `source: "direct"` | PP gives real client, "direct" uses it |
| NLB + CDN | trusted CIDRs | `source: "xff"` + trusted CIDRs | PP gives CDN IP, XFF walk gives real client |
| NLB + Cloudflare | trusted CIDRs | `source: "header"` + `CF-Connecting-IP` | PP gives CF IP, header gives real client |

## Full example

AWS NLB → Vrata with PROXY protocol, restricting access to internal IPs:

```bash
# Create listener with PROXY protocol + direct client IP
curl -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "behind-nlb",
    "port": 8443,
    "proxyProtocol": {
      "trustedCidrs": ["10.0.0.0/8"]
    },
    "clientIp": {
      "source": "direct"
    }
  }'
```

With this setup, `request.clientIp` in CEL, access logs, and authorization middlewares reflects the real client IP as reported by the NLB via PROXY protocol.
