---
title: "References & Resolution"
weight: 2
---

Any string field in any entity can contain a `{{secret:...}}` reference. The control plane resolves all references when a snapshot is created — the proxy receives literal values.

## Pattern syntax

```
{{secret:<source>:<ref>}}
```

## Sources

### `value` — from a Secret entity

```json
"cert": "{{secret:value:abc-123}}"
```

Looks up the Secret with ID `abc-123` in the store and substitutes its Value.

### `env` — from an environment variable

```json
"key": "{{secret:env:TLS_PRIVATE_KEY}}"
```

Calls `os.Getenv("TLS_PRIVATE_KEY")` on the control plane process.

### `file` — from a file on the CP filesystem

```json
"ca": "{{secret:file:/etc/ssl/custom-ca.pem}}"
```

Reads the file from the control plane's filesystem.

## When resolution happens

Resolution runs inside `POST /api/v1/snapshots` (snapshot creation):

1. The snapshot is assembled from all entities (listeners, routes, groups, destinations, middlewares)
2. The JSON is scanned for `{{secret:...}}` patterns
3. Each pattern is resolved from its source
4. The resolved JSON is stored as the versioned snapshot

If **any** reference fails to resolve — missing Secret, unset env var, missing file — the **entire snapshot creation fails** with a 400 error listing all unresolved references. No partially-resolved snapshots are ever created.

## Inline values still work

Fields that accept `{{secret:...}}` also accept literal values:

```json
"cert": "-----BEGIN CERTIFICATE-----\nMIID..."
```

No secret reference needed. This is backwards compatible.

## Resolution is one-way

Once a snapshot is created, the resolved values are baked in. If you update a Secret, existing snapshots keep the old value. Create and activate a new snapshot to pick up changes.

## Examples

### TLS listener with secrets

```json
{
  "name": "https",
  "port": 443,
  "tls": {
    "cert": "{{secret:value:prod-cert-id}}",
    "key": "{{secret:value:prod-key-id}}"
  }
}
```

### Mixed sources

```json
{
  "name": "mtls-listener",
  "port": 8443,
  "tls": {
    "cert": "{{secret:value:server-cert}}",
    "key": "{{secret:env:SERVER_KEY}}",
    "clientAuth": {
      "mode": "require",
      "ca": "{{secret:file:/certs/client-ca.pem}}"
    }
  }
}
```
