# Controller — TODO

## Pending

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **Regex overlap detection** — detect semantic overlaps when one of the paths is a RegularExpression. Currently regex paths are skipped by the dedup detector.

---

### Kube Agentic Networking support

Watch `XBackend` and `XAccessPolicy` from `agentic.prototype.x-k8s.io/v0alpha0`
and map them to Vrata entities. Uses three generic proxy features already
implemented in the server (CEL body access, mTLS client auth, and `inlineAuthz`
middleware) for InlineTools and SPIFFE/ServiceAccount identity. ExternalAuth
and XBackend work with existing Vrata entities without any additional proxy changes.

See `AGENTIC_NETWORKING_REPORT.md` for the full spec analysis.
See `SERVER_DECISIONS.md` for the proxy feature design rationale.

#### Prerequisite: types and informers

- [ ] **Go types**: vendor or generate Go types for
  `agentic.prototype.x-k8s.io/v0alpha0` (`XBackend`, `XBackendList`,
  `XAccessPolicy`, `XAccessPolicyList`). The types are available in the
  kube-agentic-networking repo at `api/v0alpha0/`. Register in the
  controller's scheme.
- [ ] **Informers**: add informers for XBackend and XAccessPolicy to the
  cache in `cmd/controller/main.go`. Only start when enabled via config.
- [ ] **Config**: add `watch.xBackends` and `watch.xAccessPolicies` boolean
  flags (default `false`). Add `agentic.trustDomain` string for SPIFFE ID
  generation from ServiceAccounts.
- [ ] **Naming convention**: entities created from agentic resources use
  `k8s:agentic:{namespace}/{name}` prefix. Update `IsOwned()` to recognize it.

#### Mapper: XBackend → Destination + Route

- [ ] **mapper/xbackend.go** (new file): `MapXBackend(backend) → MappedEntities`
  - One `Destination` per XBackend:
    - Name: `k8s:agentic:{ns}/{backend-name}`
    - Host: `{serviceName}.{ns}.svc.cluster.local` if `serviceName` is set;
      `hostname` if `hostname` is set
    - Port: `spec.mcp.port`
    - TLS: if `hostname` is set (external), enable TLS on the destination
  - One `Route` for the MCP path:
    - Name: `k8s:agentic:{ns}/{backend-name}/mcp`
    - Match: `pathPrefix: spec.mcp.path` (default `/mcp`)
    - Forward: `destinations: [{destinationId: <dest-name>}]`
    - Rewrite if needed (when HTTPRoute path differs from backend path)
  - Note: XBackend can also be referenced as a `backendRef` in a standard
    HTTPRoute. The controller must recognize `group: agentic.prototype.x-k8s.io,
    kind: XBackend` in backendRefs and resolve to the Destination above.
- [ ] **Unit tests**: backend with serviceName, backend with hostname (TLS),
  custom path, default `/mcp` path

#### Mapper: XAccessPolicy with ExternalAuth → ExtAuthz Middleware

- [ ] **mapper/xaccess_policy.go** (new file):
  `MapXAccessPolicy(policy, resolvedTargets) → MappedEntities`
  - For rules with `authorization.type: ExternalAuth`:
    - One `Middleware` (type `extAuthz`) per policy:
      - Name: `k8s:agentic:{ns}/{policy-name}/extauthz`
      - DestinationID: resolved from `externalAuth.backendRef`
      - Mode: from `externalAuth.protocol` (`HTTP` → `"http"`, `GRPC` → `"grpc"`)
      - IncludeBody: `true` if `externalAuth.forwardBody.maxSize > 0`
      - OnCheck.ForwardHeaders: from `externalAuth.http.allowedHeaders` or
        `externalAuth.grpc.allowedHeaders`
      - OnAllow.CopyToUpstream: from `externalAuth.http.allowedResponseHeaders`
      - Path: from `externalAuth.http.path`
    - Attach middleware to target routes:
      - If targetRef is XBackend: add to the XBackend's route `MiddlewareIDs`
      - If targetRef is Gateway: add to ALL groups under that gateway
  - Also create a Destination for the authz service backendRef if not already
    managed.
- [ ] **Unit tests**: HTTP mode, gRPC mode, forwardBody, target XBackend,
  target Gateway, backendRef resolution

#### Mapper: XAccessPolicy with InlineTools → `inlineAuthz` middleware

Requires server features: CEL body access (`request.body`) + `inlineAuthz` middleware.

- [ ] **`inlineAuthz` generation for InlineTools**:
  - For each XAccessPolicy with InlineTools rules targeting an XBackend:
    - One `Middleware` (type `inlineAuthz`) per policy:
      - Name: `k8s:agentic:{ns}/{policy-name}/inlineauthz`
      - Rules generated from source + tools combination:
        1. **Always-allow rule** (first, highest priority):
           ```json
           { "cel": "request.method == 'GET' || request.method == 'DELETE' || request.body.json.method in ['initialize', 'notifications/initialized', 'tools/list']", "action": "allow" }
           ```
        2. **Per-source allow rule** — combines identity match AND tool match:
           ```json
           { "cel": "request.tls.peerCertificate.uris.exists(u, u == 'spiffe://...') && (request.body.json.method != 'tools/call' || request.body.json.params.name in ['add', 'subtract'])", "action": "allow" }
           ```
      - `defaultAction: "deny"`, `denyStatus: 403`
    - Attach middleware to the XBackend's route `MiddlewareIDs`
    - If targetRef is Gateway: attach to ALL groups under that gateway
  - Multiple rules in the same policy → multiple CEL rules in the same
    `inlineAuthz` middleware, one per source+tools combination
- [ ] **Unit tests**: single tool, multiple tools, empty tools (deny all),
  always-allowed methods bypass, multiple rules in one policy

#### Mapper: XAccessPolicy source → SPIFFE CEL expressions

Requires server feature: mTLS client auth + cert info in CEL.

- [ ] **SPIFFE source**:
  - For rules with `source.type: SPIFFE`:
    - Identity CEL fragment:
      `request.tls.peerCertificate.uris.exists(u, u == "spiffe://domain/...")`
    - Combined with tools CEL fragment via `&&` in the `inlineAuthz` rule
- [ ] **ServiceAccount source**:
  - For rules with `source.type: ServiceAccount`:
    - Convert to SPIFFE ID using the configured trust domain:
      `spiffe://<trustDomain>/ns/<namespace>/sa/<name>`
    - If namespace is empty, use the policy's namespace
    - Same CEL fragment as SPIFFE source
- [ ] **Unit tests**: SPIFFE source, ServiceAccount source, SA default namespace,
  combined source + InlineTools, combined source + ExternalAuth

#### Reconciliation loop

- [ ] **Sync cycle**: after processing Gateways, HTTPRoutes, and
  SuperHTTPRoutes, add processing blocks for:
  1. `cache.List()` XBackends → map → apply to Vrata
  2. `cache.List()` XAccessPolicies → sort by seniority → map → apply to Vrata
  - XBackend destinations must be created before XAccessPolicy middlewares
    (middlewares reference destinations of authz services)
- [ ] **Seniority ordering**: when multiple XAccessPolicies target the same
  resource, order by creation timestamp (oldest first), then alphabetically.
  Enforce max 5 policies per target — reject excess with status condition.
- [ ] **Policy evaluation order in middleware chain**: gateway-level policies
  come before backend-level policies in the middleware chain. Within each
  level, ordered by seniority.

#### Garbage collection

- [ ] **Inter-group GC**: after processing all XBackends and XAccessPolicies,
  list all `k8s:agentic:*` entities in Vrata and delete those with no
  corresponding Kubernetes resource. Same pattern as HTTPRoute GC.
- [ ] **Intra-group GC**: after applying an XBackend or XAccessPolicy, list
  sub-entities and delete stale ones not produced by the current mapper output.
- [ ] **Refcount for agentic destinations**: XAccessPolicy ExternalAuth creates
  Destinations for authz services. Use existing refcount mechanism.

#### Status writing

- [ ] **XBackend status**: write `Available: True/False` condition.
  `Available: False` if the backing Service does not exist.
- [ ] **XAccessPolicy status**: write `PolicyAncestorStatus` per targetRef
  with `Accepted: True/False`. Reasons for rejection:
  - `LimitPerTargetExceeded` — more than 5 policies per target
  - Target XBackend or Gateway does not exist
  - ExternalAuth backendRef does not resolve
  Set `controllerName` to Vrata's controller name.

#### Metrics

- [ ] New controller metrics:
  - `controller_xbackends_synced_total{result=success|error}`
  - `controller_xaccess_policies_synced_total{result=success|error}`

## Done

- [x] **Batch snapshot coordination** — `vrata.io/batch` and `vrata.io/batch-size` annotations with FIFO work queue, idle timeout, and incomplete batch detection. See `CONTROLLER_DECISIONS.md`.
- [x] **Garbage collection** — inter-group GC (orphaned HTTPRoutes) and intra-group GC (stale routes/middlewares within an HTTPRoute). See `CONTROLLER_DECISIONS.md`.
- [x] **ReferenceGrant enforcement** — cross-namespace backendRefs verified via ReferenceGrant before reconciliation. See `CONTROLLER_DECISIONS.md`.
- [x] **Metrics wiring** — all 8 controller Prometheus metrics wired into the sync cycle.
- [x] **Dedup detector reset** — reset at the start of each sync cycle to avoid stale phantom entries.
- [x] **controller-runtime logr bridge** — `crlog.SetLogger` bridging slog to logr.
