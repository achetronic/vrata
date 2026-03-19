---
title: "External Processor"
weight: 5
---

Send HTTP request/response phases to an external service that can inspect, mutate, or reject them. The most powerful middleware — your service sees every header and every body byte, with full control over what happens next.

## Configuration

```json
{
  "name": "waf",
  "type": "extProc",
  "extProc": {
    "destinationId": "<waf-service>",
    "mode": "grpc",
    "phaseTimeout": "200ms",
    "allowOnError": false,
    "statusOnError": 500,
    "disableReject": false,
    "phases": {
      "requestHeaders": "send",
      "responseHeaders": "send",
      "requestBody": "buffered",
      "responseBody": "none",
      "maxBodyBytes": 1048576
    },
    "allowedMutations": {
      "allowHeaders": ["x-custom-*"],
      "denyHeaders": ["authorization"]
    },
    "forwardRules": {
      "allowHeaders": ["content-type", "x-request-id"],
      "denyHeaders": ["cookie"]
    },
    "observeMode": {
      "enabled": false,
      "workers": 64,
      "queueSize": 4096
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `destinationId` | string | required | Destination hosting the processor |
| `mode` | string | `grpc` | `grpc` or `http` |
| `phaseTimeout` | string | `200ms` | Max time per phase |
| `allowOnError` | bool | `false` | Allow requests if processor fails |
| `statusOnError` | number | `500` | HTTP status when processor errors (if `allowOnError: false`) |
| `disableReject` | bool | `false` | Prevent processor from rejecting requests |
| `phases` | object | — | Which phases to send and how |
| `phases.requestHeaders` | string | `skip` | `send` or `skip` |
| `phases.responseHeaders` | string | `skip` | `send` or `skip` |
| `phases.requestBody` | string | `none` | `none`, `buffered`, `bufferedPartial`, `streamed` |
| `phases.responseBody` | string | `none` | `none`, `buffered`, `bufferedPartial`, `streamed` |
| `phases.maxBodyBytes` | number | — | Max bytes to buffer for `bufferedPartial` |
| `allowedMutations` | object | — | Restrict which headers the processor can modify |
| `forwardRules` | object | — | Restrict which request headers are sent to the processor |
| `observeMode` | object | — | Fire-and-forget async processing |
| `metricsPrefix` | string | — | Custom prefix for this processor's metrics |

## Body modes

| Mode | Description |
|------|-------------|
| `none` | Body not sent to processor |
| `buffered` | Entire body buffered in memory, sent as one message |
| `bufferedPartial` | Buffer up to `maxBodyBytes`, send whatever was buffered (may be truncated) |
| `streamed` | Body chunks sent as they arrive, processor responds per-chunk |

## Examples

### WAF (request headers + body inspection)

```json
{
  "name": "waf",
  "type": "extProc",
  "extProc": {
    "destinationId": "<waf-service>",
    "mode": "grpc",
    "phaseTimeout": "100ms",
    "phases": {
      "requestHeaders": "send",
      "requestBody": "buffered",
      "responseHeaders": "skip",
      "responseBody": "none"
    }
  }
}
```

Inspects request headers and body for malicious payloads. Skips response phases (WAF only cares about incoming traffic).

### Response header injection

```json
{
  "name": "response-enricher",
  "type": "extProc",
  "extProc": {
    "destinationId": "<enricher-service>",
    "phaseTimeout": "50ms",
    "phases": {
      "requestHeaders": "skip",
      "responseHeaders": "send",
      "requestBody": "none",
      "responseBody": "none"
    },
    "allowedMutations": {
      "allowHeaders": ["x-custom-*", "cache-control"]
    }
  }
}
```

Only processes response headers. The processor can add or modify `x-custom-*` and `cache-control` headers but nothing else.

### Audit logger (observe mode)

```json
{
  "name": "audit-log",
  "type": "extProc",
  "extProc": {
    "destinationId": "<audit-service>",
    "phases": {
      "requestHeaders": "send",
      "responseHeaders": "send",
      "requestBody": "bufferedPartial",
      "responseBody": "none",
      "maxBodyBytes": 65536
    },
    "observeMode": {
      "enabled": true,
      "workers": 64,
      "queueSize": 4096
    }
  }
}
```

Fire-and-forget: phases are queued and processed by a background worker pool. The request is never blocked. Useful for audit logging, analytics, or compliance recording.

### Request body transformation

```json
{
  "name": "body-transform",
  "type": "extProc",
  "extProc": {
    "destinationId": "<transform-service>",
    "mode": "grpc",
    "phaseTimeout": "500ms",
    "phases": {
      "requestHeaders": "send",
      "requestBody": "buffered",
      "responseHeaders": "skip",
      "responseBody": "none"
    }
  }
}
```

The processor receives the full request body and can replace it before forwarding to upstream.

### HTTP mode (simpler, less powerful)

```json
{
  "name": "simple-proc",
  "type": "extProc",
  "extProc": {
    "destinationId": "<processor>",
    "mode": "http",
    "phaseTimeout": "200ms",
    "phases": {
      "requestHeaders": "send",
      "responseHeaders": "skip"
    }
  }
}
```

In HTTP mode, Vrata sends one POST per phase to `<destination>/process`. Simpler to implement than gRPC but limited to one message per phase (no streaming).

### Security: restrict mutations

```json
{
  "allowedMutations": {
    "allowHeaders": ["x-enriched-*"],
    "denyHeaders": ["authorization", "cookie", "host"]
  },
  "forwardRules": {
    "allowHeaders": ["content-type", "x-request-id", "accept"],
    "denyHeaders": ["cookie", "authorization"]
  }
}
```

- `allowedMutations` — the processor can only modify headers matching `x-enriched-*`. It cannot touch `authorization`, `cookie`, or `host`.
- `forwardRules` — only `content-type`, `x-request-id`, and `accept` are sent to the processor. Sensitive headers are withheld.

### Prevent rejection (logging-only processor)

```json
{
  "disableReject": true,
  "allowOnError": true
}
```

The processor can inspect and mutate, but cannot reject requests. If the processor fails, the request continues. Useful for non-critical enrichment services.

## Processor responses

The processor can return one of:

- **Continue** — let the request/response pass, with optional header mutations and body replacements
- **Replace body** — substitute the entire request or response body
- **Reject** — return an error to the client (unless `disableReject: true`)

## Monitoring

`vrata_middleware_duration_seconds{type="extProc"}` tracks per-phase processing latency. A spike means your processor is slowing down.
