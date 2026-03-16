# TODO - Rutoso

## In Progress

_(nothing)_

## Pending

### Route — new fields
- [ ] WebSocket upgrade: `ForwardAction.Websocket bool` → RouteAction.upgrade_configs
- [ ] Max gRPC timeout: `ForwardAction.MaxGRPCTimeout string` or in `RouteTimeouts` → RouteAction.max_grpc_timeout
- [ ] Internal redirects: `ForwardAction.InternalRedirect *InternalRedirectPolicy` → RouteAction.internal_redirect_policy
- [ ] gRPC match: `MatchRule.GRPC bool` → RouteMatch.grpc

### Group — new fields
- [ ] Default retry policy: `RouteGroup.RetryDefault *RouteRetry` → VirtualHost.retry_policy (route overrides if set)
- [ ] Include request attempt count: `RouteGroup.IncludeAttemptCount bool` → VirtualHost.include_request_attempt_count

### Destination — new fields
- [ ] HTTP/2 upstream: `DestinationOptions.HTTP2 bool` → Cluster.http2_protocol_options (required for gRPC backends)
- [ ] DNS refresh rate: `DestinationOptions.DNSRefreshRate string` → Cluster.dns_refresh_rate (STRICT_DNS only)
- [ ] DNS lookup family: `DestinationOptions.DNSLookupFamily string` (AUTO/V4_ONLY/V6_ONLY) → Cluster.dns_lookup_family
- [ ] Max requests per connection: `DestinationOptions.MaxRequestsPerConnection uint32` → Cluster.max_requests_per_connection
- [ ] Slow start: `DestinationOptions.SlowStart *SlowStartOptions` → Cluster.slow_start_config
- [ ] TLS upstream: wire existing `DestinationOptions.TLS` into UpstreamTlsContext / transport_socket in builder

### Listener — new fields
- [ ] Access log: `Listener.AccessLog *AccessLogConfig` → HCM.access_log (file or stdout, format template)
- [ ] HTTP/2 on listener: `Listener.HTTP2 bool` → HCM.http2_protocol_options / codec_type AUTO (required for gRPC clients)
- [ ] Listener filters TCP: `Listener.ListenerFilters []ListenerFilter` → tls_inspector, proxy_protocol, etc.
- [ ] TLS downstream: wire existing `Listener.TLS` into DownstreamTlsContext in builder
- [ ] Server name: `Listener.ServerName string` → HCM.server_name
- [ ] Multiple filter chains: support SNI routing (one cert per domain) via multiple FilterChains
- [ ] Max request headers size: `Listener.MaxRequestHeadersKB uint32` → HCM.max_request_headers_kb

### Filters — new types + full wiring
- [ ] Wire existing filter types with real Envoy configs (CORS, JWT, ExtAuthz, ExtProc) — replace stubs in buildHTTPFilter
- [ ] FilterOverrides merge logic in builder: group is base, route wins → per_filter_config on Envoy routes
- [ ] New FilterType: rate limiting (`rateLimit`) — Filter config for rate limit service + FilterOverrides for descriptors
- [ ] New FilterType: header manipulation (`headers`) — Filter config for add/remove request/response headers

### Housekeeping
- [ ] Add authentication to the REST API
- [ ] Write unit and integration tests
- [ ] HA storage: document shared-volume / Litestream pattern for multi-replica deployments
- [ ] Update `ARCHITECTURE.md` to reflect current package structure

## Done

- [x] **Wire methods, queryParams, hashPolicy in xDS builder** (cd083bb)
  - `buildRouteMatch`: maps `MatchRule.Methods` to `:method` header matcher (exact for 1, regex OR for multiple)
  - `buildRouteMatch`: maps `MatchRule.QueryParams` via `buildQueryParamMatcher` (exact, regex, presence-only)
  - `HashPolicy` moved from `BackendRef` to `ForwardAction` — hash_policy is evaluated at routing time, not at cluster level. `BackendRef` now only carries `destinationId` and `weight`
  - `applyHashPolicy`: maps header, cookie (with TTL), and sourceIP to Envoy `RouteAction.hash_policy`
  - `MatchRule.Ports` removed — Envoy has no port matching in RouteMatch
  - OpenAPI docs regenerated

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
