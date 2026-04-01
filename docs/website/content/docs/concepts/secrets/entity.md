---
title: "Secret Entity"
weight: 1
---

A Secret is a flat entity with three fields: ID, Name, and Value. One secret holds one value. If you need a cert, a key, and a CA — that's three secrets.

## Creating a secret

```bash
curl -X POST localhost:8080/api/v1/secrets \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "prod-tls-cert",
    "value": "-----BEGIN CERTIFICATE-----\nMIID..."
  }'
```

The response returns a `SecretSummary` (ID + Name only, no Value):

```json
{"id": "abc-123", "name": "prod-tls-cert"}
```

## Reading a secret

```bash
# Full secret including value
curl localhost:8080/api/v1/secrets/abc-123

# List all secrets (summaries only, no values)
curl localhost:8080/api/v1/secrets
```

The list endpoint never returns values — only ID and Name.

## Updating a secret

```bash
curl -X PUT localhost:8080/api/v1/secrets/abc-123 \
  -H 'Content-Type: application/json' \
  -d '{"name": "prod-tls-cert", "value": "-----BEGIN CERTIFICATE-----\nNEW..."}'
```

Existing activated snapshots are not affected. To push the new value to proxies, create and activate a new snapshot.

## Deleting a secret

```bash
curl -X DELETE localhost:8080/api/v1/secrets/abc-123
```

Entities that reference a deleted secret will fail at the next snapshot build.

## All fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Auto-generated UUID |
| `name` | string | Human-readable label |
| `value` | string | The sensitive content (PEM, token, password, etc.) |
