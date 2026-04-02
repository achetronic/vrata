# Controller — Design

## Overview

The Controller is a Kubernetes controller that watches Gateway API resources
(`GatewayClass`, `Gateway`, `HTTPRoute`, `GRPCRoute`, and future `SuperHTTPRoute`) and synchronises them
to Vrata via its REST API. It is a separate binary that lives in `clients/controller/`.
Its only contract with Vrata is the OpenAPI spec — no shared code, no imports.

The controller is unidirectional: it reads from Kubernetes and writes to Vrata.
It never reads from Vrata to write back to Kubernetes.

## Mapping

```
Gateway.spec.listeners[]            → Listener (1:1)
GatewayClass                        → Claimed by controller (status written)
HTTPRoute                           → RouteGroup (1:1, carries hostnames + parentRef binding)
HTTPRoute.spec.rules[]              → Route (1:N, one Route per rule)
HTTPRoute.spec.rules[].matches[]    → Route.match (N matches in a rule = N Routes)
HTTPRoute.spec.rules[].backendRefs[]→ Destination (deduplicated by Service name + namespace + port)
HTTPRoute.spec.rules[].filters[]    → depends on type:
    RequestRedirect                 → Route.redirect (no forward)
    URLRewrite                      → Route.forward.rewrite
    RequestHeaderModifier           → Middleware type=headers
GRPCRoute                           → RouteGroup (1:1, carries hostnames, grpc flag)
GRPCRoute.spec.rules[]              → Route (1:N, one Route per rule)
GRPCRoute.spec.rules[].matches[]    → Route.match (grpc: true + path from service/method)
GRPCRoute.spec.rules[].backendRefs[]→ Destination (shared with HTTPRoute destinations)
GRPCRoute.spec.rules[].filters[]    → RequestHeaderModifier → Middleware type=headers
```

## Ownership

Every entity the controller creates in Vrata is named with a convention that
identifies it as controller-managed:

- **Destinations**: `k8s:{namespace}/{service-name}:{port}`
- **Routes**: `k8s:{namespace}/{httproute-name}/rule-{index}/match-{index}`
- **RouteGroups**: `k8s:{namespace}/{httproute-name}`
- **Listeners**: `k8s:{gateway-namespace}/{gateway-name}/{listener-name}`
- **Middlewares**: `k8s:{namespace}/{httproute-name}/rule-{index}/{filter-type}`

The controller only touches entities whose name starts with `k8s:`. Entities
created manually via the Vrata API are never modified or deleted.

## Destination Reference Counting

A `Destination` in Vrata represents a Kubernetes Service (name + namespace + port).
Multiple Routes may reference the same Destination. The controller must not
delete a Destination when one Route is removed if other Routes still reference it.

The controller maintains a reference count per Destination:

1. On **create Route**: if the Destination doesn't exist, create it and set
   refcount to 1. If it exists, increment refcount.
2. On **delete Route**: decrement refcount. If refcount reaches 0, delete
   the Destination from Vrata.
3. On **update Route**: if the backendRef changed, decrement the old
   Destination's refcount and increment (or create) the new one.

The refcount is maintained in-memory by the controller. On startup, the
controller rebuilds the refcount by scanning all Routes it owns in Vrata
(names starting with `k8s:`).

## Reconciliation

The controller uses Kubernetes informers to watch Gateway, HTTPRoute, and
SuperHTTPRoute resources. On each event:

1. **Map** the resource to the desired Vrata entities (using the mapper).
2. **Diff** against the current state in Vrata (by listing entities with
   the `k8s:` prefix for that resource).
3. **Apply** changes in dependency order:
   - Creates: Destinations first, then Routes, then RouteGroups
   - Deletes: RouteGroups first, then Routes, then Destinations (only if refcount = 0)
   - Updates: Destinations, then Routes, then RouteGroups
4. **Queue** a snapshot (see batching below).

### Initial Sync

On startup, the controller:

1. Lists all Gateways and HTTPRoutes from the cluster.
2. Lists all `k8s:`-prefixed entities from Vrata.
3. Calculates the full diff (creates + updates + deletes).
4. Applies all changes.
5. Creates and activates a snapshot.

### Diff Calculation

The diff is based on semantic content, not Kubernetes metadata:

- Two entities are **equal** if their Vrata-relevant fields match (path, hostnames,
  backends, filters). Kubernetes annotations, labels, resourceVersion, etc. are
  ignored for diff purposes.
- An entity is **orphaned** if it exists in Vrata with a `k8s:` name but no
  corresponding Kubernetes resource exists.

## Snapshot Batching

The controller does not create a snapshot after every individual change.
Changes are accumulated and a snapshot is created when:

- **Debounce**: no new changes arrive for 5 seconds (configurable).
- **Max accumulation**: 100 changes (configurable) have accumulated without
  a snapshot, forcing one even if changes keep arriving.

This handles both burst scenarios (Helm upgrade touching 50 HTTPRoutes) and
steady-state (individual changes trickling in).

## Components

```
clients/controller/
├── cmd/
│   └── controller/
│       └── main.go              # flags, scheme registration, informers, start
├── internal/
│   ├── mapper/
│   │   └── mapper.go            # Gateway API types → Vrata API types (pure, no I/O)
│   ├── reconciler/
│   │   ├── gateway.go           # Gateway → Listener reconciliation
│   │   ├── httproute.go         # HTTPRoute → Route + Group + Destination reconciliation
│   │   └── refcount.go          # Destination reference counting
│   ├── vrata/
│   │   └── client.go            # Typed HTTP client for the Vrata REST API
│   ├── batcher/
│   │   └── batcher.go           # Change accumulation + debounce + snapshot trigger
│   └── status/
│       └── writer.go            # Write conditions back to HTTPRoute status
├── Makefile                     # Fetch CRDs + patch SuperHTTPRoute + codegen
└── config/
    └── crd/
        └── superhttproute.yaml  # Generated SuperHTTPRoute CRD
```

## CRD Generation Pipeline (Makefile)

1. **Fetch**: download Gateway API CRDs from `kubernetes-sigs/gateway-api` release.
2. **Patch**: copy HTTPRoute CRD → SuperHTTPRoute CRD. Change `kind`, `group`,
   `plural`. Remove all `maxItems` validations from the OpenAPI schema.
3. **Codegen**: generate Go structs from the CRDs using `controller-gen` or
   a custom code generator.
4. **Register**: generate scheme registration for `client-go` informers.

The SuperHTTPRoute spec is byte-for-byte identical to HTTPRoute spec minus
the `maxItems` constraints. The same mapper code handles both types.

## What the Controller Does NOT Do

- Does not read from Vrata to write to Kubernetes (unidirectional).
- Does not optimise, group, or collapse routes (it's a mirror).
- Does not create SuperHTTPRoute resources (the operator does).
- Does not manage TLS certificates (gap: Gateway refs Secrets, Listener expects file paths).
- Does not touch entities not created by itself (`k8s:` prefix is the boundary).
- Does not resolve hostname overlaps or suggest wildcards.

## Configuration

The controller is configured via a YAML file passed with `--config` (the only CLI flag).
All string values support `${ENV_VAR}` substitution via `os.ExpandEnv`.
See [`clients/controller/config.yaml`](../../clients/controller/config.yaml) for the
full reference with inline comments.

| Section          | Key fields                                                | Description                                                |
| ---------------- | --------------------------------------------------------- | ---------------------------------------------------------- |
| `vrata`          | `url`                                                     | Base URL of the Vrata control plane API                    |
| `tls`            | `cert`, `key`, `ca`                                       | TLS for the CP connection (inline PEM or file paths)       |
| `apiKey`         |                                                           | Bearer token sent to the CP on every request               |
| `watch`          | `namespaces`, `httpRoutes`, `superHttpRoutes`, `gateways` | Which k8s resources to watch and optional namespace filter |
| `snapshot`       | `debounce`, `maxBatch`                                    | Batching before creating a Vrata snapshot                  |
| `duplicates`     | `mode` (`off` / `warn` / `reject`)                        | Overlap detection with semantic path matching              |
| `log`            | `format`, `level`                                         | Structured logging (console/json, debug/info/warn/error)   |
| `leaderElection` | `enabled`, `leaseName`, `leaseNamespace`, durations       | Lease-based leader election for multiple replicas          |
| `metrics`        | `enabled`, `address`                                      | Prometheus metrics endpoint                                |

## Status Writing

After successful reconciliation, the controller writes conditions back to
the HTTPRoute resource:

- `Accepted: True` — the route was successfully synced to Vrata.
- `Accepted: False` — the route could not be synced (e.g. missing backend Service).
- `ResolvedRefs: True/False` — whether all backendRefs resolved to valid Services.

This follows the Gateway API status convention so operators can use standard
tooling (`kubectl get httproute` shows the conditions).
