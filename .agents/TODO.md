# TODO - Rutoso

## In Progress

_(nothing)_

## Pending

### Housekeeping
- [ ] Add authentication to the REST API
- [ ] Write unit and integration tests
- [ ] HA storage: document shared-volume / Litestream pattern for multi-replica deployments
- [ ] Update `ARCHITECTURE.md` to reflect `model` package as canonical type home (separate from `gateway`)

### Listener — first-class entity
Currently hardcoded in `xds/builder.go` (port 80, no filters, no TLS). Make it a
managed entity with full CRUD, store persistence, and xDS push.

- [ ] Define `model.Listener` — address, port, and HTTP filter chain config:
  - `id`, `name`
  - `address` (default `0.0.0.0`), `port`
  - `filters []ListenerFilter` — ordered list of HTTP filters to activate:
    - `cors` — allowed origins, methods, headers, expose headers, max age
    - `jwt` — list of providers (name, issuer, JWKS URI/inline, audiences,
      forward token, claim-to-header mappings)
    - `ext_authz` — gRPC or HTTP endpoint, timeout, include/exclude headers,
      failure mode (allow/deny), per-route override support
    - `ext_proc` — gRPC endpoint, processing mode (request/response
      headers/body/trailers), timeout, mutation rules
  - `tls` (optional) — TLS termination config: cert/key paths or SDS source,
    min/max protocol version, cipher suites
- [ ] Add `Listener` CRUD to `store.Store` interface
- [ ] Implement in `store/bolt` — new `listeners` bucket
- [ ] Implement in `store/memory`
- [ ] Add REST handlers: `GET/POST /api/v1/listeners`,
  `GET/PUT/DELETE /api/v1/listeners/{listenerId}`
- [ ] Update `gateway.Rebuild()` to fetch listeners from store and pass to builder
- [ ] Update `xds/builder.go` — generate one Envoy Listener per `model.Listener`;
  build the HCM filter chain from `listener.Filters` in declared order;
  wire per-route filter config from Route/Group policies (see below)
- [ ] Per-route filter config — routes and groups can carry overrides for filters
  registered in the listener:
  - `cors` override per route (allowed origins, methods)
  - `jwt` override per route (disable, or pick specific provider)
  - `ext_authz` override per route (disable, add context extensions)
  - `ext_proc` override per route (disable, override processing mode)

### Cluster — first-class entity
Currently auto-generated from `Backend.Host`/`Backend.Port` with no configurability.
Make it a managed entity so upstream TLS, circuit breaking, and retries are controllable.

- [ ] Define `model.Cluster` — upstream connection config:
  - `id`, `name`
  - `endpoints []Endpoint` — list of `{ host, port, weight }`
  - `tls` (optional) — upstream TLS: SNI, CA cert path or SDS, mTLS cert/key,
    skip verify flag
  - `connectTimeout` — default `5s`
  - `circuitBreaker` (optional) — max connections, max pending requests,
    max requests, max retries
  - `healthCheck` (optional) — HTTP or gRPC health check path, interval,
    timeout, unhealthy/healthy threshold
  - `lbPolicy` — `ROUND_ROBIN` (default), `LEAST_REQUEST`, `RANDOM`, `RING_HASH`
- [ ] Decouple `Backend` from inline host/port — `Route.Backends` becomes
  `[]BackendRef { clusterID, weight }` (reference to a Cluster by ID)
- [ ] Add `Cluster` CRUD to `store.Store` interface
- [ ] Implement in `store/bolt` — new `clusters` bucket
- [ ] Implement in `store/memory`
- [ ] Add REST handlers: `GET/POST /api/v1/clusters`,
  `GET/PUT/DELETE /api/v1/clusters/{clusterId}`
- [ ] Update `gateway.Rebuild()` to fetch clusters from store
- [ ] Update `xds/builder.go` — build Envoy Cluster from `model.Cluster`;
  apply TLS, circuit breaker, health check, lb policy

### RouteAction — fill missing fields
- [ ] `rewrite` — `prefixRewrite` or `regexRewrite` (pattern + substitution)
- [ ] `hostRewrite` — override the Host header sent upstream
- [ ] `retryPolicy` — retry-on conditions, num retries, per-try timeout
- [ ] `timeout` — total route timeout
- [ ] `requestMirror` — shadow traffic to a second cluster at a given percentage

## Done

- [x] Scaffold project structure: `server/` layout, `go.mod`, `Makefile`, `Dockerfile`, `config.yaml`
- [x] Implement `internal/config`: Config struct, `Load()` with `os.ExpandEnv`, defaults, `--config` flag wiring in main
- [x] Define domain model: `RouteGroup`, `Route`, `Backend`, `MatchRule`, `HeaderMatcher` in `internal/model`
- [x] Domain errors: `ErrNotFound`, `ErrDuplicateRoute`, `ErrDuplicateGroup`, `ErrInvalidWeight`
- [x] Implement `Store` interface (`internal/store/store.go`) with `StoreEvent` subscription pattern
- [x] Implement in-memory store (`internal/store/memory`) — thread-safe, pub/sub events
- [x] Duplicate route detection in store (based on path specifier equality)
- [x] Implement REST API: router, middleware (logger + recovery), `respond` helpers, dependency injection
- [x] Implement route group handlers (list, get, create, update, delete)
- [x] Implement route handlers (list, get, create, update, delete) with weight validation
- [x] Implement xDS server (`internal/xds/server.go`): gRPC, snapshot cache, monotonic version counter
- [x] Implement xDS snapshot builder (`internal/xds/builder.go`): RouteGroups → Listener + RouteConfig + Clusters + Endpoints
- [x] Implement `internal/gateway`: subscribes to store events, rebuilds and pushes xDS snapshots
- [x] Makefile targets: `build`, `run`, `run-dev`, `docker-build`, `docker-push`, `clean`
- [x] `run-dev` documented: override `XDS_ADDR` so Kubernetes-deployed Envoys can reach local machine
- [x] `main.go`: wires config, store, xDS server, gateway, HTTP server, graceful shutdown
- [x] `go build ./...` and `go vet ./...` pass with zero errors
- [x] Rename API paths: `/api/v1/route-groups` → `/api/v1/groups`, nested routes kept under `/{groupId}/routes`
- [x] Implement persistent store: bbolt (`store/bolt`), single-file DB, full `Store` interface, `--store-path` flag
- [x] OpenAPI docs: swag-go v2 annotations on all 10 handlers, `docs/` generated, Swagger UI at `/api/v1/docs/`
- [x] **Data model redesign**: Routes promoted to independent first-class entities
  - `model/route.go`: Route no longer has GroupID; added `Ports []uint32` and `QueryParams []QueryParamMatcher` to MatchRule
  - `model/group.go`: RouteGroup now holds `RouteIDs []string` (references) instead of embedded routes; added group-level matchers (`PathPrefix`, `Hostnames`, `Headers`)
  - `store/store.go`: Store interface updated — route operations are top-level (no groupID parameter)
  - `store/bolt/bolt.go`: Migrated to new interface; two flat buckets (`routes`, `groups`), old sub-key pattern removed
  - `store/memory/memory.go`: Updated to new interface
  - `gateway/gateway.go`: `Rebuild()` resolves route IDs from store before calling builder
  - `xds/builder.go`: `BuildSnapshot(version, groups, routes)` — receives pre-resolved routes per group; merges group-level matchers on top of route matchers
  - `handlers/routes.go`, `handlers/groups.go`, `router.go`: Updated to top-level `/api/v1/routes` paths
  - Swagger docs regenerated via `make docs`
  - `go build ./...` and `go vet ./...` pass clean
- [x] **PathRegex on RouteGroup**: added `PathRegex` field with full 8-case composition logic in builder
  - Group PathPrefix + any route path specifier → literal concatenation (existing behavior)
  - Group PathRegex + route PathRegex → `(?:group)(?:route)` composition
  - Group PathRegex + route Path/PathPrefix → `(?:group)(?:QuoteMeta(literal))` safe composition
  - Group PathRegex + no route path → group regex is the full match
  - All cases documented in `buildRouteMatch` with ASCII tables
