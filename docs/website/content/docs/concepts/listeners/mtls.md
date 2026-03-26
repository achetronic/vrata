---
title: "Mutual TLS (mTLS)"
weight: 2
---

A listener can require client certificates for mutual TLS authentication. The client proves its identity with a certificate signed by a trusted CA. Certificate metadata is available in CEL expressions for authorization decisions.

## Configuration

```json
{
  "name": "mtls-gateway",
  "port": 8443,
  "tls": {
    "certPath": "/certs/server.crt",
    "keyPath": "/certs/server.key",
    "clientAuth": {
      "mode": "require",
      "caFile": "/certs/trusted-clients-ca.pem"
    }
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `clientAuth.mode` | string | `none` | `none`, `optional`, or `require` |
| `clientAuth.caFile` | string | required when mode is not `none` | Path to PEM-encoded CA bundle for verifying client certs |

## Modes

| Mode | TLS handshake | Use case |
|------|---------------|----------|
| `none` | Client cert not requested | Default — same as omitting `clientAuth` |
| `optional` | Client cert requested but not required | Mixed traffic: some clients have certs, others don't |
| `require` | Connection rejected if no valid cert | Zero-trust: every client must present a cert |

## CEL variables

When a client cert is presented and verified, its metadata is available in all CEL expressions (route matching, `skipWhen`/`onlyWhen`, `inlineAuthz` rules):

| Variable | Type | Description |
|----------|------|-------------|
| `request.tls.peerCertificate.uris` | list(string) | URI SANs from the client certificate |
| `request.tls.peerCertificate.dnsNames` | list(string) | DNS SANs |
| `request.tls.peerCertificate.subject` | string | Distinguished Name |
| `request.tls.peerCertificate.serial` | string | Serial number (hex) |

All fields are empty lists/strings when no client cert is presented.

Use `has(request.tls)` to check if TLS cert info exists before accessing nested fields.

## Examples

### Require client cert (zero-trust)

```json
{
  "name": "internal",
  "port": 8443,
  "tls": {
    "certPath": "/certs/server.crt",
    "keyPath": "/certs/server.key",
    "clientAuth": {
      "mode": "require",
      "caFile": "/certs/internal-ca.pem"
    }
  }
}
```

Clients without a valid cert signed by `internal-ca.pem` are rejected at the TLS handshake.

### Optional client cert (mixed traffic)

```json
{
  "name": "mixed",
  "port": 443,
  "tls": {
    "certPath": "/certs/server.crt",
    "keyPath": "/certs/server.key",
    "clientAuth": {
      "mode": "optional",
      "caFile": "/certs/trusted-ca.pem"
    }
  }
}
```

Clients with a valid cert get their identity available in CEL. Clients without a cert connect normally — authorization is decided per-route.

### URI SAN matching in CEL

With mTLS enabled, you can match the client's URI SANs in route matching or authorization:

```
request.tls.peerCertificate.uris.exists(u, u == "https://idp.example.com/clients/my-service")
```

URI SANs are standard X.509 certificate fields. The proxy extracts all URI SANs generically — it has no knowledge of any specific identity framework.

### DNS SAN matching

```
request.tls.peerCertificate.dnsNames.exists(d, d == "service.internal.example.com")
```

### Subject matching

```
request.tls.peerCertificate.subject.contains("O=MyOrg")
```

### SPIFFE identity example

[SPIFFE](https://spiffe.io/) IDs are URI SANs with the `spiffe://` scheme. If your infrastructure issues SPIFFE certificates (via SPIRE, Istio, or cert-manager-csi-driver), you can match them directly:

```
request.tls.peerCertificate.uris.exists(u, u == "spiffe://cluster.local/ns/default/sa/payment-service")
```

The proxy doesn't know or care about SPIFFE — it's just another URI SAN.

## X-Forwarded-Client-Cert header

When a client cert is presented, Vrata automatically injects the `X-Forwarded-Client-Cert` header with the cert's URI SANs (semicolon-separated). This header is available for downstream services and `extAuthz` via `onCheck.forwardHeaders`.

Any incoming `X-Forwarded-Client-Cert` header from the client is stripped before injection to prevent spoofing.
