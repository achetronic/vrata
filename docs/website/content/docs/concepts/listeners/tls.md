---
title: "Listener TLS"
weight: 1
---

A listener can terminate TLS so clients connect over HTTPS. Vrata handles the certificate and key; backends receive plaintext HTTP.

## Configuration

```json
{
  "name": "secure",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key",
    "minVersion": "TLSv1_2",
    "maxVersion": "TLSv1_3"
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cert` | string | required | PEM-encoded TLS certificate, file path, or `{{secret:...}}` reference |
| `key` | string | required | PEM-encoded private key, file path, or `{{secret:...}}` reference |
| `minVersion` | string | `TLSv1_2` | Minimum TLS protocol version |
| `maxVersion` | string | — | Maximum TLS protocol version (empty = no upper bound) |

Supported version values: `TLSv1_0`, `TLSv1_1`, `TLSv1_2`, `TLSv1_3`.

## Examples

### Basic HTTPS listener

```json
{
  "name": "public",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key"
  }
}
```

Accepts HTTPS with TLS 1.2+ (the default). Most common setup.

### Strict TLS 1.3 only

```json
{
  "name": "strict",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key",
    "minVersion": "TLSv1_3",
    "maxVersion": "TLSv1_3"
  }
}
```

Rejects clients that don't support TLS 1.3. Use for internal services where you control both sides.

### HTTPS with HTTP/2

```json
{
  "name": "h2",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key"
  },
  "http2": true
}
```

With TLS + HTTP/2, Go negotiates the protocol via ALPN. Clients that support HTTP/2 use it; others fall back to HTTP/1.1 automatically.

### Let's Encrypt / cert-manager certificates

In Kubernetes with cert-manager, the certificate is stored in a Secret and mounted as a volume:

```yaml
volumeMounts:
  - name: tls
    mountPath: /certs
    readOnly: true
volumes:
  - name: tls
    secret:
      secretName: vrata-tls
```

Then configure the listener with the mounted paths:

```json
{
  "name": "public",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key"
  }
}
```

### Legacy compatibility (TLS 1.0+)

```json
{
  "name": "legacy",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key",
    "minVersion": "TLSv1_0"
  }
}
```

Only use this for legacy clients that can't be upgraded. TLS 1.0 and 1.1 are deprecated.

## When to use TLS

- **Public-facing traffic** — always terminate TLS at the proxy
- **Internal services with compliance requirements** — PCI-DSS, SOC2, HIPAA require encryption in transit
- **gRPC** — gRPC over TLS uses HTTP/2 ALPN negotiation, which requires `tls` + `http2: true`

## When to skip TLS

- **Behind a load balancer that terminates TLS** — AWS ALB, GCP GCLB, or an Ingress controller already handles TLS. The listener runs plaintext.
- **Service mesh** — if you have mTLS between all pods via Istio/Linkerd, the proxy doesn't need its own TLS termination.
- **Development** — `http://localhost` is fine for local testing.

Omit the `tls` field entirely to create a plaintext listener.
