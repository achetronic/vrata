---
title: "HTTP/2"
weight: 4
---

Enable HTTP/2 on a listener for multiplexed connections, header compression, and gRPC support.

## Configuration

```json
{
  "name": "h2-listener",
  "port": 9090,
  "http2": true
}
```

Set `http2: true` on the listener. That's it.

## Examples

### gRPC listener (TLS + HTTP/2)

gRPC requires HTTP/2. With TLS, Go negotiates HTTP/2 via ALPN automatically:

```json
{
  "name": "grpc",
  "port": 443,
  "http2": true,
  "tls": {
    "certPath": "/certs/tls.crt",
    "keyPath": "/certs/tls.key"
  }
}
```

Clients that negotiate `h2` over ALPN get HTTP/2. Clients that negotiate `http/1.1` get HTTP/1.1. Both work on the same port.

### h2c (HTTP/2 cleartext, no TLS)

```json
{
  "name": "h2c",
  "port": 8080,
  "http2": true
}
```

Without TLS, Vrata supports h2c — HTTP/2 over plaintext using the Upgrade mechanism. This is common behind a load balancer that already terminates TLS.

### Mixed listener (HTTP/1.1 + HTTP/2 + TLS)

```json
{
  "name": "mixed",
  "port": 443,
  "http2": true,
  "tls": {
    "certPath": "/certs/tls.crt",
    "keyPath": "/certs/tls.key"
  }
}
```

Both HTTP/1.1 and HTTP/2 clients connect on the same port. ALPN negotiation picks the best protocol for each connection.

### HTTP/1.1 only (default)

```json
{
  "name": "h1-only",
  "port": 8080
}
```

Omit `http2` (or set it to `false`). Only HTTP/1.1 is accepted.

## When to enable HTTP/2

- **gRPC services** — gRPC is built on HTTP/2 and won't work without it
- **High-concurrency APIs** — HTTP/2 multiplexes many requests over a single TCP connection, reducing connection overhead
- **Browser-facing services** — modern browsers negotiate HTTP/2 automatically over TLS

## When to keep HTTP/1.1

- **Simple internal services** — HTTP/1.1 is simpler to debug (plaintext, one request per connection)
- **Proxying to HTTP/1.1 backends** — the listener protocol is independent of the upstream protocol. You can accept HTTP/2 from clients and forward to HTTP/1.1 backends

## HTTP/2 to upstream

The listener's `http2` setting controls the **client ↔ proxy** protocol. To use HTTP/2 between the proxy and the upstream, set `options.http2: true` on the destination:

```json
{
  "name": "grpc-backend",
  "host": "grpc-svc.default.svc.cluster.local",
  "port": 50051,
  "options": {
    "http2": true
  }
}
```
