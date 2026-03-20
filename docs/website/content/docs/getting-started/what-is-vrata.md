---
title: "What is Vrata?"
weight: 1
---

Vrata is a programmable HTTP reverse proxy with a REST API. You create listeners, destinations, routes, and middlewares through HTTP calls — Vrata applies them instantly without restarts, reloads, or dropped connections.

## The idea

Every existing API gateway tries to be everything for everyone. They accumulate hundreds of features, plugins, and configuration knobs to cover every corner of the industry. The result is complexity: massive config files, external dependencies, version-locked sidecars, and features you'll never use slowing down the ones you need.

Vrata takes a different approach. Instead of covering every possible use case, it borrows the best ideas from existing proxies, discards the features that are rarely needed in a modern API gateway, and redesigns the ones that matter — from scratch, with a clean API surface.

## What you get

- **A REST API for everything** — every listener, route, destination, middleware, and snapshot is a JSON resource you create, update, and delete through HTTP. Any tool that speaks HTTP — a CI pipeline, a script, a UI, a Kubernetes controller — can configure the proxy.

- **Versioned snapshots** — changes are staged, not live. You edit your config via the API, capture a snapshot, and activate it. All proxies receive the new config atomically via SSE — a simple, reliable push mechanism inspired by the xDS model but without the complexity of protobuf schemas or incremental deltas. Bad deploy? Activate the previous snapshot. One call, instant rollback.

- **CEL expressions everywhere** — route matching, middleware conditions, and JWT claim assertions use CEL (Common Expression Language). Cross-field logic that other proxies can't express without custom plugins is a one-liner in Vrata.

- **External processor** — send any request/response phase to a gRPC or HTTP service for inspection and mutation. No plugin SDK, no recompiling the proxy. Your service, your language, your rules.

- **Reusable entities** — middlewares, destinations, and groups are independent resources. Declare a JWT middleware once, attach it to 50 routes. Update the JWKS endpoint in one place, every route picks it up.

- **Full HTTP protocol support** — HTTP/1.1, HTTP/2, gRPC, WebSockets, and any protocol built on top of HTTP. First-class support, not bolted on.

- **TLS at the edge** — terminate TLS on listeners, connect to upstreams over TLS or mTLS. Vrata secures the entry point to your infrastructure without trying to be a service mesh.

- **Observability from minute zero** — 22 Prometheus metrics across 5 dimensions, toggleable per listener. You don't install a plugin or configure a sidecar — metrics are built in.

- **One binary, zero dependencies** — no sidecar, no external database, no runtime dependencies. Embedded bbolt for storage, embedded Raft for HA. Run it on bare metal, Docker, or Kubernetes.

## How it works

```bash
# Open a port
curl -X POST localhost:8080/api/v1/listeners \
  -d '{"name": "main", "port": 3000}'

# Point at a backend
curl -X POST localhost:8080/api/v1/destinations \
  -d '{"name": "api", "host": "api-svc.default.svc.cluster.local", "port": 80}'

# Create a route
curl -X POST localhost:8080/api/v1/routes \
  -d '{"name": "api", "match": {"pathPrefix": "/api"}, "forward": {"destinations": [{"destinationId": "...", "weight": 100}]}}'

# Snapshot and activate — pushes to all proxies instantly
curl -X POST localhost:8080/api/v1/snapshots -d '{"name": "v1"}'
curl -X POST localhost:8080/api/v1/snapshots/{id}/activate
```

Changes propagate in milliseconds via SSE. No restart, no reload, no dropped connections.
