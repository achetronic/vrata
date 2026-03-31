# AGENTS.md — Vrata

Vrata is a programmable HTTP reverse proxy with a REST API. It manages
routes, destinations, listeners, and middlewares, applying changes live
to connected proxy instances via SSE — no restarts, no downtime.

## Project structure

Two Go modules in one repository:

- **Server** (`server/`) — the proxy + control plane
- **Controller** (`clients/controller/`) — Kubernetes Gateway API controller

Plus infrastructure:

- **Helm chart** (`charts/vrata/`) — deploys control plane, proxy fleet, and controller
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
│   ├── proxy/                  # Native reverse proxy: router, balancers, circuit breaker, health, outlier, metrics
│   │   ├── middlewares/        # CORS, JWT, ExtAuthz, ExtProc, RateLimit, Headers, AccessLog, InlineAuthz
│   │   └── celeval/            # CEL compiler + evaluator (request matching, body access, TLS cert access)
│   ├── gateway/                # Watches store, rebuilds routing table, reconciles listeners
│   ├── raft/                   # Embedded Raft HA (hashicorp/raft)
│   ├── k8s/                    # EndpointSlice + ExternalName watcher
│   ├── sync/                   # SSE client for proxy-mode instances
│   └── session/                # Session store interface + Redis implementation
├── proto/                      # gRPC protobuf (extproc, extauthz)
├── test/e2e/                   # End-to-end tests
└── docs/                       # Generated OpenAPI spec
```

## Controller architecture

```
clients/controller/
├── cmd/controller/main.go      # Entry point, informers, leader election, watch loop
├── internal/
│   ├── config/                 # Controller config loader
│   ├── vrata/                  # Typed HTTP client for Vrata REST API
│   ├── mapper/                 # HTTPRoute/Gateway/XBackend/XAccessPolicy → Vrata entities (pure, no I/O)
│   ├── reconciler/             # Apply/delete with dependency ordering + refcount
│   ├── batcher/                # Debounce + max batch → snapshot create+activate
│   ├── dedup/                  # Semantic overlap detection (path, headers, methods)
│   ├── status/                 # HTTPRoute + XBackend + XAccessPolicy status condition writer
│   ├── refgrant/               # ReferenceGrant cross-namespace checker
│   └── metrics/                # 8 Prometheus metrics
├── apis/v1/                    # SuperHTTPRoute CRD types
├── apis/agentic/               # XBackend + XAccessPolicy CRD types (Kube Agentic Networking)
├── scripts/                    # crdclean + helmwrap for CRD generation pipeline
└── test/e2e/                   # End-to-end tests
```

## Key design decisions

Documented in detail in `SERVER_DECISIONS.md`. Highlights:

- Native Go proxy (net/http + httputil.ReverseProxy), no Envoy dependency
- SSE snapshots instead of xDS — simple JSON push, no protobuf negotiation
- Two-level load balancing with proper sticky at both levels
- httpsnoop for all ResponseWriter interception — never manual wrappers
- Per-entity fault isolation — one broken route never takes down the proxy
- Cleanup callbacks on routing table swap — no leaked goroutines

## Code conventions

Documented in `CONVENTIONS.md`. Key rules:

- DRY/KISS, explicit dependency injection, error bubbling
- All exported symbols documented, slog only, no globals
- httpsnoop for ResponseWriter, no external routers
- Apache 2.0 license header on all Go files

## Pending work

- `SERVER_TODO.md` — API auth, multi-value matchers, proxy fleets
- `CONTROLLER_TODO.md` — TLS gap, regex overlap detection, agentic networking support

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

| Library                               | Purpose                      |
| ------------------------------------- | ---------------------------- |
| `net/http` (stdlib)                   | REST API + proxy             |
| `log/slog` (stdlib)                   | Structured logging           |
| `gopkg.in/yaml.v3`                    | Config parsing               |
| `go.etcd.io/bbolt`                    | Embedded storage             |
| `github.com/hashicorp/raft`           | HA consensus                 |
| `github.com/google/cel-go`            | CEL expressions              |
| `github.com/prometheus/client_golang` | Metrics                      |
| `github.com/felixge/httpsnoop`        | ResponseWriter interception  |
| `github.com/redis/go-redis/v9`        | Sticky sessions              |
| `github.com/swaggo/swag/v2`           | OpenAPI spec generation      |
| `sigs.k8s.io/controller-runtime`      | Controller informers + cache |
| `sigs.k8s.io/gateway-api`             | HTTPRoute/Gateway types      |
