---
title: "Kubernetes Discovery"
weight: 7
---

Vrata auto-discovers pod IPs for destinations backed by Kubernetes Services. When pods scale up, down, or restart, the endpoint list updates automatically — no manual configuration needed.

## Configuration

```json
{
  "name": "api",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80,
  "options": {
    "discovery": {"type": "kubernetes"}
  }
}
```

Set `discovery.type` to `"kubernetes"` and provide the Service's FQDN as the `host`.

## All fields

| Field | Type | Values | Description |
|-------|------|--------|-------------|
| `discovery.type` | string | `kubernetes` | Enable Kubernetes EndpointSlice watching |

## Examples

### Basic discovery

```json
{
  "name": "api",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80,
  "options": {
    "discovery": {"type": "kubernetes"}
  }
}
```

Vrata parses the FQDN to extract `namespace: default`, `service: api-svc`, then watches EndpointSlices for that Service. When a pod starts or stops, the endpoint list updates within seconds.

### Discovery with load balancing

```json
{
  "name": "api",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80,
  "options": {
    "discovery": {"type": "kubernetes"},
    "endpointBalancing": {
      "algorithm": "LEAST_REQUEST",
      "leastRequest": {"choiceCount": 2}
    }
  }
}
```

Discovered pods are load-balanced using least-request. As pods scale, the balancing pool adjusts automatically.

### Discovery with health checks

```json
{
  "name": "api",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80,
  "options": {
    "discovery": {"type": "kubernetes"},
    "healthCheck": {
      "path": "/health",
      "interval": "10s",
      "unhealthyThreshold": 3
    }
  }
}
```

Kubernetes readiness probes tell the kubelet when a pod is ready. Vrata's health checks add a second layer — verifying from the proxy's perspective that the pod is actually serving requests.

### Discovery with TLS

```json
{
  "name": "secure-api",
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

Connects to discovered pod IPs over TLS. The `sni` field tells the backend which certificate to present — pod IPs don't have DNS names.

### Different namespaces

```json
{
  "name": "payments",
  "host": "payments-svc.payments.svc.cluster.local",
  "port": 80,
  "options": {
    "discovery": {"type": "kubernetes"}
  }
}
```

The namespace is extracted from the FQDN. The proxy needs RBAC permissions to list EndpointSlices in that namespace.

## ExternalName Services

Kubernetes Services of type `ExternalName` resolve to `spec.externalName`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: external-api
  namespace: default
spec:
  type: ExternalName
  externalName: api.partner.com
```

With discovery enabled, Vrata watches the Service object and uses `api.partner.com` as the sole endpoint. If `externalName` changes, the endpoint updates automatically.

## Without Kubernetes

If Vrata runs outside of Kubernetes (bare metal, Docker, local development), discovery is silently disabled. Vrata connects directly to `host:port`. No error, no crash.

## Static endpoints as alternative

If you don't want discovery, provide endpoints explicitly:

```json
{
  "name": "api",
  "host": "api-svc.default.svc.cluster.local",
  "port": 80,
  "endpoints": [
    {"host": "10.0.1.10", "port": 8080},
    {"host": "10.0.1.11", "port": 8080},
    {"host": "10.0.1.12", "port": 8080}
  ]
}
```

Static endpoints are fixed — you're responsible for updating them when pods change.
