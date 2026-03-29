# AGENTS.md — Vrata

Vrata is a programmable HTTP reverse proxy with a REST API. It manages
routes, destinations, listeners, and middlewares, applying changes live
to a fleet of Envoy proxy instances via xDS (ADS/gRPC) — no restarts, no downtime.

## Project structure

Two Go modules in one repository:

- **Server** (`server/`) — the control plane + xDS server
- **Controller** (`clients/controller/`) — Kubernetes Gateway API controller

Plus infrastructure:

- **Extensions** (`extensions/`) — Go filter plugins for Envoy (.so)
- **Helm chart** (`charts/vrata/`) — deploys control plane, Envoy fleet, and controller
- **Documentation website** (`docs/website/`) — Hugo site published to GitHub Pages
- **CI/CD** (`.github/workflows/`) — release binaries, Docker image, Helm OCI, docs deploy

## Server architecture

```
server/
├── cmd/vrata/main.go           # Entry point, --config, dependency wiring
├── internal/
│   ├── config/                 # YAML + os.ExpandEnv config loader
│   ├── api/                    # REST API (net/http, swag-go v2)
│   │   ├── handlers/           # One file per resource
│   │   ├── middleware/         # Logging, recovery
│   │   └── respond/            # JSON response helpers
│   ├── store/                  # Persistence: bolt (prod), memory (test), raftstore (HA)
│   ├── model/                  # Domain types: Route, Group, Destination, Listener, Middleware, Snapshot
│   ├── xds/                    # Envoy xDS translator + gRPC ADS server
│   │   ├── server.go           # ADS server, PushSnapshot, cluster/route/listener builders
│   │   ├── helpers.go          # HCM builder, TLS helpers, naming, protobuf utils
│   │   └── middlewares.go      # Middleware → Envoy HTTP filter translation
│   ├── gateway/                # Watches store, rebuilds xDS snapshot, pushes to Envoy fleet
│   ├── raft/                   # Embedded Raft HA (hashicorp/raft)
│   ├── k8s/                    # EndpointSlice + ExternalName watcher
│   └── session/                # Session store interface + Redis implementation
├── proto/                      # gRPC protobuf (extproc, extauthz)
├── test/e2e/                   # End-to-end tests
└── docs/                       # Generated OpenAPI spec
```

## Extensions (Envoy Go filter plugins)

```
extensions/
├── Dockerfile                  # Builds Envoy image with .so plugins baked in
├── ENVOY_BOOTSTRAP.md          # Example bootstrap config for connecting Envoy to xDS
├── sticky/                     # Redis-backed sticky session routing
│   ├── filter.go               # Reads session cookie, looks up Redis, injects routing header
│   └── go.mod                  # Independent Go module
├── inlineauthz/                # CEL-based inline authorization
│   ├── filter.go               # Evaluates CEL rules against request, deny/allow
│   └── go.mod                  # Independent Go module
└── xfcc/                       # X-Forwarded-Client-Cert injection
    ├── filter.go               # Strips incoming XFCC, injects from verified TLS metadata
    └── go.mod                  # Independent Go module
```

## Controller architecture

```
clients/controller/
├── cmd/controller/main.go      # Entry point, informers, leader election, watch loop
├── internal/
│   ├── config/                 # Controller config loader
│   ├── vrata/                  # Typed HTTP client for Vrata REST API
│   ├── mapper/                 # HTTPRoute/Gateway → Vrata entities (pure, no I/O)
│   ├── reconciler/             # Apply/delete with dependency ordering + refcount
│   ├── batcher/                # Debounce + max batch → snapshot create+activate
│   ├── dedup/                  # Semantic overlap detection (path, headers, methods)
│   ├── status/                 # HTTPRoute status condition writer
│   ├── refgrant/               # ReferenceGrant cross-namespace checker
│   └── metrics/                # 8 Prometheus metrics
├── apis/v1/                    # SuperHTTPRoute CRD types
├── scripts/                    # crdclean + helmwrap for CRD generation pipeline
└── test/e2e/                   # End-to-end tests
```

## Key design decisions

Documented in detail in `SERVER_DECISIONS.md`. Highlights:

- Envoy as the data plane, Vrata as a pure xDS control plane
- REST API unchanged — user doesn't need to know about Envoy
- xDS push on every store change (no explicit snapshot activate step)
- Two-level load balancing with proper sticky at both levels
- Per-entity fault isolation — one broken entity never takes down the control plane
- Cleanup callbacks on routing table swap — no leaked goroutines

## Code conventions

Documented in `CONVENTIONS.md`. Key rules:

- DRY/KISS, explicit dependency injection, error bubbling
- All exported symbols documented, slog only, no globals
- Apache 2.0 license header on all Go files

## Pending work

- `SERVER_TODO.md` — API auth, multi-value matchers, Listener.GroupIDs redesign
- `CONTROLLER_TODO.md` — TLS gap, regex overlap detection
- `ENVOY_XDS.md` — sticky response-side pinning, ExtProc filter, CEL matchers

## Build

```bash
make build          # Server + controller binaries
make test           # All unit tests
make e2e            # All e2e tests (needs running infra)
make server-docs    # Regenerate OpenAPI spec
make docker-build   # Docker image
make deps           # Install dev tools
```

## Dependencies

| Library | Purpose |
|---------|---------|
| `net/http` (stdlib) | REST API |
| `log/slog` (stdlib) | Structured logging |
| `gopkg.in/yaml.v3` | Config parsing |
| `go.etcd.io/bbolt` | Embedded storage |
| `github.com/hashicorp/raft` | HA consensus |
| `github.com/envoyproxy/go-control-plane` | xDS protobuf types + ADS server |
| `google.golang.org/grpc` | gRPC transport for xDS |
| `google.golang.org/protobuf` | Protobuf serialization |
| `github.com/prometheus/client_golang` | Metrics |
| `github.com/redis/go-redis/v9` | Sticky sessions (extensions) |
| `github.com/google/cel-go` | CEL expressions (extensions) |
| `github.com/swaggo/swag/v2` | OpenAPI spec generation |
| `sigs.k8s.io/controller-runtime` | Controller informers + cache |
| `sigs.k8s.io/gateway-api` | HTTPRoute/Gateway types |
