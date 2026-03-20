# Controller — TODO

## Pending

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **Regex overlap detection** — detect semantic overlaps when one of the paths is a RegularExpression. Currently regex paths are skipped by the dedup detector.

## Done

- [x] **Config loader** — YAML + os.ExpandEnv, --config flag, defaults, validation (5 unit tests)
- [x] **SuperHTTPRoute** — `vrata.io/v1` CRD, Go types wrapping gwapiv1.HTTPRouteSpec, scheme registration, informer, crdclean script (strips maxItems + CEL), Makefile targets (controller-generate-crd, controller-deploy-crd)
- [x] **Vrata API client** — typed HTTP client, CRUD all resources, Owned() filter (6 unit tests)
- [x] **Mapper** — HTTPRoute → Routes+Groups+Destinations+Middlewares, Gateway → Listeners, pure/no I/O (15 unit tests)
- [x] **Reconciler** — apply, delete, dependency order, refcount rebuild from Vrata (5 unit tests)
- [x] **Shared Destination refcount** — derived from Vrata on startup, zero storage (e2e verified)
- [x] **Batcher** — debounce + max batch → snapshot create+activate (5 unit tests)
- [x] **Watch loop** — poll-based sync every 2s via controller-runtime cache
- [x] **Gateway reconciler** — MapGateway → Listeners, create in Vrata
- [x] **Status writer** — Accepted/ResolvedRefs conditions on HTTPRoute (5 unit tests)
- [x] **Duplicate detection** — semantic overlap (prefix covers prefix/exact, segment-aware), 3 modes: off/warn/reject (15 unit tests)
- [x] **Health endpoint** — /healthz on :8081
- [x] **Graceful shutdown** — signal handling + batcher flush
- [x] **Leader election** — lease-based via k8s.io/client-go/tools/leaderelection, configurable lease name/namespace/duration
- [x] **ReferenceGrant** — checks cross-namespace backendRefs against ReferenceGrant resources (5 unit tests)
- [x] **Prometheus metrics** — 8 metrics (reconcile duration/errors/total, snapshots, pending changes, overlaps detected/rejected, refgrant denied) with isolated registry (2 unit tests)
- [x] **3 filter types** — RequestRedirect, URLRewrite, RequestHeaderModifier
- [x] **Helm chart** — Deployment + ServiceAccount + RBAC + ConfigMap in charts/vrata/templates/controller/
- [x] **E2E mapping tests** — hostnames, exact path, multiple matches, method, headers, weighted backends, URL rewrite, header modifier, FQDN, multiple rules (15 e2e tests)
- [x] **63 unit tests + 15 e2e tests** — all passing
