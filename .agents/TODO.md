# TODO - Rutoso

## In Progress

_(nothing)_

## Pending

- [ ] Add authentication to the REST API
- [ ] Write unit and integration tests
- [ ] HA storage: document shared-volume / Litestream pattern for multi-replica deployments
- [ ] Update `ARCHITECTURE.md` to reflect `model` package as canonical type home (separate from `gateway`)

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
