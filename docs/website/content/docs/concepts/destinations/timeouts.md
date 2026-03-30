---
title: "Destination Timeouts"
weight: 1
---

Every stage of the connection from Vrata to your upstream has a configurable timeout. This prevents hung connections, protects against slow backends, and gives you precise control over failure behaviour.

## Why you need them

Without timeouts, a backend that accepts a TCP connection but never responds will hold the request open forever. Your proxy's connection pool fills up. Users see hanging requests. Circuit breakers never fire because there's no error — just silence.

Destination timeouts turn silence into a clean, fast failure.

## All 7 timeouts

```json
{
  "options": {
    "timeouts": {
      "request": "30s",
      "connect": "5s",
      "dualStackFallback": "300ms",
      "tlsHandshake": "5s",
      "responseHeader": "10s",
      "expectContinue": "1s",
      "idleConnection": "90s"
    }
  }
}
```

### `request` — the total budget

**Default: 30s** · Go field: `Client.Timeout`

The absolute ceiling for the entire HTTP call: connect + TLS + send request + wait + receive response. If the total exceeds this, Vrata returns a structured JSON error with the appropriate status code.

**When to change it:**
- Uploads or streaming responses → increase to minutes
- Health check endpoints → decrease to 2-3s
- If a route also sets `forward.timeouts.request`, the route's value takes precedence

### `connect` — TCP connection

**Default: 5s** · Go field: `Dialer.Timeout`

How long to wait for the TCP handshake. If the backend is down or unreachable, this fires.

**When to change it:**
- Cross-region backends → increase to 10s
- Same-datacenter → decrease to 1-2s

### `dualStackFallback` — IPv4/IPv6 fallback

**Default: 300ms** · Go field: `Dialer.FallbackDelay`

When the destination hostname resolves to both IPv4 and IPv6 addresses, Vrata tries one first. If it doesn't connect within this delay, it tries the other family in parallel (RFC 6555 Happy Eyeballs).

**When to change it:** Almost never. The default 300ms is the RFC recommendation.

### `tlsHandshake` — TLS negotiation

**Default: 5s** · Go field: `Transport.TLSHandshakeTimeout`

How long to complete the TLS handshake after TCP connect succeeds. Covers certificate verification, protocol negotiation, and (for mTLS) client cert presentation.

**When to change it:**
- mTLS with external CA validation → increase
- Internal services with self-signed certs → can decrease

### `responseHeader` — waiting for the upstream to start responding

**Default: 10s** · Go field: `Transport.ResponseHeaderTimeout`

After Vrata sends the request, how long to wait for the upstream to send the first byte of response headers. This catches the "accepted but thinking forever" scenario.

**When to change it:**
- ML inference endpoints → increase to 60s+
- Fast APIs → decrease to 2-3s
- This is often the most important timeout to tune

### `expectContinue` — 100-Continue protocol

**Default: 1s** · Go field: `Transport.ExpectContinueTimeout`

When the request includes `Expect: 100-continue`, how long to wait for the server's `100 Continue` response before sending the body anyway. Rare outside of large upload scenarios.

### `idleConnection` — connection pool hygiene

**Default: 90s** · Go field: `Transport.IdleConnTimeout`

How long a reusable connection stays in the pool without being used before Vrata closes it. Keeps the pool from growing unbounded.

**When to change it:**
- High-traffic destinations → increase (connections are reused quickly)
- Low-traffic destinations → decrease (don't hold connections open for nothing)

## How they interact with route timeouts

The route's `forward.timeouts.request` is a watchdog that wraps everything. The destination timeouts are partial — each protects one step. The most restrictive always wins.

```
forward.timeouts.request (30s) ← route watchdog
  └─ destination.timeouts.request (30s) ← total HTTP call
       ├─ connect (5s)
       ├─ tlsHandshake (5s)
       ├─ responseHeader (10s) ← usually the one that matters
       └─ idleConnection (90s) ← pool management, not per-request
```

If `responseHeader` fires at 10s, the route watchdog at 30s never needs to. If the route sets `forward.timeouts.request: 5s`, it overrides everything — even if `responseHeader` is 10s, the route cuts at 5s.
