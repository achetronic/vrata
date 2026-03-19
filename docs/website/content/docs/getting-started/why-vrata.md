---
title: "Why Vrata?"
weight: 2
---

There are good proxies out there. Vrata exists because none of them got the balance right.

## The problem with existing gateways

Every major proxy — Envoy, Traefik, NGINX, Kong — started with a broad mission: handle every type of traffic for every type of organization. Over the years they accumulated features for every edge case, compatibility layers for every protocol, and plugin systems to cover what they couldn't build in. The result is powerful but heavy: dozens of moving parts, external dependencies, and configuration surfaces that take months to master.

If you just need an API gateway — HTTP routing, auth, rate limiting, observability, canary deploys — you're paying the complexity tax for features you'll never touch.

## What Vrata does differently

Vrata studies what the existing proxies got right, discards what most API gateways never need, and redesigns the rest with a clean, minimal API.

**Borrowed and improved:**

- **Envoy's two-level load balancing** — destination selection (which service?) then endpoint selection (which pod?). Vrata offers 3 algorithms at the destination level and 6 at the endpoint level, covering weighted random, consistent hashing, least-request, Maglev, and more. Where other proxies struggle — particularly with sticky sessions during canary deploys, where users need to stay pinned to a specific version — Vrata gets it right with cookie-based, consistent hash, and Redis-backed zero-disruption pinning at both levels.

- **Envoy's control plane / data plane separation** — Envoy pioneered the idea that a control plane holds configuration and the proxy just consumes it (via a protocol called xDS). Vrata takes the same architectural split but replaces the complexity with a much simpler mechanism: the control plane pushes complete JSON snapshots over SSE. No protobuf schemas, no incremental deltas, no version negotiation. One stream, one document, done.

- **Kong's REST API approach** — but applied to every entity, with versioned snapshots and instant rollback. Kong requires a database; Vrata embeds its own.

- **Envoy's external processor** — send request/response phases to your own service for inspection and mutation. Vrata keeps the model but simplifies the configuration to a single block — no plugin SDK, no sidecar, no recompiling the proxy.

- **Traefik's single-binary philosophy** — but with embedded HA via Raft instead of requiring external consensus.

**Left out on purpose:**

- **Plugin ecosystems** — Vrata has built-in middlewares and external processor/authorization for anything custom. No plugin SDK to version-lock against, no dynamic loading.

- **Service mesh** — Vrata supports mTLS at the edge (listener TLS termination and TLS to upstream), but it's not a service mesh. It doesn't inject sidecars or manage mTLS between workloads. It's a modern API gateway that serves as the HTTP router for all your services.

## First-class HTTP

Vrata is built for HTTP and everything that runs on top of it:

- **HTTP/1.1 and HTTP/2** — with automatic ALPN negotiation
- **gRPC** — first-class support via HTTP/2 listeners
- **WebSockets** — transparent upgrade handling
- **Any HTTP-based protocol** — if it speaks HTTP, Vrata routes it

## When to use something else

- **Massive plugin ecosystem** — Kong and Traefik have hundreds of community plugins. Vrata covers the common cases with built-in middlewares + external processor.
- **HTTP/3 / QUIC** — not supported yet.
- **Battle-tested at hyperscale** — Envoy has years of production use at Google/Lyft scale. Vrata is newer — but every feature ships with unit tests and massive end-to-end suites (thousands of routes, bulk reconciliation, failure injection) to guarantee the behaviour matches the spec. Young project, high engineering standards.

Choose the tool that matches your problem.
