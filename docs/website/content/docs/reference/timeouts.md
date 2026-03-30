---
title: "Timeouts Reference"
weight: 3
---

Vrata has timeouts at three levels. All use semantic names and Go duration strings (`5s`, `100ms`, `2m30s`).

## Listener timeouts

How long the listener waits for the **client**. Configured on the listener entity via the API.

| Field | Go field | Default | What happens when it fires |
|-------|----------|---------|---------------------------|
| `clientHeader` | `ReadHeaderTimeout` | `10s` | Client too slow sending headers → connection closed |
| `clientRequest` | `ReadTimeout` | `60s` | Client too slow sending body → connection closed |
| `clientResponse` | `WriteTimeout` | `60s` | Response too slow to write → connection closed |
| `idleBetweenRequests` | `IdleTimeout` | `120s` | No new request on keep-alive → connection closed |

## Destination timeouts

How long each stage of the **upstream connection** takes. Configured on the destination entity via the API.

| Field | Go field | Default | What happens when it fires |
|-------|----------|---------|---------------------------|
| `request` | `Client.Timeout` | `30s` | Total call exceeded → structured error (502) |
| `connect` | `Dialer.Timeout` | `5s` | TCP connect failed → connection_refused |
| `dualStackFallback` | `Dialer.FallbackDelay` | `300ms` | Try other IP family (IPv4↔IPv6) |
| `tlsHandshake` | `TLSHandshakeTimeout` | `5s` | TLS failed → tls_handshake_failure |
| `responseHeader` | `ResponseHeaderTimeout` | `10s` | Upstream accepted but won't respond → timeout |
| `expectContinue` | `ExpectContinueTimeout` | `1s` | No 100-Continue → send body anyway |
| `idleConnection` | `IdleConnTimeout` | `90s` | Idle pool connection → closed |

## Route timeouts

The outermost **watchdog** — if the total time exceeds this, Vrata cuts the request. Configured on the route entity via the API.

| Field | Default | Description |
|-------|---------|-------------|
| `forward.timeouts.request` | — | Total request deadline (wraps everything) |

If the route doesn't set this, Vrata falls back to `destination.options.timeouts.request`. The most restrictive always wins.

## Middleware timeouts

| Middleware | Field | Default | Description |
|-----------|-------|---------|-------------|
| ExtAuthz | `decisionTimeout` | `5s` | Total time for auth decision |
| ExtProc | `phaseTimeout` | `200ms` | Time per processing phase |
| JWT | `jwksRetrievalTimeout` | `10s` | Time to download JWKS |

## How they interact

```
Client ──── Listener ──── Vrata ──── Destination ──── Upstream

  clientHeader ──────────┐
  clientRequest ─────────┤ listener
  clientResponse ────────┤
  idleBetweenRequests ───┘
                              │
                       forward.timeouts.request (route watchdog)
                              │
                              ├─ request ──────────────┐
                              ├─ connect               │
                              ├─ tlsHandshake          ├─ destination
                              ├─ responseHeader        │
                              └─ idleConnection ───────┘
```

The most restrictive timeout always wins. A `responseHeader: 10s` on the destination fires before `forward.timeouts.request: 30s` on the route if the upstream takes 11 seconds to start responding.
