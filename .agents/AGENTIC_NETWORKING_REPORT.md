# Kube Agentic Networking — Feasibility Report

**Date**: 2026-03-25
**Source**: `/home/ahernandez/Documents/Git/3rdparty/kube-agentic-networking` (actual repo)
**Status**: Fully implemented (proxy + controller)

---

## What the spec actually is

Kube Agentic Networking (`agentic.prototype.x-k8s.io/v0alpha0`) defines **exactly 2 CRDs**.
It has nothing to do with Inference Extension (`inference.networking.k8s.io`), which is
a separate project, separate API group, separate repo. They share the same SIG umbrella
but are independent specs.

### XBackend

An MCP (Model Context Protocol) backend. Fields:

| Field | Type | Description |
|---|---|---|
| `spec.mcp.serviceName` | `*string` | K8s Service name (mutually exclusive with hostname) |
| `spec.mcp.hostname` | `*string` | External FQDN (mutually exclusive with serviceName) |
| `spec.mcp.port` | `int32` | Port (1-65535) |
| `spec.mcp.path` | `string` | URL path for MCP traffic (default `/mcp`) |

An XBackend is referenced from standard HTTPRoute `backendRefs`. The reference
implementation translates it to an Envoy cluster (STRICT_DNS for serviceName,
LOGICAL_DNS+TLS for hostname).

### XAccessPolicy

Authorization policy for tool access. Fields:

| Field | Type | Description |
|---|---|---|
| `spec.targetRefs` | `[]LocalPolicyTargetReferenceWithSectionName` | Targets: XBackend or Gateway (not both) |
| `spec.rules[].name` | `string` | Unique rule name |
| `spec.rules[].source` | `Source` | Who is making the request |
| `spec.rules[].authorization` | `*AuthorizationRule` | What they can do (optional — absent = deny all) |

**Source types** (who):

- `ServiceAccount` → `{name, namespace}` — a k8s service account
- `SPIFFE` → `"spiffe://domain/ns/default/sa/agent"` — a SPIFFE URI

**AuthorizationRule types** (what):

- `InlineTools` → `tools: ["add", "subtract"]` — static allowlist of MCP tool names
- `ExternalAuth` → `{backendRef, protocol: HTTP|GRPC, http: {...}, grpc: {...},
  forwardBody: {maxSize}}` — delegate to ext_authz service

**Policy evaluation order** (from proposal 0059):

1. Gateway-level policies evaluated first (oldest creation timestamp wins)
2. If all pass, backend-level policies evaluated
3. Any deny → request rejected

**Always-allowed MCP methods** (from reference implementation):

- `initialize`, `notifications/initialized`, `tools/list` — any identity can call these
- `DELETE` with `mcp-session-id` header — session close
- `GET` — SSE stream establishment

---

## How the reference implementation works

The reference implementation uses **Envoy as data plane** with a custom MCP filter:

1. Controller deploys an Envoy proxy pod per Gateway
2. Translates everything to xDS (listeners, routes, clusters, RBAC filters)
3. XBackend → Envoy cluster
4. Identity is established via **SPIFFE mTLS** — listeners require client certs,
   RBAC matches on SPIFFE principal from the cert
5. `ServiceAccount` sources are converted to SPIFFE IDs:
   `spiffe://<trustDomain>/ns/<ns>/sa/<name>`
6. `InlineTools` → Envoy RBAC filter that reads MCP metadata (tool name) from a
   **custom Envoy filter** (`envoy.filters.http.mcp`) that parses the JSON-RPC body
7. `ExternalAuth` → Envoy ext_authz filter triggered by RBAC shadow rule match
8. Max 5 AccessPolicies per target (seniority by creation timestamp)

---

## Mapping to Vrata

### XBackend → Destination + Route (95% covered, no proxy changes)

| Requirement | Vrata equivalent | Status |
|---|---|---|
| Backend with k8s Service | `Destination` (FQDN from serviceName) | Exists |
| Backend with external hostname | `Destination` (host = hostname) | Exists |
| Port | `Destination.Port` | Exists |
| MCP path | `Route.Match.PathPrefix` + `ForwardAction.Rewrite` | Exists |
| TLS for external backends | `Destination.Options.TLS` | Exists |
| Referenced from HTTPRoute backendRef | Controller already maps backendRefs → Destinations | Exists |

No gaps.

### XAccessPolicy with ExternalAuth → ExtAuthz Middleware (85% covered, no proxy changes)

| Requirement | Vrata equivalent | Status |
|---|---|---|
| Delegate authz to external service | `Middleware.ExtAuthz` | Exists |
| HTTP protocol | `ExtAuthz.Mode: "http"` | Exists |
| gRPC protocol | `ExtAuthz.Mode: "grpc"` | Exists |
| backendRef for authz service | `ExtAuthz.DestinationID` | Exists |
| Forward request body | `ExtAuthz.IncludeBody` | Exists |
| Allowed request headers | `ExtAuthz.OnCheck.ForwardHeaders` | Exists |
| Allowed response headers | `ExtAuthz.OnAllow.CopyToUpstream` | Exists |
| HTTP path prefix | `ExtAuthz.Path` | Exists |
| Apply per-backend (per-route scope) | `MiddlewareIDs` on Route/Group | Exists |
| Apply per-gateway (all traffic) | Not directly — Vrata scopes middlewares to Route/Group | Controller wiring |

The gateway-level scope gap is a controller concern: attach the middleware to all
groups under that gateway. No proxy change needed.

### XAccessPolicy with InlineTools — Uses: CEL body access + inlineAuthz (implemented)

| Requirement | Vrata feature | Status |
|---|---|---|
| Parse JSON-RPC request body | CEL `request.body.json` | **Implemented** |
| Extract tool name from body | CEL: `request.body.json.params.name` | **Implemented** |
| Match tool name against allowlist | CEL: `request.body.json.params.name in ["add"]` | **Implemented** |
| Always allow initialize/list/close | CEL rule in inlineAuthz middleware | **Implemented** |
| Deny unmatched tools/call | inlineAuthz defaultAction: deny | **Implemented** |

The controller maps InlineTools to `inlineAuthz` middleware rules with generated
CEL expressions — the proxy never knows about MCP.

### SPIFFE/ServiceAccount identity — Uses: mTLS client auth (implemented)

| Requirement | Vrata feature | Status |
|---|---|---|
| Require/verify client certificates | `ListenerTLS.ClientAuth` (mode + CA) | **Implemented** |
| Extract SPIFFE ID from client cert | CEL `request.tls.peerCertificate.uris` | **Implemented** |
| Match SPIFFE ID in authorization | CEL expression in inlineAuthz | **Implemented** |
| ServiceAccount → SPIFFE conversion | Controller logic (not proxy) | Controller TODO |
| Forward client cert to ExtAuthz | XFCC header injection | **Implemented** |

---

## Generic proxy features used (3 total, all implemented)

All features are protocol-agnostic extensions of existing Vrata capabilities.
None contains any MCP-specific logic. See `SERVER_DECISIONS.md` for design.
See `SERVER_TODO.md` (Done section) for implementation details and test counts.

### Feature 1: CEL body access (`request.body.raw` + `request.body.json`)

**What**: Two new fields in the CEL `request` map:
- `request.body.raw` — always `string`, raw bytes up to `celBodyMaxSize` (default 64KB)
- `request.body.json` — `map(string, dyn)`, only populated when `Content-Type` is
  `application/json` and parse succeeds. Field does not exist otherwise (`has()` = false).

Body buffering is **lazy** — only triggered when a CEL program in the matched route
references `request.body`. Determined at build time via a `needsBody` flag on the
compiled route/middleware. Routes without body-referencing CEL have zero overhead.

**What it enables** (without the proxy knowing about any of these protocols):
- MCP tool authorization: `request.body.json.method == "tools/call" && request.body.json.params.name in ["add"]`
- GraphQL operation filtering: `request.body.json.operationName == "IntrospectionQuery"`
- Raw text matching: `request.body.raw.contains("ERROR")`

**Overlap with existing features**:
- `ExtProc` buffers request bodies for its streaming pipeline. Orthogonal — CEL body
  access happens in route matching / middleware conditions, not in ExtProc.
- `ExtAuthz.IncludeBody` forwards the body externally. CEL evaluates locally. No overlap.

### Feature 2: mTLS client authentication + cert info in CEL

**What**: New `clientAuth` object on `ListenerTLS` (mode: none/optional/require + caFile).
When a client cert is verified, its metadata is exposed in CEL:
- `request.tls.peerCertificate.uris` — URI SANs (includes SPIFFE IDs)
- `request.tls.peerCertificate.dnsNames` — DNS SANs
- `request.tls.peerCertificate.subject` — Distinguished Name
- `request.tls.peerCertificate.serial` — Serial number (hex)

Automatic `X-Forwarded-Client-Cert` header injection (incoming XFCC stripped to
prevent spoofing). ExtAuthz can forward it via `onCheck.forwardHeaders`.

**What it enables**:
- SPIFFE authorization: `request.tls.peerCertificate.uris.exists(u, u == "spiffe://...")`
- Any mTLS verification: `request.tls.peerCertificate.dnsNames.exists(d, d == "agent.example.com")`
- Cert forwarding to ExtAuthz without ExtAuthz changes

**Overlap with existing features**:
- `ListenerTLS` already has cert/key/minVersion/maxVersion. `ClientAuth` completes mTLS.
- `JWT` validates bearer tokens (application layer). mTLS validates certs (transport layer). No overlap.

### Feature 3: Inline authorization middleware (`inlineAuthz`)

**What**: New middleware type. Symmetric pair of `extAuthz`: evaluates authorization
locally with ordered CEL rules instead of delegating to an external service.

```json
{
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      { "cel": "<expression>", "action": "allow" },
      { "cel": "<expression>", "action": "deny" }
    ],
    "defaultAction": "deny",
    "denyStatus": 403,
    "denyBody": "{\"error\": \"access denied\"}"
  }
}
```

First-match wins. CEL expressions have access to the full `request.*` map including
`request.body.*` and `request.tls.*`. Standard middleware lifecycle (skipWhen/onlyWhen,
MiddlewareOverride, metrics).

**What it enables**:
- Body-based authorization: tool allowlists, operation filtering, JSON-RPC dispatch
- Identity-based authorization: SPIFFE, cert DN, any mTLS attribute
- Any combination via CEL: `identity AND operation AND method AND path`

**Overlap with existing features**:
- `extAuthz` delegates externally. `inlineAuthz` evaluates locally. Complementary.
- `skipWhen`/`onlyWhen` condition whether a middleware runs. `inlineAuthz` decides
  whether the request passes. Different semantics.
- `assertClaims` (JWT) evaluates CEL against JWT claims. `inlineAuthz` evaluates
  against the full request. Different scope, no overlap.

---

## Coverage after implementation

| Feature | Before | After | How |
|---|---|---|---|
| XBackend → Destination | 95% | 100% | Controller mapper only |
| ExternalAuth → ExtAuthz | 85% | 100% | Controller mapper only |
| InlineTools (body-based authz) | 0% | 100% | CEL body access + `inlineAuthz` middleware (implemented) + controller |
| SPIFFE identity | 0% | 100% | mTLS client auth (implemented) + CEL cert access + controller |
| ServiceAccount identity | 0% | 100% | Controller converts SA → SPIFFE URI, same mechanism |
| Gateway-level policies | 0% | 100% | Controller attaches `inlineAuthz`/`extAuthz` to all groups |
| Always-allow MCP methods | 0% | 100% | Controller generates allow rules in `inlineAuthz` |
| Policy evaluation order | 0% | 100% | Controller orders middleware chain by seniority |
| Max 5 policies per target | 0% | 100% | Controller enforces limit |

**Proxy features**: all implemented and tested (305 server unit + 96 server e2e).
**Controller wiring**: all implemented and tested (19 mapper unit + 5 controller e2e).
See `SERVER_TODO.md` and `CONTROLLER_TODO.md` for full implementation details.
