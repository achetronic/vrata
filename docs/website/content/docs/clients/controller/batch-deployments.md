---
title: "Batch Deployments"
weight: 7
---

When you deploy many HTTPRoutes at once (for example, a Helm upgrade that creates or updates hundreds of routes), the controller can group them into a single atomic snapshot instead of creating intermediate snapshots as each route arrives.

## The problem

Without batching, a Helm upgrade that touches 200 HTTPRoutes produces multiple snapshots during the rollout — the proxy receives partially-applied config several times before everything is in place. This is usually harmless, but if your routes depend on each other (e.g. a redirect route and the route it points to), intermediate snapshots can cause brief errors.

## How it works

Add the `vrata.io/batch` annotation to your HTTPRoutes:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api-users
  annotations:
    vrata.io/batch: "release-v2.4"
spec:
  # ...
```

All HTTPRoutes with the same `vrata.io/batch` value are treated as a single group. The controller waits until all members have arrived before reconciling them and creating one snapshot.

## How the controller knows the group is complete

The controller watches for new HTTPRoutes arriving with the same batch annotation. When no new members arrive for a configurable idle period (default: 10 seconds), the controller considers the group complete and reconciles all members at once.

This means you don't need to tell the controller in advance how many routes to expect — it figures it out by waiting for the stream to stop.

## Optional: explicit group size

If you know exactly how many HTTPRoutes are in the batch, you can tell the controller with a second annotation:

```yaml
annotations:
  vrata.io/batch: "release-v2.4"
  vrata.io/batch-size: "200"
```

With `batch-size` set:

- If all 200 arrive before the idle timeout, the snapshot is created **immediately** — no waiting for the idle period.
- If only 150 arrive and the idle timeout expires, the controller logs an error with the exact count (`got 150/200`) and creates the snapshot anyway. The operator should check what went wrong with the remaining 50.

Without `batch-size`, the controller can't tell the difference between "complete" and "interrupted" — it just waits for the idle timeout either way.

## Queue ordering

Batch groups and individual routes are processed in the order they appear. If a batch group is still accumulating members, everything behind it in the queue waits. This guarantees that:

- A batch is never split across multiple snapshots
- Routes that arrive after a batch started don't get processed before the batch finishes
- Multiple batches are processed one at a time, in order

## Configuration

```yaml
snapshot:
  debounce: "5s"              # Normal debounce for non-batched routes
  maxBatch: 100               # Max changes before forced snapshot (non-batched)
  batchIdleTimeout: "10s"     # How long to wait after last batch member arrives
```

The `batchIdleTimeout` only affects routes with the `vrata.io/batch` annotation. Routes without it use the normal `debounce` behaviour.

## Example: Helm release with batch annotations

Add the annotations to your HTTPRoute templates:

```yaml
# templates/httproute.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: {{ .Release.Name }}-api
  annotations:
    vrata.io/batch: {{ .Release.Name }}-{{ .Release.Revision | quote }}
    vrata.io/batch-size: "3"  # if you know the count
spec:
  # ...
```

When you run `helm upgrade`, all HTTPRoutes in the release share the same batch annotation value. The controller waits for all of them and creates a single snapshot.

## What happens if Helm fails mid-deploy

If Helm crashes after applying 80 of 200 HTTPRoutes, the remaining 120 never arrive. The controller waits for the idle timeout and then needs to decide what to do.

This is where `batch-size` and `batchIncompletePolicy` work together.

### Without `batch-size`

The controller can't tell the difference between "all 80 arrived" and "80 of 200 arrived". It treats the batch as complete when the idle timeout expires and creates a snapshot.

### With `batch-size` — policy `apply` (default)

The controller knows 80 of 200 arrived. It logs an error with the exact count and **creates the snapshot anyway**. The proxy gets the partially-applied config. This is the right choice when partial config is better than no config — for example, if the 80 routes that arrived are independent and functional on their own.

```
ERROR workqueue: batch group timed out before all members arrived, applying partial set
  batch=release-v2.4 got=80 expected=200
```

### With `batch-size` — policy `reject`

The controller knows 80 of 200 arrived. It logs an error and **discards the batch entirely** — no snapshot is created, no config reaches the proxy. The previous snapshot stays active. This is the right choice when partial config would cause errors — for example, if the missing routes include dependencies that the existing 80 need.

```
ERROR workqueue: incomplete batch rejected, discarding
  batch=release-v2.4 got=80 expected=200
```

The operator must re-run the deployment. On the next successful deploy, all 200 routes will arrive and the batch will complete normally.

### Choosing the right policy

| Scenario | Recommended policy |
|---|---|
| Routes are independent (each serves its own path) | `apply` |
| Routes depend on each other (redirects pointing to other routes) | `reject` |
| You want to fail fast and fix the pipeline | `reject` |
| You prefer partial service over no service | `apply` |

## Configuration

```yaml
snapshot:
  debounce: "5s"                  # Normal debounce for non-batched routes
  maxBatch: 100                   # Max changes before forced snapshot (non-batched)
  batchIdleTimeout: "10s"         # How long to wait after last batch member arrives
  batchIncompletePolicy: "apply"  # apply | reject (only matters with batch-size)
```

`batchIdleTimeout` only affects routes with the `vrata.io/batch` annotation. Routes without it use the normal `debounce` behaviour.

`batchIncompletePolicy` only takes effect when **both** `vrata.io/batch` and `vrata.io/batch-size` are present and the batch times out before all members arrive. Without `batch-size`, the controller always applies.
