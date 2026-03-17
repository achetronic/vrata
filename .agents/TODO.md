# TODO - Rutoso

## In Progress

_(nothing)_

## Pending

### HITO MAYOR: Rutoso como proxy nativo Go (eliminar dependencia de Envoy)
Rutoso deja de ser un control plane para Envoy y pasa a ser el proxy en sí.
Mismo binario, misma API REST, mismas entidades (Destination, Listener, Route,
RouteGroup, Middleware). Por debajo, Go nativo haciendo reverse proxy.

Principios:
- Las 5 entidades se mantienen: Destination, Listener, Route, RouteGroup, Middleware
- La API REST no cambia. Lo que cambia es que ya no genera protos xDS, sino que
  reconfigura el proxy interno en caliente
- Reload sin corte: conexiones activas terminan con config vieja, nuevas usan la nueva
- Un solo binario. Sin Envoy. Sin protos. Sin traducciones.

Features requeridas (paridad + mejoras):
- Listener: address:port, TLS downstream con reload de certs, HTTP/2, access log,
  server name, max request headers
- Destination: host:port, TLS upstream (tls/mtls con system CA por defecto),
  HTTP/2, DNS refresh, health checks, circuit breaker, outlier detection,
  slow start, max requests per connection, Kubernetes EDS
- Route: path/prefix/regex match, methods, headers, query params, gRPC match,
  forward (weighted backends, sticky sessions con ring hash/maglev), redirect,
  direct response, timeouts, retry con backoff, rewrite (path/regex/host),
  mirror, websocket upgrade, max gRPC timeout, internal redirects
- RouteGroup: path prefix/regex composition, hostnames, headers, retry default,
  include attempt count, middleware/override inheritance
- Middleware (el sistema entero cambia — ya no mapea a protos Envoy):
  - CORS: implementación directa, no depende de filter_enabled ni per_filter_config
  - OAuth2/Auth: middleware de alto nivel. El usuario pone destinationId + path +
    signin path. Rutoso sabe qué headers mandar, recibir, pasar al upstream.
    Sin allowedHeaders/allowedClientHeaders/headersToAdd manuales.
  - Headers: add/remove request/response headers
  - Rate limit: embebido (token bucket por ruta/IP/header) o con servicio externo
  - ExtProc: compatibilidad con el proto de ext_proc de Envoy para reutilizar
    procesadores existentes
  - WASM: ejecutar filtros WASM via wasmtime/wazero embebido
  - JWT: validación directa en Go (lestrrat-go/jwx), sin servicio externo

Arquitectura:
- Reemplazar internal/xds/ por internal/proxy/ con el reverse proxy Go
- El gateway pasa de generar snapshots xDS a reconfigurar el proxy en caliente
- internal/proxy/router.go: tabla de routing con hot-swap atómico
- internal/proxy/middleware.go: chain de middlewares como http.Handler wrappers
- internal/proxy/balancer.go: weighted random, ring hash, maglev, least request
- internal/proxy/health.go: health checks activos + outlier detection
- internal/proxy/circuit.go: circuit breaker por destination
- El 80% del código actual (modelo, store, API, handlers, k8s watcher, bbolt)
  no cambia. Solo se reemplaza la capa de proxy.

Tasks:
- [ ] Diseñar el paquete internal/proxy/ con el reverse proxy base
- [ ] Implementar hot-reload de config (atomic swap de routing table)
- [ ] Implementar balancer (weighted, ring hash, maglev, least request, random)
- [ ] Implementar health checks + outlier detection
- [ ] Implementar circuit breaker
- [ ] Implementar TLS upstream/downstream con cert reload
- [ ] Implementar middleware chain con enable/disable por ruta
- [ ] Implementar middlewares: CORS, OAuth2, Headers, Rate limit, JWT
- [ ] Implementar WebSocket proxying
- [ ] Implementar HTTP/2 upstream/downstream
- [ ] Implementar access log
- [ ] Implementar retry con backoff
- [ ] Implementar path rewrite (prefix, regex, host)
- [ ] Implementar request mirror
- [ ] Adaptar gateway.go para reconfigurar proxy en vez de generar xDS
- [ ] Eliminar dependencia de go-control-plane
- [ ] Tests de paridad con la versión Envoy

### TLS (versión Envoy — pendiente mientras exista)
- [ ] TLS upstream (Destination): wire `DestinationOptions.TLS` into UpstreamTlsContext / transport_socket in builder. Critical for ExtAuthz and any external service over TLS.
- [ ] TLS downstream (Listener): wire `Listener.TLS` into DownstreamTlsContext in builder.
- [ ] Multiple filter chains: support SNI routing (one cert per domain) via multiple FilterChains on a single Listener.

### HA — Embedded distributed store (Hashicorp Raft + bbolt)
Rutoso must run in HA with N replicas where any node can die without losing
configuration and Envoy always has a Rutoso available for xDS.

Design:
- New package `internal/raft/` wrapping `hashicorp/raft`.
- Each Rutoso replica runs an embedded Raft node.
- Writes (POST/PUT/DELETE) go through Raft consensus: proposed as a command,
  replicated to quorum, then applied to each node's local bbolt via the FSM.
- Reads (GET, xDS) go directly to the local bbolt — no Raft round-trip.
- New `store/raft/raft.go` implementing store.Store: delegates reads to
  bolt store, writes to Raft. Subscribe emits events from FSM apply.
- Leader election is automatic (Raft). Non-leader nodes redirect writes
  to the leader (307 or proxy).
- Config in config.yaml:
  ```yaml
  cluster:
    nodeId: "rutoso-0"
    bindAddress: ":7000"
    peers:
      - "rutoso-0=10.0.0.1:7000"
      - "rutoso-1=10.0.0.2:7000"
      - "rutoso-2=10.0.0.3:7000"
    dataDir: "/data/raft"
  ```
- If `cluster` is not configured, Rutoso runs in single-node mode with
  plain bbolt (current behaviour, for development).
- Raft snapshots use bbolt's consistent read to serialize the full DB.
- All existing code (handlers, gateway, builder, xDS) is unchanged —
  only the store.Store implementation injected in main.go changes.

Tasks:
- [ ] Add `hashicorp/raft` and `hashicorp/raft-boltdb` to go.mod
- [ ] Implement `internal/raft/fsm.go`: FSM that applies commands to bolt store
- [ ] Implement `internal/raft/cluster.go`: Raft node lifecycle, transport, snapshot store
- [ ] Implement `store/raft/raft.go`: store.Store wrapper (reads → bolt, writes → Raft)
- [ ] Add `cluster` config block to config.yaml and `internal/config`
- [ ] Update `main.go`: if cluster configured, use raft store; else use bolt
- [ ] Leader detection: non-leader nodes redirect or proxy writes to leader
- [ ] Snapshot/restore: serialize and restore full bbolt state
- [ ] Integration test: 3-node cluster, kill leader, verify config survives

### Housekeeping
- [ ] Add authentication to the REST API
- [ ] Write unit and integration tests
- [ ] Update `ARCHITECTURE.md` to reflect current package structure

## Done

- [x] **Wire methods, queryParams, hashPolicy in xDS builder** (cd083bb)
  - `buildRouteMatch`: maps `MatchRule.Methods` to `:method` header matcher (exact for 1, regex OR for multiple)
  - `buildRouteMatch`: maps `MatchRule.QueryParams` via `buildQueryParamMatcher` (exact, regex, presence-only)
  - `HashPolicy` moved from `BackendRef` to `ForwardAction`
  - `applyHashPolicy`: maps header, cookie (with TTL), and sourceIP to Envoy `RouteAction.hash_policy`
  - `MatchRule.Ports` removed — Envoy has no port matching in RouteMatch

- [x] **All entity fields implemented and wired in builder** (89bf667)
  - Route: websocket, maxGrpcTimeout, internalRedirect, gRPC match
  - Group: retryDefault, includeAttemptCount
  - Destination: HTTP/2, DNS refresh/family, maxRequestsPerConnection, slowStart
  - Listener: accessLog, HTTP/2, listenerFilters (tls_inspector/proxy_protocol/original_dst), serverName, maxRequestHeadersKB

- [x] **Full middleware wiring with real Envoy configs** (1fc6db4)
  - CORS, JWT, ExtAuthz, ExtProc: real typed configs replacing stubs
  - RateLimit and Headers: new middleware types
  - per_filter_config: group base + route wins merge logic

- [x] **Rename Filter to Middleware, auto-register in HCM** (3c1da88)
  - Filter entity renamed to Middleware across entire codebase
  - API paths: /api/v1/filters → /api/v1/middlewares
  - Route and Group gain `middlewareIds` — users attach middlewares to routes/groups
  - Listener.FilterIDs removed — users never touch it
  - Builder auto-collects middleware IDs from routes/groups and registers in HCM
  - Per-route disable for middlewares not active on that route

- [x] **CORS fix: empty Cors{} in HCM, CorsPolicy in per-route** (cc4f40e)
  - CORS filter_enabled set to 100% so Envoy intercepts preflights

- [x] **README** (9b3e9e8)
  - Architecture diagram (mermaid), dev env, build, deploy, k8s discovery

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
- [x] OpenAPI docs: swag-go v2 annotations on all handlers, `docs/` generated, Swagger UI at `/api/v1/docs/`
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
- [x] **Filter entity** — Filter as independent first-class entity (consistent with Route/Group pattern)
  - `model/filter.go`: `Filter`, `FilterConfig`, `FilterType`, `JWTConfig`, `ExtAuthzConfig`, `ExtProcConfig`, `CORSConfig`, `FilterOverride`
  - `model/listener.go`: `Listener` with `FilterIDs []string` and `TLS *ListenerTLS`
  - `model/route.go`, `model/group.go`: `FilterOverrides map[string]FilterOverride` added to both
  - `store/store.go`: `SaveFilter`, `GetFilter`, `ListFilters`, `DeleteFilter`, `SaveListener`, `GetListener`, `ListListeners`, `DeleteListener` added to interface
  - `store/bolt/bolt.go`: `filters` and `listeners` buckets, all 8 new CRUD methods implemented
  - `store/memory/memory.go`: implemented all 8 new methods; added `filters` and `listeners` maps to struct
  - `handlers/filters.go`, `handlers/listeners.go`: full CRUD handlers with swag annotations
  - `router.go`: `/api/v1/filters` and `/api/v1/listeners` routes registered
  - `gateway/gateway.go`: `Rebuild()` fetches listeners and filters from store, passes to builder
  - `xds/builder.go`: `BuildSnapshot(version, listeners, filters, groups, routes)` new signature; builds real Envoy Listener per `model.Listener`; fallback to hardcoded `rutoso_listener 0.0.0.0:80` if no listeners in store
  - Swagger docs regenerated via `make docs`
  - `go build ./...` and `go vet ./...` pass clean

- [x] **Destination entity** — `model.Destination` with `DestinationOptions` (TLS, LB, circuit breaker, health check, outlier detection, discovery)
  - `model/destination.go`: `Destination`, `DestinationOptions`, `TLSOptions`, `BalancingOptions`, `CircuitBreakerOptions`, `HealthCheckOptions`, `OutlierDetectionOptions`, `Discovery`; `BackendRef` and `HashPolicy` defined here (used by Route)
  - `model/route.go`: `Route.Backends` changed from `[]Backend` to `[]BackendRef`; old `Backend` struct removed
  - `store/store.go`: `SaveDestination`, `GetDestination`, `ListDestinations`, `DeleteDestination` added to interface
  - `store/bolt/bolt.go`: `destinations` bucket + 4 CRUD methods
  - `store/memory/memory.go`: `destinations` map + 4 CRUD methods
  - `handlers/destinations.go`: full CRUD handlers with swag annotations
  - `router.go`: `GET/POST /api/v1/destinations` and `GET/PUT/DELETE /api/v1/destinations/{destinationId}` registered
  - `gateway/gateway.go`: `Rebuild()` fetches destinations from store, passes to `BuildSnapshot`
  - `xds/builder.go`: `BuildSnapshot` signature updated; `buildClusterFromDestination` and `buildEndpointFromDestination` implemented; EDS/STATIC/STRICT_DNS auto-derivation via `clusterTypeFor`; old `buildCluster(model.Backend)` and `buildEndpoint(model.Backend)` removed
  - `go build ./...` and `go vet ./...` pass clean
  - Swagger docs regenerated

- [x] **Route action modes + full RouteAction fields**
  - `model/route.go`: `Route` now supports three mutually exclusive action modes: `Backends` (forward), `Redirect`, `DirectResponse`
  - New model types: `RouteTimeouts`, `RouteRetry`, `RetryCondition`, `RetryBackoff`, `RouteRewrite`, `RewriteRegex`, `RouteRedirect`, `RouteDirectResponse`, `RouteMirror`
  - `model/errors.go`: `ErrConflictingAction` added
  - `xds/builder.go`: `buildRouteAction` dispatches to `buildForwardAction`, `buildRedirectAction`, `buildDirectResponseAction`; helpers `applyTimeouts`, `applyRetryPolicy`, `applyRewrite`, `applyMirror` wired into `RouteAction`; `retryConditionMap` translates semantic names to Envoy `retry_on` values; added `typev3` and `strings` imports
  - `handlers/routes.go`: `validateRouteAction` enforces mutual exclusivity on create and update (400 if violated)
  - `go build ./...` and `go vet ./...` pass clean
  - Swagger docs regenerated via `make docs`

- [x] **ForwardAction refactor** — forwarding behaviour grouped under `Route.Forward`
  - `model/route.go`: `Backends`, `Timeouts`, `Retry`, `Rewrite`, `Mirror` moved into new `ForwardAction` struct; `Route` now has `Forward *ForwardAction`, `Redirect *RouteRedirect`, `DirectResponse *RouteDirectResponse`
  - `xds/builder.go`: `buildForwardAction` reads from `r.Forward`; `buildRouteAction` checks `r.Forward != nil` instead of `default`
  - `handlers/routes.go`: `validateRouteAction` checks `route.Forward != nil`
  - `go build ./...` and `go vet ./...` pass clean
  - Swagger docs regenerated via `make docs`

- [x] **Debug endpoint: GET /api/v1/debug/xds/snapshot**
  - `xds/server.go`: `Snapshot()` method — returns `map[string]cachev3.ResourceSnapshot` keyed by Envoy node ID
  - `handlers/debug.go`: `GetXDSSnapshot` serialises each resource with `protojson`; response is `nodeID → resourceType → []resource`
  - `handlers/deps.go`: `XDSServer *xds.Server` added to `Dependencies`
  - `api/router.go`: `NewRouter` updated to accept `*xds.Server`; route registered
  - `cmd/rutoso/main.go`: `NewRouter` call updated to pass `xdsSrv`
  - `go build ./...` and `go vet ./...` pass clean
  - Swagger docs regenerated via `make docs`

- [x] **Kubernetes EndpointSlice watcher** (`internal/k8s/watcher.go`)
  - `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery` added to `go.mod`; `go mod tidy` applied
  - `internal/k8s/watcher.go`: `Watcher` struct with `Dependencies` (Store, Client, Logger, Rebuild func); `Run` subscribes to store, reconciles on Destination events; `reconcileWatches` diffs active informers vs desired EDS Destinations; `watchEndpointSlices` starts a per-service `SharedInformerFactory` filtered by `kubernetes.io/service-name` label; calls `Rebuild` on add/update/delete
  - `parseFQDN` extracts service+namespace from `<svc>.<ns>.svc.cluster.local` host field
  - `gateway/gateway.go`: `Rebuild(ctx) error` public method added as wrapper around `rebuild`
  - `cmd/rutoso/main.go`: `buildK8sClient()` tries in-cluster config then `~/.kube/config`; watcher instantiated and started in goroutine if client available; k8s failure is non-fatal (logged as Warn, watcher disabled)
  - `go build ./...` and `go vet ./...` pass clean
