# Architecture — Vrata

## Overview

Vrata is a programmable HTTP reverse proxy with a REST API. It has two modes:

- **Control plane** — exposes the REST API, stores configuration in bbolt,
  and pushes active snapshots to connected proxies via SSE. Optionally runs
  with Raft consensus for HA (3-5 nodes).
- **Proxy** — stateless, connects to a control plane via SSE, receives
  configuration snapshots, and routes traffic. Horizontally scalable.

In development, a single process runs both modes. In production, the control
plane runs as a StatefulSet and proxies as a Deployment.

## Components

### cmd/vrata

Entry point. Responsible for:

- Parsing the `--config` flag and loading configuration.
- Instantiating all dependencies (store, gateway, proxy router, listeners).
- Starting the REST API server and the proxy gateway.
- Handling OS signals for graceful shutdown.

### internal/config

Loads and validates the `config.yaml` file. Applies `os.ExpandEnv` to the raw
YAML bytes before unmarshalling so that `${ENV_VAR}` references are resolved.

### internal/model

Pure domain types. No business logic, no I/O. Key types:

- **Route** — matching rules + action (forward/redirect/directResponse).
- **RouteGroup** — a named collection of routes with shared matchers.
- **Destination** — an upstream target with endpoints, timeouts, TLS, balancing, circuit breaker, health checks, outlier detection.
- **Listener** — a network entry point with optional TLS, HTTP/2, metrics, proxy error formatting.
- **Middleware** — CORS, JWT, ExtAuthz, ExtProc, RateLimit, Headers, AccessLog, InlineAuthz.
- **Snapshot** — immutable point-in-time capture of all configuration.

### internal/store

Pluggable persistence interface. Implementations:

- **bolt** — bbolt embedded KV store (production).
- **memory** — in-memory (testing).
- **raftstore** — Raft wrapper that reads locally and writes through the Raft log.

### internal/api

REST API built on `net/http`. Structured as:

- **router** — registers all routes, applies middleware chain.
- **handlers/** — one handler file per resource.
- **middleware/** — request logging (httpsnoop), panic recovery.
- **respond/** — JSON response helpers.

### internal/proxy

The native HTTP reverse proxy. Core files:

- **router.go** — atomic routing table swap, request matching, metrics injection.
- **table.go** — compiles routes, groups, destinations into a RoutingTable.
- **handler.go** — forward/redirect/directResponse handlers, middleware chain.
- **endpoint.go** — creates Endpoints with Transport (all 7 destination timeouts wired).
- **pool.go** — DestinationPool with balancer, circuit breaker, session store.
- **balancer.go** — RoundRobin, Random, LeastRequest, RingHash, Maglev.
- **pinning.go** — weighted consistent hash ring for destination pinning.
- **retry.go** — retryTransport with backoff, per-attempt timeout, onRetry callback.
- **listener.go** — ListenerManager (start/stop/reconcile HTTP servers, metrics, timeouts, proxy error detail injection).
- **circuit.go** — CircuitBreaker (configurable threshold and open duration).
- **health.go** — HealthChecker (active HTTP probes with thresholds).
- **outlier.go** — OutlierDetector (ejects endpoints by consecutive errors).
- **errors.go** — ProxyError classification, structured JSON error responses with detail levels.
- **metrics.go** — MetricsCollector (25 Prometheus metrics, per-listener registry).
- **session.go** — SessionStore interface for sticky sessions.

### internal/proxy/middlewares

One file per middleware type. All use `httpsnoop` for response interception.
Each middleware that launches goroutines returns a stop function for cleanup on table swap.

### internal/proxy/celeval

CEL expression compiler and evaluator for route matching, skipWhen/onlyWhen,
JWT assertClaims, and inlineAuthz rules. Supports `request.body` (lazy buffering)
and `request.tls` (client certificate metadata from mTLS).

### internal/gateway

Orchestrator. Subscribes to store events, rebuilds the routing table, swaps it atomically, reconciles listeners, updates health checker and outlier detector.

### internal/raft

Embedded Raft consensus via hashicorp/raft. FSM backed by bbolt. Peer discovery via static list or DNS. Write-forwarding from followers to leader.

### internal/k8s

Kubernetes EndpointSlice and ExternalName Service watcher. Resolves pod IPs for destinations with `discovery.type: "kubernetes"`.

### internal/sync

SSE client for proxy-mode instances. Connects to `GET /api/v1/sync/snapshot`, receives versioned snapshots, applies them atomically.

### internal/session

Session store interface and Redis implementation for STICKY balancing.

### internal/tlsutil

Builds `tls.Config` and `http.Transport` from `config.TLSConfig`. Supports inline PEM and file paths via `resolvePEM`. Used by the CP HTTP server (server TLS + mTLS) and the sync client (client TLS + mTLS).

### internal/resolve

Resolves `{{secret:value/env/file}}` patterns in serialized JSON. Used by `buildSnapshot()` to resolve secret references before the snapshot is stored and pushed to proxies.

### internal/encrypt

AES-256-GCM encryption for at-rest protection of secrets and snapshots in bbolt. The `Cipher` type provides `Seal` and `Open` methods. Integrated into the bolt store via `encryptValue`/`decryptValue` — no-ops when no cipher is configured.

## Data Flow

```
Operator (API calls)
        │
        ▼
  REST API (net/http)
        │  validates, writes
        ▼
     Store (bbolt) ───── Raft log (HA) ───── Other CP nodes
        │  publishes StoreEvent
        ▼
    Gateway
        │  rebuilds routing table
        ▼
   Proxy Router ←── Listeners (0..N ports)
        │                    ↑
        │              Users connect here
        ▼
  Destinations (upstream services)
```

In proxy mode, the Gateway is replaced by the SSE sync client which receives
snapshots from the control plane and applies them.

## Folder Structure

```
server/
├── cmd/vrata/main.go
├── internal/
│   ├── config/config.go
│   ├── model/                  # Pure domain types
│   ├── store/                  # Store interface + bolt + memory + raftstore
│   ├── api/                    # REST API (handlers, middleware, respond, router)
│   ├── proxy/                  # Native HTTP proxy
│   │   ├── middlewares/        # CORS, JWT, ExtAuthz, ExtProc, RateLimit, Headers, AccessLog, InlineAuthz
│   │   └── celeval/            # CEL expression evaluation
│   ├── gateway/gateway.go      # Store → proxy bridge
│   ├── raft/                   # Raft consensus (FSM, node, peer discovery)
│   ├── k8s/watcher.go          # Kubernetes endpoint discovery
│   ├── sync/client.go          # SSE sync client (proxy mode)
│   └── session/                # Session store interface + Redis
├── proto/                      # gRPC proto definitions (extproc, extauthz)
├── test/e2e/                   # End-to-end tests
├── docs/                       # Generated OpenAPI spec
├── Dockerfile (at repo root)
└── go.mod
```
