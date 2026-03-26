---
title: "Inline Authorization"
weight: 5
---

Evaluate authorization rules locally using CEL expressions. The symmetric pair of `extAuthz` — where `extAuthz` delegates decisions to an external service, `inlineAuthz` evaluates them inside the proxy with zero network hops.

## Configuration

```json
{
  "name": "access-control",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      { "cel": "request.method == 'GET'", "action": "allow" },
      { "cel": "request.path == '/admin'", "action": "deny" }
    ],
    "defaultAction": "deny",
    "denyStatus": 403,
    "denyBody": "{\"error\": \"access denied\"}"
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `rules` | array | required | Ordered list of rules (first match wins) |
| `rules[].cel` | string | required | CEL expression (must return bool) |
| `rules[].action` | string | required | `allow` or `deny` |
| `defaultAction` | string | `deny` | Action when no rule matches |
| `denyStatus` | number | `403` | HTTP status code on deny |
| `denyBody` | string | `{"error":"access denied"}` | Response body on deny |

## How it works

1. Rules are evaluated in order against the incoming request
2. The first rule whose CEL expression returns `true` wins
3. The winning rule's `action` is applied: `allow` passes the request to the next middleware/upstream, `deny` returns the configured status and body
4. If no rule matches, `defaultAction` is applied

CEL expressions are compiled once at routing table build time. Evaluation cost is ~1-2μs per expression.

## CEL variables

All standard request variables are available:

| Variable | Type | Description |
|----------|------|-------------|
| `request.method` | string | HTTP method |
| `request.path` | string | Request path |
| `request.host` | string | Host header (port stripped) |
| `request.scheme` | string | `http` or `https` |
| `request.headers` | map | Request headers (lowercased keys) |
| `request.queryParams` | map | Query parameters |
| `request.clientIp` | string | Client IP (from XFF or RemoteAddr) |

Plus body and TLS variables when available:

| Variable | Type | When available |
|----------|------|----------------|
| `request.body.raw` | string | When any rule references `request.body` |
| `request.body.json` | map | When Content-Type is `application/json` |
| `request.tls.peerCertificate.uris` | list(string) | When client presents a TLS cert |
| `request.tls.peerCertificate.dnsNames` | list(string) | When client presents a TLS cert |
| `request.tls.peerCertificate.subject` | string | When client presents a TLS cert |

Body buffering is lazy — only triggered when a rule references `request.body`. Routes without body-referencing rules have zero overhead.

## Examples

### Basic path-based access

```json
{
  "name": "admin-guard",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      { "cel": "request.path.startsWith('/admin')", "action": "deny" }
    ],
    "defaultAction": "allow"
  }
}
```

Block `/admin/*` paths, allow everything else.

### IP-based allowlist

```json
{
  "name": "ip-guard",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      { "cel": "request.clientIp.startsWith('10.0.')", "action": "allow" },
      { "cel": "request.clientIp.startsWith('192.168.')", "action": "allow" }
    ],
    "defaultAction": "deny",
    "denyStatus": 403,
    "denyBody": "{\"error\": \"forbidden: not on allowlist\"}"
  }
}
```

Only allow internal IPs.

### JSON body field matching

Any JSON body field is accessible via `request.body.json`. This works with any protocol that sends JSON (JSON-RPC, GraphQL, REST, custom APIs).

```json
{
  "name": "rpc-guard",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      {
        "cel": "request.method == 'GET'",
        "action": "allow"
      },
      {
        "cel": "has(request.body) && has(request.body.json) && request.body.json.action in ['read', 'list', 'ping']",
        "action": "allow"
      },
      {
        "cel": "has(request.body) && has(request.body.json) && request.body.json.action == 'execute' && request.body.json.params.name in ['report', 'export']",
        "action": "allow"
      }
    ],
    "defaultAction": "deny"
  }
}
```

Allows safe read-only actions and only specific execute operations. All other requests are denied.

### Client identity + operation authorization (mTLS + body)

When mTLS is enabled on the listener, you can combine identity verification with body-based authorization:

```json
{
  "name": "service-access",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      {
        "cel": "request.method == 'GET'",
        "action": "allow"
      },
      {
        "cel": "has(request.tls) && request.tls.peerCertificate.dnsNames.exists(d, d == 'admin.internal') && has(request.body) && has(request.body.json) && request.body.json.params.operation in ['read', 'write']",
        "action": "allow"
      },
      {
        "cel": "has(request.tls) && request.tls.peerCertificate.dnsNames.exists(d, d == 'viewer.internal') && has(request.body) && has(request.body.json) && request.body.json.params.operation in ['read']",
        "action": "allow"
      }
    ],
    "defaultAction": "deny"
  }
}
```

The admin service (identified by DNS SAN) can read and write. The viewer service can only read.

### MCP tool authorization

For [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) backends, you can control which tools each caller can invoke:

```json
{
  "name": "mcp-tools",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      {
        "cel": "request.method == 'GET' || request.method == 'DELETE'",
        "action": "allow"
      },
      {
        "cel": "has(request.body) && has(request.body.json) && request.body.json.method in ['initialize', 'notifications/initialized', 'tools/list']",
        "action": "allow"
      },
      {
        "cel": "has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == 'spiffe://cluster.local/ns/default/sa/agent-a') && has(request.body) && has(request.body.json) && (request.body.json.method != 'tools/call' || request.body.json.params.name in ['add', 'subtract'])",
        "action": "allow"
      }
    ],
    "defaultAction": "deny"
  }
}
```

This allows MCP session setup for everyone, and only the `add` and `subtract` tools for `agent-a` (identified by SPIFFE URI SAN from mTLS). The Vrata controller generates these rules automatically from `XAccessPolicy` resources — see [Agentic Networking]({{< relref "/docs/clients/controller/agentic-networking" >}}).

### GraphQL operation filtering

```json
{
  "name": "graphql-guard",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      {
        "cel": "has(request.body) && has(request.body.json) && request.body.json.operationName == 'IntrospectionQuery'",
        "action": "deny"
      }
    ],
    "defaultAction": "allow"
  }
}
```

Block GraphQL introspection queries in production.

## Comparison with extAuthz

| | `inlineAuthz` | `extAuthz` |
|---|---|---|
| Where decisions are made | Inside the proxy | External service |
| Network hop | None | One round-trip per request |
| Rule language | CEL expressions | Any (service decides) |
| Best for | Static rules, body/header/identity matching | Dynamic policies, user databases, complex logic |
| Configuration | Inline in middleware config | Separate auth service deployment |

Use `inlineAuthz` when the decision depends on data available in the request (identity, body, headers). Use `extAuthz` when the decision requires external state (user DB, policy engine, rate limits).

Both can coexist on the same route — `inlineAuthz` for fast local checks, `extAuthz` for complex decisions.

## Fault isolation

If a CEL expression has a compile error, that rule is skipped with an error log. The remaining rules still work. One bad rule never breaks the entire middleware.
