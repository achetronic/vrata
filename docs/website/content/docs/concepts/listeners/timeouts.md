---
title: "Listener Timeouts"
weight: 2
---

Control how long the listener waits for clients at each stage of the HTTP connection. These timeouts protect against slow clients, aborted connections, and resource exhaustion attacks.

## Configuration

```json
{
  "timeouts": {
    "clientHeader": "10s",
    "clientRequest": "60s",
    "clientResponse": "60s",
    "idleBetweenRequests": "120s"
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `clientHeader` | string | `10s` | Time for client to send all request headers |
| `clientRequest` | string | `60s` | Time to receive the complete request including body |
| `clientResponse` | string | `60s` | Time to send the complete response to the client |
| `idleBetweenRequests` | string | `120s` | Keep-alive idle time between requests from the same client |

All values are Go duration strings: `5s`, `100ms`, `2m30s`, `1h`.

## Examples

### Default (no changes)

Omit the `timeouts` field entirely. Vrata uses sensible defaults:

```json
{
  "name": "default",
  "port": 8080
}
```

### API server (fast requests, short timeouts)

```json
{
  "name": "api",
  "port": 8080,
  "timeouts": {
    "clientHeader": "5s",
    "clientRequest": "10s",
    "clientResponse": "30s",
    "idleBetweenRequests": "60s"
  }
}
```

API clients send small payloads and expect fast responses. Short timeouts free connections quickly.

### File upload endpoint

```json
{
  "name": "uploads",
  "port": 8081,
  "timeouts": {
    "clientHeader": "10s",
    "clientRequest": "300s",
    "clientResponse": "60s",
    "idleBetweenRequests": "30s"
  }
}
```

Large uploads need a long `clientRequest` timeout. The header timeout stays short — even large uploads send headers quickly.

### Streaming / Server-Sent Events

```json
{
  "name": "streaming",
  "port": 8082,
  "timeouts": {
    "clientHeader": "10s",
    "clientRequest": "30s",
    "clientResponse": "0s",
    "idleBetweenRequests": "300s"
  }
}
```

Set `clientResponse` to `0s` (unlimited) for long-lived streaming responses. The upstream controls when the response ends.

### Aggressive anti-slowloris

```json
{
  "name": "hardened",
  "port": 443,
  "timeouts": {
    "clientHeader": "3s",
    "clientRequest": "15s",
    "clientResponse": "30s",
    "idleBetweenRequests": "30s"
  }
}
```

Short `clientHeader` (3s) drops slowloris attackers quickly. Short `idleBetweenRequests` (30s) reclaims idle connections faster.

## How they work

### clientHeader

Starts when the connection is accepted. Ends when all request headers have been received. If the timer fires, the connection is closed with no response.

**Protects against:** Slowloris attacks where a client sends headers one byte at a time to exhaust connections.

### clientRequest

Starts when the connection is accepted. Ends when the complete request (headers + body) has been received. This is a superset of `clientHeader`.

**Protects against:** Slow uploads that tie up connections. If an upload stalls, this timeout closes the connection.

### clientResponse

Starts when Vrata begins writing the response. Ends when the complete response has been sent to the client. If the client reads slowly, this timeout fires.

**Protects against:** Slow-read attacks where a client accepts data one byte at a time, tying up the goroutine.

### idleBetweenRequests

On keep-alive connections, this is the idle time between the end of one request/response cycle and the start of the next. When it fires, the connection is closed gracefully.

**Protects against:** Connection pool exhaustion from clients that open connections but don't send requests.

## Relationship with destination timeouts

Listener timeouts operate on the **client ↔ proxy** side. Destination timeouts operate on the **proxy ↔ upstream** side. They are independent:

```
Client ──[listener timeouts]──► Proxy ──[destination timeouts]──► Backend
```

A common mistake is setting `clientResponse` shorter than the upstream's response time. If your backend takes 10s to respond but `clientResponse` is 5s, the client connection closes before the response arrives.
