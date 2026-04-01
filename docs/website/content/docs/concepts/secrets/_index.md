---
title: "Secrets"
weight: 7
---

Secrets are first-class entities that hold sensitive values — TLS certificates, private keys, API tokens, or any other material you don't want to hardcode into entity definitions.

## How it works

1. **Create a Secret** via the REST API with a name and a value
2. **Reference it** in any string field of any entity using `{{secret:value:<id>}}`
3. **Create a snapshot** — the control plane resolves all references and embeds the literal values
4. **The proxy receives resolved values** — it never sees `{{secret:...}}` patterns

Secrets never travel in the snapshot as a separate array. Only their resolved values appear inside the entities that reference them.

## Quick example

```bash
# Create a secret with a TLS certificate
curl -X POST localhost:8080/api/v1/secrets \
  -H 'Content-Type: application/json' \
  -d '{"name": "prod-cert", "value": "-----BEGIN CERTIFICATE-----\nMIID..."}'

# Reference it in a listener
curl -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "https",
    "port": 443,
    "tls": {
      "cert": "{{secret:value:<secret-id>}}",
      "key": "{{secret:value:<key-secret-id>}}"
    }
  }'

# Create a snapshot — references are resolved
curl -X POST localhost:8080/api/v1/snapshots \
  -d '{"name": "v1.0"}'
```

## Three reference sources

| Pattern | Resolved from | Example |
|---------|--------------|---------|
| `{{secret:value:<id>}}` | Secret entity in the store | `{{secret:value:abc123}}` |
| `{{secret:env:<var>}}` | Environment variable on the CP | `{{secret:env:TLS_KEY}}` |
| `{{secret:file:<path>}}` | File on the CP filesystem | `{{secret:file:/certs/ca.pem}}` |

All three are resolved by the control plane at snapshot build time. The proxy is stateless — it receives a snapshot with literal values and never reads env vars or files for secrets.

## Security

- **List endpoint** returns only ID and Name — never the Value
- **Secret values are never logged** — only ID and Name appear in slog output
- **The SSE channel should be TLS-protected** — resolved values travel in the snapshot JSON
- **Snapshot persistence** — resolved snapshots in bbolt contain literal values. Protect the bbolt file with appropriate permissions.
