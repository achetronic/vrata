---
title: "Client Certificates (mTLS)"
weight: 7
---

A TLS listener can require or request client certificates for mutual TLS authentication. The client proves its identity with an X.509 certificate, and Vrata verifies it against a trusted CA.

## Configuration

Add `clientAuth` inside the listener's `tls` block:

```json
{
  "name": "mtls-listener",
  "port": 8443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key",
    "clientAuth": {
      "mode": "require",
      "ca": "/certs/trusted-clients-ca.pem"
    }
  }
}
```

## Modes

| Mode | Behavior |
|------|----------|
| `none` | Don't request a client certificate. Same as not setting `clientAuth` at all. |
| `optional` | Request a certificate but don't reject if the client doesn't send one. Useful for mixed traffic where some clients have certs and some don't. |
| `require` | Reject the TLS handshake if the client doesn't present a valid certificate signed by the trusted CA. |

`ca` is required when mode is `optional` or `require`. It points to a PEM file with one or more CA certificates used to verify client certs.

## Client certificate data in CEL

When a client presents a valid certificate, its metadata is available in CEL expressions — in route matching, `skipWhen`/`onlyWhen` conditions, and `inlineAuthz` rules:

| Variable | Type | Description |
|----------|------|-------------|
| `request.tls.peerCertificate.uris` | list(string) | URI SANs — SPIFFE IDs are URI SANs with `spiffe://` scheme |
| `request.tls.peerCertificate.dnsNames` | list(string) | DNS SANs |
| `request.tls.peerCertificate.subject` | string | Distinguished Name (e.g. `CN=agent-a,O=myorg`) |
| `request.tls.peerCertificate.serial` | string | Serial number in hex |

All fields are empty when no client cert is presented. Always guard with `has()`:

```
has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "spiffe://cluster.local/ns/default/sa/frontend")
```

## X-Forwarded-Client-Cert header

When a client certificate is verified, Vrata automatically injects the `X-Forwarded-Client-Cert` (XFCC) header with the certificate's URI SANs (semicolon-separated). This header is forwarded to the upstream so backends can see the caller's identity without parsing TLS themselves.

Any incoming XFCC header from the client is stripped before injection to prevent spoofing.

Example header value:
```
X-Forwarded-Client-Cert: spiffe://cluster.local/ns/default/sa/agent-a
```

Multiple URIs:
```
X-Forwarded-Client-Cert: spiffe://cluster.local/ns/default/sa/agent-a;https://example.com/id/123
```

## Examples

### Require mTLS for all clients

```json
{
  "name": "internal",
  "port": 8443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key",
    "clientAuth": {
      "mode": "require",
      "ca": "/certs/ca.pem"
    }
  }
}
```

Clients without a valid certificate can't connect at all.

### Optional mTLS with per-route authorization

```json
{
  "name": "mixed",
  "port": 443,
  "tls": {
    "cert": "/certs/tls.crt",
    "key": "/certs/tls.key",
    "clientAuth": {
      "mode": "optional",
      "ca": "/certs/ca.pem"
    }
  }
}
```

All clients can connect. Then use an `inlineAuthz` middleware on sensitive routes to check the cert:

```json
{
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      { "cel": "has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == \"spiffe://cluster.local/ns/default/sa/trusted\")", "action": "allow" }
    ],
    "defaultAction": "deny"
  }
}
```

### SPIFFE-based service-to-service auth

In a Kubernetes cluster with SPIFFE (e.g. SPIRE), each pod gets a certificate with a URI SAN like `spiffe://cluster.local/ns/<namespace>/sa/<service-account>`. Combine `require` mode with `inlineAuthz` to restrict which services can call which endpoints.

## Validation

The API rejects invalid configurations at creation time:

- `mode` must be `none`, `optional`, or `require`
- `ca` is required when mode is `optional` or `require`
- Unknown mode values return 400
