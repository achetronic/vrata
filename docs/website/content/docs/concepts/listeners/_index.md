---
title: "Listeners"
weight: 1
---

A Listener is the entry point to Vrata's proxy. It opens a TCP port, accepts HTTP connections from clients, and feeds them into the routing engine. Without at least one listener, the proxy does nothing.

## What a listener does

1. **Binds a port** — opens a TCP socket on `address:port`
2. **Terminates TLS** — optionally handles HTTPS so backends receive plaintext
3. **Negotiates protocol** — HTTP/1.1 by default, HTTP/2 if enabled (required for gRPC)
4. **Enforces limits** — max header size, connection timeouts, idle timeouts
5. **Resolves the real client IP** — determines the true client IP from XFF, a custom header, or the direct connection — before route matching
6. **Parses PROXY protocol** — optionally reads PROXY protocol v1/v2 headers from load balancers to recover the real client address at the TCP level
7. **Serves metrics** — optionally exposes a Prometheus scrape endpoint on this listener's port
8. **Formats proxy errors** — controls how much detail Vrata includes in its own error responses

Each listener is independent. You can run multiple listeners on different ports with different configurations — for example one for public HTTPS and another for internal plaintext.

## Creating a listener

```bash
curl -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "public",
    "port": 443
  }'
```

## Minimal listener

The only required fields are `name` and `port`:

```json
{
  "name": "internal",
  "port": 3000
}
```

This creates a plaintext HTTP/1.1 listener on `0.0.0.0:3000` with default timeouts and no metrics.

## Structure

```json
{
  "name": "public",
  "address": "0.0.0.0",
  "port": 443,
  "tls": { ... },
  "http2": true,
  "serverName": "vrata",
  "maxRequestHeadersKB": 64,
  "timeouts": { ... },
  "metrics": { ... },
  "proxyErrors": { "detail": "standard" },
  "clientIp": { "source": "xff", "trustedCidrs": ["10.0.0.0/8"] }
}
```

## All fields

| Field | Type | Default | Description | Details |
|-------|------|---------|-------------|---------|
| `name` | string | required | Human-readable label, must be unique | — |
| `address` | string | `0.0.0.0` | Bind address | — |
| `port` | number | required | TCP port | — |
| `tls` | object | — | TLS termination config | [Listener TLS]({{< relref "tls" >}}) |
| `http2` | bool | `false` | Enable HTTP/2 (required for gRPC) | [HTTP/2]({{< relref "http2" >}}) |
| `serverName` | string | — | Value for the `Server` response header | — |
| `maxRequestHeadersKB` | number | `0` | Max request header size in KB (0 = Go default ~1MB) | — |
| `timeouts` | object | — | Client connection timeouts | [Listener Timeouts]({{< relref "timeouts" >}}) |
| `metrics` | object | — | Prometheus metrics config | [Listener Metrics]({{< relref "metrics" >}}) |
| `proxyErrors` | object | — | Proxy error response format | [Proxy Error Responses]({{< relref "proxy-errors" >}}) |
| `clientIp` | object | — | Real client IP resolution (hot-reloadable) | [Client IP Resolution]({{< relref "client-ip" >}}) |
| `proxyProtocol` | object | — | PROXY protocol v1/v2 support | [PROXY Protocol]({{< relref "proxy-protocol" >}}) |

## Multiple listeners

Routes are matched across all listeners — Vrata doesn't bind routes to specific listeners. A common pattern is two listeners — one public and one internal — and using hostname matching in your routes to differentiate traffic.

## Lifecycle

Listeners are created and updated via the API, but changes only take effect when a snapshot is activated. You can add, modify, or remove listeners safely — nothing changes in the running proxy until you explicitly activate.

## What triggers a restart?

Some listener fields require restarting the TCP server (brief connection drain). Others are hot-swapped atomically with zero disruption.

| Field | On change | Why |
|-------|-----------|-----|
| `address` | **Restart** | Changes which network interface the port binds to |
| `port` | **Restart** | Changes the TCP port — requires closing and reopening the socket |
| `tls` | **Restart** | Certificates and TLS versions are locked at server startup |
| `http2` | **Restart** | Enabling/disabling HTTP/2 changes how the connection is negotiated |
| `timeouts` | **Restart** | Connection timeouts are locked at server startup |
| `metrics` | **Restart** | Creates a new metrics registry and scrape endpoint |
| `proxyProtocol` | **Restart** | Wraps the TCP socket — cannot be added/removed on a live connection |
| `serverName` | **Hot-swap** | Applied per-request, no connection state involved |
| `maxRequestHeadersKB` | **Hot-swap** | Checked per-request, no connection state involved |
| `proxyErrors` | **Hot-swap** | Applied per-request, no connection state involved |
| `clientIp` | **Hot-swap** | Resolved per-request, no connection state involved |
