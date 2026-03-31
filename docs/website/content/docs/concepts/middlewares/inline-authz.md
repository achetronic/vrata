---
title: "Inline Authorization"
weight: 8
---

Evaluate authorization rules locally using CEL expressions — no external service needed. This is the local counterpart of [External Authorization]({{< relref "ext-authz" >}}): where `extAuthz` delegates to a remote service, `inlineAuthz` evaluates rules in the proxy itself.

## Configuration

```json
{
  "name": "tool-guard",
  "type": "inlineAuthz",
  "inlineAuthz": {
    "rules": [
      { "cel": "request.method == \"GET\"", "action": "allow" },
      { "cel": "request.path.endsWith(\"/admin\")", "action": "deny" }
    ],
    "defaultAction": "deny",
    "denyStatus": 403,
    "denyBody": "{\"error\": \"access denied\"}"
  }
}
```

## How it works

Rules are evaluated in order. The first rule whose CEL expression returns `true` wins. If no rule matches, `defaultAction` applies.

1. Request arrives
2. Each rule is evaluated top-to-bottom
3. First match → apply `action` (allow or deny)
4. No match → apply `defaultAction`

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `rules` | object[] | required | Ordered list of authorization rules |
| `rules[].cel` | string | required | CEL expression (must return bool) |
| `rules[].action` | string | required | `"allow"` or `"deny"` |
| `defaultAction` | string | `"deny"` | What to do when no rule matches |
| `denyStatus` | integer | `403` | HTTP status code on deny |
| `denyBody` | string | `{"error":"access denied"}` | Response body on deny (sent as JSON) |

## CEL variables

Rules have access to the full request context:

| Variable | Type | Description |
|----------|------|-------------|
| `request.method` | string | HTTP method |
| `request.path` | string | URL path |
| `request.host` | string | Hostname without port |
| `request.scheme` | string | `"http"` or `"https"` |
| `request.headers` | map | Request headers (lowercase keys) |
| `request.queryParams` | map | Query parameters |
| `request.clientIp` | string | Client IP address |
| `request.body.raw` | string | Raw request body (up to `celBodyMaxSize`) |
| `request.body.json` | map | Parsed JSON body (when Content-Type is `application/json`) |
| `request.tls.peerCertificate.uris` | list | URI SANs from client cert (SPIFFE IDs live here) |
| `request.tls.peerCertificate.dnsNames` | list | DNS SANs from client cert |
| `request.tls.peerCertificate.subject` | string | Certificate subject DN |
| `request.tls.peerCertificate.serial` | string | Certificate serial (hex) |

Body and TLS fields are only present when the corresponding data exists. Always guard access with `has()`:

```
has(request.body) && has(request.body.json) && request.body.json.method == "tools/call"
```

```
has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "spiffe://cluster.local/ns/default/sa/agent")
```

## Examples

### Allow GET, deny everything else

```json
{
  "rules": [
    { "cel": "request.method == \"GET\"", "action": "allow" }
  ],
  "defaultAction": "deny"
}
```

### MCP tool filtering

Allow session management and specific tools, deny everything else:

```json
{
  "rules": [
    { "cel": "request.method == \"GET\" || request.method == \"DELETE\"", "action": "allow" },
    { "cel": "has(request.body) && has(request.body.json) && request.body.json.method in [\"initialize\", \"tools/list\"]", "action": "allow" },
    { "cel": "has(request.body) && has(request.body.json) && request.body.json.method == \"tools/call\" && request.body.json.params.name in [\"add\", \"subtract\"]", "action": "allow" }
  ],
  "defaultAction": "deny",
  "denyStatus": 403,
  "denyBody": "{\"error\": \"tool access denied\"}"
}
```

### SPIFFE identity check

Only allow requests from a specific service identity:

```json
{
  "rules": [
    { "cel": "has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == \"spiffe://cluster.local/ns/default/sa/frontend\")", "action": "allow" }
  ],
  "defaultAction": "deny"
}
```

### Default allow with blocklist

```json
{
  "rules": [
    { "cel": "request.path.endsWith(\"/admin\")", "action": "deny" },
    { "cel": "request.headers[\"x-internal\"] == \"true\"", "action": "deny" }
  ],
  "defaultAction": "allow"
}
```

## Validation

CEL expressions are compiled when you create or update the middleware. Invalid expressions are rejected with a 400 error — you'll never get a broken rule into a snapshot.

## Conditional execution

Like all middlewares, `inlineAuthz` supports `skipWhen`, `onlyWhen`, and `disabled` via [Conditions]({{< relref "conditions" >}}). These conditions also have access to `request.body` and `request.tls`.

## When to use inlineAuthz vs extAuthz

| Use case | Middleware |
|----------|-----------|
| Decision depends only on request data (path, headers, body, cert) | `inlineAuthz` |
| Decision needs external state (user DB, dynamic policies, OPA) | `extAuthz` |
| Low latency required (no network hop) | `inlineAuthz` |
| Centralized policy management across services | `extAuthz` |
