---
title: "Configuration"
weight: 5
---

The controller reads a YAML config file via `--config`. All string values support `${ENV_VAR}` substitution.

## Full reference

```yaml
# URL of the Vrata control plane REST API.
controlPlaneUrl: "${CONTROLPLANE_URL:-http://localhost:8080}"

# Which Kubernetes resources to watch.
watch:
  namespaces: []          # Empty = all namespaces
  httpRoutes: true        # Standard Gateway API HTTPRoutes
  superHttpRoutes: false  # SuperHTTPRoute (no maxItems limits)
  gateways: true          # Gateway resources → Vrata Listeners

# Snapshot batching.
snapshot:
  debounce: "5s"          # Wait after last change before snapshot
  maxBatch: 100           # Force snapshot after this many changes
  autoCreate: true        # Create snapshots automatically on flush
  autoActivate: true      # Activate snapshots immediately after creation
  batchIdleTimeout: "10s" # Wait after last batch member arrives (vrata.io/batch)
  # What to do when a batch with vrata.io/batch-size times out before all
  # members arrive. Only applies when both annotations are present.
  #   "apply"  — create the snapshot with whatever arrived (default)
  #   "reject" — discard the incomplete batch, don't create a snapshot
  batchIncompletePolicy: "apply"

# Overlap detection.
duplicates:
  mode: "warn"            # off | warn | reject

# Logging.
log:
  format: "console"       # console | json
  level: "info"           # debug | info | warn | error

# Leader election for multiple replicas.
leaderElection:
  enabled: false
  leaseName: "vrata-controller-leader"
  leaseNamespace: "default"
  leaseDuration: "15s"
  renewDeadline: "10s"
  retryPeriod: "2s"

# Prometheus metrics.
metrics:
  enabled: false
  address: ":9090"
```

## Field reference

| Field | Default | Description |
|-------|---------|-------------|
| `controlPlaneUrl` | `http://localhost:8080` | Vrata control plane URL |
| `watch.namespaces` | `[]` (all) | Restrict to specific namespaces |
| `watch.httpRoutes` | `true` | Watch HTTPRoute resources |
| `watch.superHttpRoutes` | `false` | Watch SuperHTTPRoute resources |
| `watch.gateways` | `true` | Watch Gateway resources |
| `snapshot.debounce` | `5s` | Debounce before creating snapshot |
| `snapshot.maxBatch` | `100` | Max changes before forced snapshot |
| `snapshot.autoCreate` | `true` | Create snapshots automatically on flush. When `false`, the controller syncs resources to Vrata but never creates snapshots — use the API manually |
| `snapshot.autoActivate` | `true` | Activate snapshots immediately. When `false`, snapshots are created but left inactive for manual review and activation via the API. Only applies when `autoCreate` is `true` |
| `snapshot.batchIdleTimeout` | `10s` | Idle wait for `vrata.io/batch` groups. See [Batch Deployments]({{< relref "batch-deployments" >}}) |
| `snapshot.batchIncompletePolicy` | `apply` | `apply`: snapshot with partial set. `reject`: discard incomplete batch. See [Batch Deployments]({{< relref "batch-deployments" >}}) |
| `duplicates.mode` | `warn` | `off`: disabled, `warn`: log only, `reject`: skip route |
| `leaderElection.enabled` | `false` | Enable lease-based leader election |
| `metrics.enabled` | `false` | Enable Prometheus metrics on `:9090` |
