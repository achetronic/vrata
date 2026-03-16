# TODO - Rutoso

## In Progress

_(nothing)_

## Pending

### Housekeeping
- [ ] Add authentication to the REST API
- [ ] Write unit and integration tests
- [ ] HA storage: document shared-volume / Litestream pattern for multi-replica deployments
- [ ] Update `ARCHITECTURE.md` to reflect current package structure

### xDS builder — wire actual Envoy HTTP filter objects
Filters are stored and referenced from Listeners, but the builder does not yet
turn `FilterIDs` into real Envoy HTTP filter objects.

- [ ] In `xds/builder.go`, for each `filterID` in `listener.FilterIDs`, look up the filter
  in the filters slice passed to `BuildSnapshot`
- [ ] Build the corresponding `envoy_listener.Filter` based on `filter.Type`:
  - `cors` → `envoy.filters.http.cors`
  - `jwt` → `envoy.filters.http.jwt_authn`
  - `ext_authz` → `envoy.filters.http.ext_authz`
  - `ext_proc` → `envoy.filters.http.ext_proc`
- [ ] Append to the filter chain before the router filter (order: CORS → JWT → ext_authz → ext_proc → router)

### xDS builder — FilterOverrides merge logic (per_filter_config)
- [ ] When building a route's xDS representation, merge `RouteGroup.FilterOverrides` (base)
  with `Route.FilterOverrides` (wins on collision) to produce `per_filter_config`
- [ ] Serialize each override into the appropriate Envoy typed config proto

### TLS termination in xDS builder
- [ ] Decide cert delivery mechanism: file mount (static `certPath`/`keyPath`) vs SDS
- [ ] Wire `model.Listener.TLS` into Envoy's `DownstreamTlsContext` in the builder

### Cluster — first-class entity
DONE — implemented as `Destination` entity. See Done section below.

### RouteAction — fill missing fields
DONE — all route action fields implemented. See Done section below.

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
