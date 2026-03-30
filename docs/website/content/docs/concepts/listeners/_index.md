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
5. **Serves metrics** — optionally exposes a Prometheus scrape endpoint on this listener's port
6. **Formats proxy errors** — controls how much detail Vrata includes in its own error responses

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
  "proxyErrors": { "detail": "standard" }
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

## Multiple listeners

Routes are matched across all listeners — Vrata doesn't bind routes to specific listeners. A common pattern is two listeners — one public and one internal — and using hostname matching in your routes to differentiate traffic.

## Lifecycle

Listeners are created and updated via the API, but changes only take effect when a snapshot is activated. You can add, modify, or remove listeners safely — nothing changes in the running proxy until you explicitly activate.
