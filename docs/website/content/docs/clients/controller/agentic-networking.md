---
title: "Agentic Networking"
weight: 6
---

The controller supports [Kube Agentic Networking](https://kube-agentic-networking.sigs.k8s.io/) (`agentic.prototype.x-k8s.io/v0alpha0`), the Kubernetes SIG-Network standard for securing AI agent access to MCP (Model Context Protocol) backends.

## Resources

| CRD | What it does | Vrata mapping |
|-----|-------------|---------------|
| `XBackend` | Declares an MCP backend (service or external hostname) | `Destination` + `Route` |
| `XAccessPolicy` | Authorizes who can call which tools | `inlineAuthz` or `extAuthz` `Middleware` |

## Enabling

Add to your controller config:

```yaml
watch:
  xBackends: true
  xAccessPolicies: true
agentic:
  trustDomain: "cluster.local"
```

Install the CRDs:

```bash
make controller-deploy-agentic-crds
```

## XBackend

An XBackend declares an MCP server. It can reference a Kubernetes Service or an external hostname.

```yaml
apiVersion: agentic.prototype.x-k8s.io/v0alpha0
kind: XBackend
metadata:
  name: calc-server
spec:
  mcp:
    serviceName: calc-svc
    port: 9000
    path: /mcp
```

### Mapping

| Field | Vrata entity |
|-------|-------------|
| `spec.mcp.serviceName` | `Destination` with host `{serviceName}.{namespace}.svc.cluster.local` |
| `spec.mcp.hostname` | `Destination` with host = hostname, TLS enabled |
| `spec.mcp.port` | `Destination.port` |
| `spec.mcp.path` | `Route` with `pathPrefix` (default `/mcp`) |

### Ownership

Entities are named `k8s:agentic:{namespace}/{name}`:

- Destination: `k8s:agentic:default/calc-server`
- Route: `k8s:agentic:default/calc-server/mcp`

### External MCP servers

```yaml
apiVersion: agentic.prototype.x-k8s.io/v0alpha0
kind: XBackend
metadata:
  name: wiki
spec:
  mcp:
    hostname: mcp.deepwiki.com
    port: 443
    path: /mcp
```

External backends get TLS enabled automatically on the Destination.

## XAccessPolicy

An XAccessPolicy defines who can access which tools on an MCP backend.

```yaml
apiVersion: agentic.prototype.x-k8s.io/v0alpha0
kind: XAccessPolicy
metadata:
  name: calc-access
spec:
  targetRefs:
  - group: agentic.prototype.x-k8s.io
    kind: XBackend
    name: calc-server
  rules:
  - name: agent-a-tools
    source:
      type: SPIFFE
      spiffe: "spiffe://cluster.local/ns/default/sa/agent-a"
    authorization:
      type: InlineTools
      tools:
      - add
      - subtract
  - name: agent-b-tools
    source:
      type: ServiceAccount
      serviceAccount:
        name: agent-b
    authorization:
      type: InlineTools
      tools:
      - subtract
```

### InlineTools mapping

The controller generates an `inlineAuthz` middleware with CEL rules:

1. **Always-allow rule** — `GET`, `DELETE`, `initialize`, `notifications/initialized`, `tools/list` are always permitted
2. **Per-source rules** — each rule combines identity matching (SPIFFE URI from mTLS cert) with tool allowlist

ServiceAccount sources are converted to SPIFFE IDs using the configured `trustDomain`:
`spiffe://{trustDomain}/ns/{namespace}/sa/{name}`

### ExternalAuth mapping

```yaml
rules:
- name: ext-rule
  source:
    type: SPIFFE
    spiffe: "spiffe://example.org/sa/agent"
  authorization:
    type: ExternalAuth
    externalAuth:
      backendRef:
        name: authz-service
        port: 9090
      protocol: GRPC
```

The controller generates an `extAuthz` middleware pointing to the authorization service.

### Ownership

Middlewares are named `k8s:agentic:{namespace}/{policy-name}/inlineauthz` or `k8s:agentic:{namespace}/{policy-name}/extauthz`.

## Garbage collection

When an `XBackend` or `XAccessPolicy` is deleted from Kubernetes, the controller automatically removes the corresponding Destinations, Routes, and Middlewares from Vrata.

## Prerequisites

For InlineTools with SPIFFE/ServiceAccount identity, the listener must have [mTLS enabled]({{< relref "/docs/concepts/listeners/mtls" >}}) so client certificates are verified and available in CEL expressions.

## Full example

1. Create an mTLS listener:

```json
POST /api/v1/listeners
{
  "name": "mcp-gateway",
  "port": 8443,
  "tls": {
    "certPath": "/certs/server.crt",
    "keyPath": "/certs/server.key",
    "clientAuth": {
      "mode": "require",
      "caFile": "/certs/trusted-ca.pem"
    }
  }
}
```

2. Apply XBackend and XAccessPolicy in Kubernetes (see above).

3. The controller creates the Destination, Route, and `inlineAuthz` Middleware in Vrata.

4. Agents with valid SPIFFE certificates can call only the tools listed in their policy. All others are denied with 403.
