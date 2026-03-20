---
title: "TLS to Upstream"
weight: 6
---

By default, Vrata connects to backends in plaintext. Enable TLS when your upstream requires encrypted connections — for external APIs, compliance requirements, or zero-trust architectures.

## Configuration

```json
{
  "options": {
    "tls": {
      "mode": "tls",
      "sni": "api.internal",
      "caFile": "/certs/ca.pem",
      "minVersion": "TLSv1_2",
      "maxVersion": "TLSv1_3"
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | string | `none` | `none` (plaintext), `tls` (verify server), `mtls` (mutual TLS) |
| `sni` | string | destination host | Server Name Indication override |
| `caFile` | string | system CA bundle | Path to CA certificate PEM for verification |
| `certFile` | string | — | Client certificate PEM (required for `mtls`) |
| `keyFile` | string | — | Client private key PEM (required for `mtls`) |
| `minVersion` | string | — | Minimum TLS version: `TLSv1_0` to `TLSv1_3` |
| `maxVersion` | string | — | Maximum TLS version |

## Examples

### Plaintext (default)

```json
{
  "name": "internal-api",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80
}
```

No `tls` option — plaintext HTTP. This is the default.

### TLS (verify server certificate)

```json
{
  "name": "external-api",
  "host": "api.partner.com",
  "port": 443,
  "options": {
    "tls": {
      "mode": "tls"
    }
  }
}
```

Vrata verifies the backend's certificate against the system CA bundle. Use for any HTTPS backend with a valid certificate.

### TLS with custom CA

```json
{
  "name": "internal-secure",
  "host": "payments.internal",
  "port": 443,
  "options": {
    "tls": {
      "mode": "tls",
      "caFile": "/certs/internal-ca.pem"
    }
  }
}
```

For internal services with certificates signed by a private CA.

### TLS with SNI override

```json
{
  "name": "shared-host",
  "host": "10.0.1.50",
  "port": 443,
  "options": {
    "tls": {
      "mode": "tls",
      "sni": "api.example.com"
    }
  }
}
```

When connecting to an IP address or a hostname that doesn't match the certificate's CN/SAN, set `sni` to the expected server name.

### Mutual TLS (mTLS)

```json
{
  "name": "zero-trust",
  "host": "secure-svc.default.svc.cluster.local",
  "port": 443,
  "options": {
    "tls": {
      "mode": "mtls",
      "certFile": "/certs/client.pem",
      "keyFile": "/certs/client-key.pem",
      "caFile": "/certs/ca.pem"
    }
  }
}
```

Both sides verify each other. Vrata presents `certFile`/`keyFile` to the backend. The backend presents its certificate, which Vrata verifies against `caFile`. Use for zero-trust service mesh patterns.

### TLS 1.3 only

```json
{
  "options": {
    "tls": {
      "mode": "tls",
      "minVersion": "TLSv1_3",
      "maxVersion": "TLSv1_3"
    }
  }
}
```

Force the upstream connection to use TLS 1.3. Fails if the backend doesn't support it.

## Modes summary

| Mode | Encrypts traffic | Verifies server | Presents client cert |
|------|-----------------|-----------------|---------------------|
| `none` | No | No | No |
| `tls` | Yes | Yes | No |
| `mtls` | Yes | Yes | Yes |

## Interaction with Kubernetes discovery

When using Kubernetes discovery with TLS, Vrata connects to each pod IP with TLS. The `sni` field is important here — pod IPs don't have DNS names that match certificates:

```json
{
  "name": "secure-service",
  "host": "secure-svc.default.svc.cluster.local",
  "port": 443,
  "options": {
    "discovery": {"type": "kubernetes"},
    "tls": {
      "mode": "tls",
      "sni": "secure-svc.default.svc.cluster.local"
    }
  }
}
```
