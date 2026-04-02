---
title: "Configuration"
weight: 5
---

The controller reads a YAML config file via `--config`. All string values support `${ENV_VAR}` substitution.

## Full reference

```yaml
# URL of the Vrata control plane REST API.
controlPlaneUrl: "${CONTROLPLANE_URL:-http://localhost:8080}"

# TLS for the connection to the control plane (optional).
# tls:
#   cert: "${CONTROLLER_TLS_CERT}"   # Client cert for mTLS
#   key: "${CONTROLLER_TLS_KEY}"     # Client private key
#   ca: "${CP_CA}"                   # CA to verify the CP server cert

# API key sent to the control plane on every request (optional).
# apiKey: "${CONTROLLER_API_KEY}"

# Which Kubernetes resources to watch.
watch:
  namespaces: []              # Empty = all namespaces
  httpRoutes: true            # Standard Gateway API HTTPRoutes
  grpcRoutes: true            # Standard Gateway API GRPCRoutes
  superHttpRoutes: false      # SuperHTTPRoute (no maxItems limits)
  gateways: true              # Gateway resources → Vrata Listeners
  gatewayClassName: "vrata"   # Only reconcile Gateways with this class

# Snapshot batching.
snapshot:
  debounce: "5s"              # Wait after last change before snapshot
  maxBatch: 100               # Force snapshot after this many changes
  batchIdleTimeout: "10s"     # Wait after last batch member arrives (vrata.io/batch)
  batchIncompletePolicy: "apply"  # apply | reject

# Overlap detection.
duplicates:
  mode: "warn"                # off | warn | reject

# Logging.
log:
  format: "console"           # console | json
  level: "info"               # debug | info | warn | error

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
| `tls` | — | TLS config for the CP connection: `cert`, `key`, `ca` (same as proxy) |
| `apiKey` | — | Bearer token sent to the CP on every request |
| `watch.namespaces` | `[]` (all) | Restrict to specific namespaces |
| `watch.httpRoutes` | `true` | Watch HTTPRoute resources |
| `watch.grpcRoutes` | `true` | Watch GRPCRoute resources |
| `watch.superHttpRoutes` | `false` | Watch SuperHTTPRoute resources |
| `watch.gateways` | `true` | Watch Gateway resources |
| `watch.gatewayClassName` | `vrata` | Only reconcile Gateways with this `spec.gatewayClassName` |
| `snapshot.debounce` | `5s` | Debounce before creating snapshot |
| `snapshot.maxBatch` | `100` | Max changes before forced snapshot |
| `snapshot.batchIdleTimeout` | `10s` | Idle wait for `vrata.io/batch` groups. See [Batch Deployments]({{< relref "batch-deployments" >}}) |
| `snapshot.batchIncompletePolicy` | `apply` | `apply`: snapshot with partial set. `reject`: discard incomplete batch. See [Batch Deployments]({{< relref "batch-deployments" >}}) |
| `duplicates.mode` | `warn` | `off`: disabled, `warn`: log only, `reject`: skip route |
| `leaderElection.enabled` | `false` | Enable lease-based leader election |
| `leaderElection.leaseName` | `vrata-controller-leader` | Name of the Lease resource |
| `leaderElection.leaseNamespace` | `default` | Namespace where the Lease is created |
| `leaderElection.leaseDuration` | `15s` | How long the leader holds the lease |
| `leaderElection.renewDeadline` | `10s` | How long the leader waits before renewing |
| `leaderElection.retryPeriod` | `2s` | How often non-leaders retry acquiring the lease |
| `metrics.enabled` | `false` | Enable Prometheus metrics |
| `metrics.address` | `:9090` | Address the metrics server listens on |
