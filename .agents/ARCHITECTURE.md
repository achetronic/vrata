# Architecture - Vrata

## Overview

Vrata is a control plane for Envoy proxies. It exposes a REST API that lets operators
define route groups and routes with rich matching rules, and translates that configuration
into xDS resources that are pushed live to connected Envoy instances via gRPC streaming.

The system is designed to be horizontally scalable. Multiple Vrata replicas can run
simultaneously, sharing state through a pluggable persistence layer. Every change to the
store triggers a snapshot rebuild that is pushed to all connected Envoys without requiring
a proxy restart.

Vrata intentionally has no UI of its own. It is the backend layer. A UI, a CLI, or any
other client talks to the REST API.

## Components

### cmd/vrata
Entry point. Responsible for:
- Parsing the `--config` flag and loading configuration.
- Instantiating all dependencies (store, xDS cache, logger).
- Starting the REST API server and the xDS gRPC server.
- Handling OS signals for graceful shutdown.

### internal/config
Loads and validates the `config.yaml` file. Applies `os.ExpandEnv` to the raw YAML
bytes before unmarshalling so that any `${ENV_VAR}` references are resolved. Owns the
`Config` struct that all other components receive at startup.

### internal/model
Pure domain types. No business logic, no I/O. Key types:

- **RouteGroup** — a named collection of routes with shared attributes (prefix, headers,
  hostnames). Acts as a namespace and a policy umbrella for its children.
- **Route** — a single routing rule inside a group. Defines match criteria (path, method,
  headers, query params) and one or more weighted backends for traffic splitting.
- **Backend** — a destination: host, port, protocol, and weight (for canary/A-B splits).
- **MatchRule** — the combination of fields that uniquely identifies a route within a group.
  Used for duplicate detection.

### internal/store
Pluggable persistence interface. All reads and writes go through this interface; components
never touch storage directly.

```go
type Store interface {
    GetGroups(ctx context.Context) ([]model.RouteGroup, error)
    GetGroup(ctx context.Context, id string) (model.RouteGroup, error)
    CreateGroup(ctx context.Context, g model.RouteGroup) error
    UpdateGroup(ctx context.Context, g model.RouteGroup) error
    DeleteGroup(ctx context.Context, id string) error

    GetRoutes(ctx context.Context, groupID string) ([]model.Route, error)
    GetRoute(ctx context.Context, groupID, routeID string) (model.Route, error)
    CreateRoute(ctx context.Context, r model.Route) error
    UpdateRoute(ctx context.Context, r model.Route) error
    DeleteRoute(ctx context.Context, groupID, routeID string) error

    Subscribe(ctx context.Context) (<-chan StoreEvent, error)
}
```

The `Subscribe` method allows the gateway layer to react to changes in real time.
Initial implementation TBD (see DECISIONS.md). Requirements: simple backup, HA-capable
replication across Vrata replicas.

### internal/api
REST API built on `net/http`. Structured as:
- **router** — registers all routes, applies middleware chain.
- **handlers/** — one handler file per resource (groups, routes). Each handler is a
  method on a struct that holds a `Dependencies` reference.
- **middleware/** — logging (request/response), panic recovery, (future) authentication.

All handlers are annotated for swag-go v2 to generate the OpenAPI 3.1 spec.

### internal/xds
gRPC server implementing the xDS protocol via `go-control-plane`. Owns:
- The snapshot cache (one snapshot per Envoy node ID).
- The snapshot builder that translates `model.RouteGroup` + `model.Route` objects into
  Envoy `Listener`, `RouteConfiguration`, `Cluster`, and `ClusterLoadAssignment` resources.
- The version counter (monotonically incrementing, required by xDS protocol).

### internal/gateway
The orchestrator. Subscribes to store events and, on any change, triggers a full snapshot
rebuild and pushes the new snapshot to the xDS cache. This is the only component that
couples the store and the xDS server together.

## Data Flow

```
Client (UI / CLI / curl)
        │
        ▼
  REST API (net/http)
        │  validates, checks duplicates, writes
        ▼
     Store  ──────────────────────────────────────────┐
        │  publishes StoreEvent                        │
        ▼                                             │
    Gateway                                    (backup / HA sync)
        │  rebuilds snapshot                          │
        ▼                                         Other Vrata
   xDS Server                                     replicas
        │  streams updated snapshot
        ▼
  Envoy proxies (0..N)
```

## Boundaries and Interfaces

| Component | Exposes | Consumes |
|-----------|---------|----------|
| config | `Config` struct | YAML file + env vars |
| model | domain types | nothing |
| store | `Store` interface | persistence backend |
| api | HTTP endpoints | Store interface |
| xds | gRPC xDS endpoint | model types |
| gateway | internal only | Store.Subscribe, xds cache |

## Folder Structure

```
server/
├── cmd/
│   └── vrata/
│       └── main.go               # Wiring, startup, signal handling
├── internal/
│   ├── config/
│   │   └── config.go             # Config struct, Load() function
│   ├── model/
│   │   ├── group.go              # RouteGroup, MatchRule
│   │   └── route.go              # Route, Backend
│   ├── store/
│   │   ├── store.go              # Store interface + StoreEvent type
│   │   └── memory/               # In-memory implementation (initial / testing)
│   │       └── memory.go
│   ├── api/
│   │   ├── router.go             # Route registration, middleware chain
│   │   ├── handlers/
│   │   │   ├── groups.go         # CRUD for RouteGroups
│   │   │   └── routes.go         # CRUD for Routes within a group
│   │   └── middleware/
│   │       ├── logger.go         # Request logging via slog
│   │       └── recovery.go       # Panic recovery → 500 JSON response
│   ├── xds/
│   │   ├── server.go             # gRPC server setup
│   │   ├── cache.go              # Snapshot cache wrapper
│   │   └── builder.go            # model → Envoy xDS resources translation
│   └── gateway/
│       └── gateway.go            # Store subscriber + snapshot push orchestrator
├── docs/                         # Generated by swag-go v2 (do not edit manually)
├── Dockerfile
├── Makefile
└── config.example.yaml
```
