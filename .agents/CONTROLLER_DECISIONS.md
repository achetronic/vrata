# Controller — Technical Decisions

Technical decisions made for the Vrata controller. Any AI tool working on this repository
**must respect these decisions** and not revert them without explicit approval.

---

## Garbage collection: full-cycle diff against Vrata state

**Date**: 2026-03-20
**Status**: Implemented

The controller performs garbage collection at two levels on every sync cycle:

- **Inter-group GC**: after processing all HTTPRoutes/SuperHTTPRoutes, the controller
  lists all `k8s:`-prefixed groups in Vrata and deletes those that have no corresponding
  resource in Kubernetes. This handles HTTPRoute deletion and renaming.
- **Intra-group GC** (inside `ApplyHTTPRoute`): after creating/updating the desired
  routes and middlewares for an HTTPRoute, the controller lists all `k8s:ns/name/*`
  routes and middlewares in Vrata and deletes those not produced by the current mapper
  output. This handles match removal, rule removal, and reordering within an HTTPRoute.

Destination deletion is always refcount-gated: a destination is only deleted when
no owned route references it anymore.

The dedup detector is reset at the start of every sync cycle to avoid stale entries
from deleted HTTPRoutes blocking new ones with the same paths.

**Do not**: skip GC for performance reasons. Do not delete destinations without
checking the refcount. Do not run GC before applying all desired state — always
apply first, then GC.

---

## Batch snapshot coordination via `vrata.io/batch` annotation

**Date**: 2026-03-20
**Status**: Decided — not yet implemented

### Problem

A large Helm release may create hundreds of HTTPRoutes simultaneously. The batcher's
debounce mechanism creates intermediate snapshots as each HTTPRoute is reconciled,
pushing partially-applied config to proxies. For large deployments this is undesirable:
the proxy should only receive a snapshot when the entire release is fully reconciled.

### Mechanism

HTTPRoutes (and SuperHTTPRoutes) may carry two annotations:

```
vrata.io/batch: "<batch-group-name>"
vrata.io/batch-size: "<expected-member-count>"   # optional
```

`vrata.io/batch` is a free-form string identifying a logical deployment group (e.g.
`release-v2.3`, `helm-upgrade-1234`). All HTTPRoutes sharing the same value are
treated as an indivisible batch.

`vrata.io/batch-size` is optional. When present, it tells the controller exactly how
many HTTPRoutes to expect in the group. If inconsistent values are seen across members
of the same group, the controller logs a warning and uses the value from the first
member observed.

### Work queue

The controller maintains an **ordered FIFO work queue**. Each element is one of:

- `SingleRoute` — an HTTPRoute without the batch annotation, processed with normal
  debounce behaviour.
- `BatchGroup` — all HTTPRoutes sharing the same annotation value, treated as a unit.

Elements are enqueued in **order of first observation** (first tick in which a resource
with a new batch group value is seen). The queue is **strictly sequential**: the head
element must complete before the next element is processed.

### BatchGroup lifecycle

1. **Accumulating** — the group is seen for the first time or new members keep arriving.
   The idle timeout resets on every new member arrival. The queue is blocked at this group.
2. **Ready** — completeness is determined differently depending on whether `batch-size`
   is set:
   - **Without `batch-size`**: the group is Ready when the idle timeout expires. The
     controller cannot distinguish a complete batch from an interrupted one.
   - **With `batch-size`**: the group is Ready when `count == batch-size`. If the idle
     timeout expires before all members have arrived, the controller logs an error
     (`"batch release-v2.4 timed out: got 80/200"`) and makes a snapshot anyway as a
     failsafe — the operator must re-run the deployment to converge.
3. When Ready, all members are reconciled atomically and a single snapshot is created.

### Idle timeout semantics

The idle timeout is **not** a fixed countdown from first observation. It resets every
time a new HTTPRoute belonging to the group arrives in the informer cache. This means
a batch of 1000 HTTPRoutes that Helm applies over 2 minutes will not time out
prematurely — the timeout only starts counting when Helm stops adding new members.

### Queue blocking

While a `BatchGroup` is in `Accumulating` state:

- No `SingleRoute` behind it in the queue is processed.
- No other `BatchGroup` behind it is processed.
- Normal debounce snapshots are suppressed.

When a `BatchGroup` transitions to `Ready`, all its members are reconciled atomically
(in a single pass) and a single snapshot is created and activated.

### Concurrent batches

If a second batch group (`release-v2.4`) arrives while `release-v2.3` is still
accumulating, it is enqueued **behind** `v2.3`. It does not start accumulating its
own idle timeout until `v2.3` has completed.

### Configuration

```yaml
snapshot:
  debounce: "5s" # existing: debounce for SingleRoute items
  maxBatch: 100 # existing: max changes before forced flush
  batchIdleTimeout: "10s" # new: idle window after last member arrival
  batchIncompletePolicy: "apply" # new: apply | reject for incomplete batches
```

When a batch with `batch-size` times out before all members arrive, the
`batchIncompletePolicy` controls what happens:

- `apply` (default): log an error with the count (`got 80/200`), reconcile the
  members that arrived, and create the snapshot. The proxy gets partial config.
- `reject`: log an error, discard the batch entirely, do not create a snapshot.
  The previous active snapshot stays in effect. The operator must re-deploy.

Without `batch-size`, the policy is irrelevant — the controller cannot detect
incomplete batches and always applies when the idle timeout expires.

**Do not**: process batch groups out of order. Do not apply the batch annotation
semantics to Gateways — only HTTPRoute and SuperHTTPRoute carry it. Do not create
intermediate snapshots while a batch group is accumulating.

---

## Level-triggered reconciliation (not edge-triggered)

**Date**: 2026-03-20
**Status**: Implemented

The controller uses controller-runtime's informer cache (`cache.Cache`) to watch
Kubernetes resources. The cache is kept up to date via Kubernetes watch streams.
However, the controller does **not** use informer event handlers (OnAdd, OnUpdate,
OnDelete). Instead, it calls `cache.List()` on every tick (every 2 seconds) and
computes a full diff against the current Vrata state.

This is a **level-triggered** model: the controller reacts to the current desired
state, not to individual change events.

**Reasoning**: level-triggered reconciliation guarantees convergence even if events
are missed, reordered, or delivered multiple times. It eliminates a class of race
conditions that arise when multiple events arrive faster than they can be processed.
The full-diff approach also makes the GC logic trivial: anything in Vrata that is
not in the current `List()` result is orphaned and must be deleted. The 2-second
polling interval is acceptable given that Vrata's snapshot mechanism already
introduces an intentional delay before config reaches proxies.

**Do not**: add OnAdd/OnUpdate/OnDelete event handlers to the informer cache.
Do not bypass the ticker with direct API calls triggered by watch events.
If lower latency is needed in the future, reduce the ticker interval rather than
switching to edge-triggered reconciliation.

---

## ReferenceGrant enforcement for cross-namespace backendRefs

**Date**: 2026-03-20
**Status**: Implemented

Before reconciling an HTTPRoute, the controller checks every backendRef that
references a Service in a different namespace than the HTTPRoute itself. If no
ReferenceGrant in the target namespace permits the reference, the HTTPRoute is
**skipped entirely** for that reconcile cycle and the status condition
`ResolvedRefs: False` with reason `BackendNotFound` is written back to the
HTTPRoute resource.

Same-namespace backendRefs are always allowed without any ReferenceGrant check.

**Reasoning**: the Gateway API spec requires controllers to enforce ReferenceGrants
for cross-namespace references. Silently forwarding traffic to a Service in another
namespace without explicit permission would be a security violation.

**Do not**: skip the ReferenceGrant check for performance reasons. Do not partially
reconcile an HTTPRoute when one of its backendRefs is denied — skip the whole route.
Do not enforce ReferenceGrants on same-namespace references.

---
