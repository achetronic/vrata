# KController — TODO

## Phase 1: Core Sync

### CRD Pipeline

- [ ] Makefile target to fetch Gateway API CRDs from `kubernetes-sigs/gateway-api`
- [ ] Script to patch HTTPRoute CRD → SuperHTTPRoute (remove `maxItems`, change kind/group)
- [ ] Go struct generation from CRDs
- [ ] Scheme registration for client-go

### Vrata API Client

- [ ] Typed HTTP client for Vrata REST API (generated or hand-written from OpenAPI)
- [ ] CRUD for: Listeners, Destinations, Routes, RouteGroups, Middlewares
- [ ] Snapshot create + activate
- [ ] List entities by name prefix (`k8s:*`) for ownership filtering

### Mapper

- [ ] Gateway → Listener mapping
- [ ] HTTPRoute → RouteGroup mapping (hostnames + parentRef)
- [ ] HTTPRoute rule → Route mapping (path, headers, method, queryParams)
- [ ] HTTPRoute rule matches → N Routes (one per match)
- [ ] backendRef → Destination mapping (dedup by Service name+namespace+port)
- [ ] RequestRedirect filter → Route.redirect
- [ ] URLRewrite filter → Route.forward.rewrite
- [ ] RequestHeaderModifier filter → Middleware type=headers
- [ ] Unit tests for all mappings

### Reconciler

- [ ] Gateway reconciler (create/update/delete Listeners)
- [ ] HTTPRoute reconciler (create/update/delete Routes + Groups + Destinations)
- [ ] Destination reference counting (in-memory, rebuild on startup)
- [ ] Diff calculation (semantic comparison, not metadata)
- [ ] Dependency-ordered apply (Destinations → Routes → Groups on create, reverse on delete)
- [ ] Initial full sync on startup
- [ ] Unit tests for diff and refcount

### Batcher

- [ ] Change accumulation
- [ ] Debounce timer (configurable, default 5s)
- [ ] Max batch size (configurable, default 100)
- [ ] Snapshot create + activate on flush
- [ ] Unit tests

### Informers

- [ ] Gateway informer
- [ ] HTTPRoute informer
- [ ] SuperHTTPRoute informer (same reconciler, different GVK)
- [ ] Proper error handling and requeue

### Status Writer

- [ ] Write `Accepted` condition to HTTPRoute
- [ ] Write `ResolvedRefs` condition to HTTPRoute

## Phase 2: Production Readiness

- [ ] Metrics (Prometheus) — reconcile duration, errors, queue depth
- [ ] Health endpoint (`/healthz`)
- [ ] Graceful shutdown
- [ ] Leader election (for running multiple replicas)
- [ ] Helm chart for the controller
- [ ] E2E tests against kind cluster with Vrata + controller + sample HTTPRoutes
- [ ] Documentation in docs/

## Phase 3: SuperHTTPRoute

- [ ] SuperHTTPRoute CRD generation (automated from HTTPRoute CRD)
- [ ] Install CRD on cluster via Helm or kustomize
- [ ] Validate that SuperHTTPRoute with >16 hostnames and >64 rules works end-to-end

## Known Gaps

- **TLS certificates**: Gateway references Secrets for TLS, Vrata Listener expects
  file paths. Need a mechanism to mount Secrets as files or extend Vrata to accept
  inline certs.
- **Duplicate detection**: `--warn-duplicates` flag logs conflicts but doesn't
  resolve them. Future: configurable precedence rules.
- **ReferenceGrant**: Gateway API uses ReferenceGrant to allow cross-namespace
  references. The controller should respect these.
