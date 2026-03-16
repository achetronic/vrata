# TODO - Rutoso

## In Progress

_(nothing yet — project is being set up)_

## Pending

- [ ] Scaffold project structure: `server/` layout, `go.mod`, `Makefile`, `Dockerfile`, `config.example.yaml`
- [ ] Implement `internal/config`: Config struct, `Load()` with `os.ExpandEnv`, `--config` flag wiring
- [ ] Define domain model: `RouteGroup`, `Route`, `Backend`, `MatchRule` in `internal/model`
- [ ] Implement `Store` interface + in-memory implementation in `internal/store/memory`
- [ ] Implement REST API skeleton: router, middleware (logger, recovery), dependency injection
- [ ] Implement route group handlers: `HandleListGroups`, `HandleCreateGroup`, `HandleGetGroup`, `HandleUpdateGroup`, `HandleDeleteGroup`
- [ ] Implement route handlers: `HandleListRoutes`, `HandleCreateRoute`, `HandleGetRoute`, `HandleUpdateRoute`, `HandleDeleteRoute`
- [ ] Duplicate route detection (atomic check + write in store)
- [ ] Integrate swag-go v2: annotate all handlers, `make docs` target, serve spec at `/docs`
- [ ] Implement xDS server: gRPC setup, snapshot cache, version counter
- [ ] Implement xDS snapshot builder: `model.RouteGroup` + `model.Route` → Envoy `Listener`, `RouteConfiguration`, `Cluster`, `ClusterLoadAssignment`
- [ ] Implement `internal/gateway`: subscribe to store events, trigger snapshot rebuild + push
- [ ] Makefile targets: `build`, `run`, `run-dev`, `docker-build`, `docker-push`, `docs`
- [ ] `run-dev` target: document / automate xDS port reachability for Envoys running in Kubernetes
- [ ] Decide and implement production-ready `Store` backend (replicated, HA, simple backup)
- [ ] Add authentication to the REST API
- [ ] Write unit and integration tests

## Done

_(nothing yet)_
