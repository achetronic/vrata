# Architecture — Vrata

## Overview

Vrata is a programmable HTTP reverse proxy with a REST API. It operates as
an xDS control plane that manages a fleet of Envoy proxy instances:

- **Control plane** — exposes the REST API, stores configuration in bbolt,
  translates Vrata model entities to Envoy xDS resources, and pushes them
  to connected Envoy instances via ADS (gRPC). Optionally runs with Raft
  consensus for HA (3-5 nodes).
- **Envoy fleet** — stateless Envoy instances connect to the control plane's
  xDS gRPC port (:18000), receive configuration dynamically, and route
  traffic. Horizontally scalable.

In development, the control plane runs as a single process. In production,
the control plane runs as a StatefulSet and Envoy as a DaemonSet or Deployment.

## Components

### cmd/vrata

Entry point. Responsible for:

- Parsing the `--config` flag and loading configuration.
- Instantiating all dependencies (store, xDS server, gateway, k8s watcher).
- Starting the REST API server (:8080) and the xDS gRPC server (:18000).
- Starting the gateway event loop.
- Handling OS signals for graceful shutdown.

### internal/config

Loads and validates the `config.yaml` file. Applies `os.ExpandEnv` to the raw
YAML bytes before unmarshalling so that `${ENV_VAR}` references are resolved.

Key config sections:

- `controlPlane.address` — REST API listen address
- `controlPlane.xdsAddress` — xDS gRPC listen address
- `controlPlane.storePath` — bbolt + Raft data directory
- `controlPlane.raft` — optional Raft HA config
- `log` — format (text/json) and level (debug/info/warn/error)
- `sessionStore` — optional Redis for STICKY balancing

### internal/model

Pure domain types. No business logic, no I/O. Key types:

- **Route** — matching rules + action (forward/redirect/directResponse) + onError fallbacks.
- **RouteGroup** — a named collection of routes with shared matchers.
- **Destination** — an upstream target with endpoints, timeouts, TLS, balancing, circuit breaker, health checks, outlier detection.
- **Listener** — a network entry point with optional TLS/mTLS, GroupIDs for explicit routing topology.
- **Middleware** — CORS, JWT, ExtAuthz, ExtProc, RateLimit, Headers, AccessLog.
- **Snapshot** — immutable point-in-time capture of all configuration.

### internal/store

Pluggable persistence interface. Implementations:

- **bolt** — bbolt embedded KV store (production).
- **memory** — in-memory (testing).
- **raftstore** — Raft wrapper that reads locally and writes through the Raft log.

### internal/api

REST API built on `net/http`. Structured as:

- **router** — registers all routes, applies middleware chain.
- **handlers/** — one handler file per resource (routes, groups, destinations, listeners, middlewares, snapshots, sync, raft, debug).
- **middleware/** — request logging, panic recovery.
- **respond/** — JSON response helpers.

### internal/xds

The Envoy xDS translator and ADS server. Core files:

- **server.go** — gRPC ADS server, `PushSnapshot` entry point, cluster builder (LB policy, circuit breaker, outlier detection, health checks, upstream TLS, HTTP/2), route builder (forward with retry/rewrite/mirror/hash policy, redirect, direct response), listener builder (TLS/mTLS, HCM).
- **helpers.go** — HCM builder with access logs, downstream TLS builder, TLS params mapper, naming helpers, protobuf duration helpers.
- **middlewares.go** — translates Vrata Middleware entities to Envoy HTTP filters: CORS (native), JWT (native), ExtAuthz (native HTTP + gRPC), RateLimit (native local), Headers (native header_mutation), AccessLog (file access logger on HCM), InlineAuthz (Go plugin), XFCC (Go plugin, auto on mTLS).

### internal/gateway

Orchestrator. Subscribes to store events, fetches all resources, merges
dynamically discovered endpoints, calls `xds.PushSnapshot` to push the
full xDS snapshot to all connected Envoy nodes. Every store mutation
triggers a full rebuild.

### internal/raft

Embedded Raft consensus via hashicorp/raft. FSM backed by bbolt. Peer
discovery via static list or DNS. Write-forwarding from followers to leader.

### internal/k8s

Kubernetes EndpointSlice and ExternalName Service watcher. Resolves pod IPs
for destinations with `discovery.type: "kubernetes"`. Triggers gateway
rebuild on changes.

### internal/session

Session store interface and Redis implementation for STICKY balancing
(used by the sticky Envoy Go filter extension).

## Data Flow

```
Operator (API calls)
        │
        ▼
  REST API (net/http, :8080)
        │  validates, writes
        ▼
     Store (bbolt) ───── Raft log (HA) ───── Other CP nodes
        │  publishes StoreEvent
        ▼
    Gateway
        │  rebuilds xDS snapshot
        ▼
   xDS Server (gRPC, :18000)
        │  ADS push
        ▼
  Envoy fleet ←── Users connect here
        │
        ▼
  Destinations (upstream services)
```

## Folder Structure

```
server/
├── cmd/vrata/main.go
├── internal/
│   ├── config/config.go
│   ├── model/                  # Pure domain types
│   ├── store/                  # Store interface + bolt + memory + raftstore
│   ├── api/                    # REST API (handlers, middleware, respond, router)
│   ├── xds/                    # Envoy xDS translator + ADS server
│   ├── gateway/gateway.go      # Store → xDS bridge
│   ├── raft/                   # Raft consensus (FSM, node, peer discovery)
│   ├── k8s/watcher.go          # Kubernetes endpoint discovery
│   └── session/                # Session store interface + Redis
├── proto/                      # gRPC proto definitions (extproc, extauthz)
├── test/e2e/                   # End-to-end tests
├── docs/                       # Generated OpenAPI spec
├── Dockerfile
└── go.mod
```

## What does NOT exist in this branch

The following packages from the main branch were removed:

- `internal/proxy/` — the native Go reverse proxy (replaced by Envoy fleet)
- `internal/sync/` — SSE client for proxy-mode instances (replaced by xDS ADS)
- `internal/proxy/middlewares/` — Go middleware implementations (replaced by Envoy native filters + Go plugins)
- `internal/proxy/celeval/` — CEL compiler/evaluator (moved to extensions/inlineauthz for Envoy)
