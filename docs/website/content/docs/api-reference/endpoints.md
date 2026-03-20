---
title: "API Reference"
weight: 50
---

The Vrata control plane ships a built-in **Swagger UI** with the full OpenAPI specification. This is the authoritative, always-up-to-date reference for every endpoint, request body, and response schema.

## Accessing the Swagger UI

The control plane serves the interactive API explorer at:

```
http://<control-plane-host>:8080/api/v1/docs/
```

By default the control plane listens on port `8080`. If you changed the `controlPlane.address` in your config, adjust accordingly.

### In Kubernetes

If you deployed with the Helm chart:

```bash
kubectl port-forward svc/vrata-control-plane 8080:8080
```

Then open [http://localhost:8080/api/v1/docs/](http://localhost:8080/api/v1/docs/) in your browser.

### In Docker

```bash
docker run -p 8080:8080 ghcr.io/achetronic/vrata server --config /etc/vrata/config.yaml
```

Then open [http://localhost:8080/api/v1/docs/](http://localhost:8080/api/v1/docs/).

## OpenAPI spec (machine-readable)

The raw OpenAPI JSON document is available at:

```
http://<control-plane-host>:8080/api/v1/docs/doc.json
```

Use this to generate clients, import into Postman, or feed to any tool that consumes OpenAPI specs.

```bash
curl -s localhost:8080/api/v1/docs/doc.json | jq .info
```

## What the Swagger UI covers

The spec documents every resource endpoint:

| Resource | Operations |
|----------|------------|
| **Listeners** | List, Create, Get, Update, Delete |
| **Destinations** | List, Create, Get, Update, Delete |
| **Routes** | List, Create, Get, Update, Delete |
| **Groups** | List, Create, Get, Update, Delete |
| **Middlewares** | List, Create, Get, Update, Delete |
| **Snapshots** | List, Create, Get, Delete, Activate |

Plus internal endpoints:

| Endpoint | Description |
|----------|-------------|
| `GET /sync/snapshot` | SSE stream for proxy-mode instances |
| `POST /sync/raft` | Raft write-forwarding (internal) |
| `GET /debug/config` | Dump live configuration |

Every endpoint includes full request/response schemas, field descriptions, validation rules, and example payloads — all generated from the Go source code annotations.

## Error format

All API errors return JSON:

```json
{"error": "resource not found"}
```

| Status | Meaning |
|--------|---------|
| `400` | Bad request (validation failure, malformed JSON) |
| `404` | Resource not found |
| `409` | Conflict (duplicate name) |
| `500` | Internal server error |

## Why Swagger instead of static docs

The Swagger UI is generated directly from the server's Go struct definitions and handler annotations using [swag](https://github.com/swaggo/swag). This means:

- **Always accurate** — the spec matches the running binary exactly
- **Try it live** — execute requests directly from the browser
- **Schema validation** — see every field, type, default, and constraint
- **No drift** — no manually-maintained API docs to get out of sync
