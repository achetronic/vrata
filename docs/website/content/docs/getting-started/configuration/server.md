---
title: "Server Config"
weight: 1
---

The server binary (`/server`) accepts a single flag: `--config path/to/config.yaml`. The config controls which mode Vrata runs in and how each component behaves.

## Full reference

```yaml
# Required: "controlplane" or "proxy"
mode: "controlplane"

#############################
## Control plane mode
#############################
controlPlane:
  address: ":8080"              # HTTP API listen address
  storePath: "/data"            # Root dir for bbolt + raft data
  # raft:                       # Uncomment for HA (see HA with Raft)
  #   nodeId: "${POD_NAME}"
  #   bindAddress: ":7000"
  #   advertiseAddress: "${POD_IP}:7000"
  #   discovery:
  #     dns: "vrata-headless.vrata.svc.cluster.local"

#############################
## Proxy mode
#############################
proxy:
  controlPlaneUrl: "http://control-plane:8080"  # Required in proxy mode
  reconnectInterval: "5s"                        # SSE reconnection delay

#############################
## Logging
#############################
log:
  format: "console"   # "console" or "json"
  level: "info"       # "debug", "info", "warn", "error"

#############################
## Session store (optional)
#############################
# Required only for STICKY load balancing algorithms
# sessionStore:
#   type: "redis"
#   redis:
#     address: "${REDIS_ADDRESS:-localhost:6379}"
#     password: "${REDIS_PASSWORD}"
#     db: 0
```

## Control plane mode

Stores configuration in an embedded bbolt database, exposes the REST API, and pushes snapshots to connected proxies via SSE.

```yaml
mode: "controlplane"
controlPlane:
  address: ":8080"
  storePath: "/data"
```

| Field | Default | Description |
|-------|---------|-------------|
| `controlPlane.address` | `:8080` | HTTP API listen address |
| `controlPlane.storePath` | `/data` | Root directory — bbolt DB at `<storePath>/vrata.db`, Raft data at `<storePath>/raft/` |
| `controlPlane.raft` | — | Raft HA config (see [HA with Raft]({{< relref "ha-raft" >}})) |

In this mode, the control plane also runs the proxy internally — one process does everything. Useful for development or small deployments.

## Proxy mode

Stateless. Connects to a control plane via SSE, receives snapshots, and routes traffic.

```yaml
mode: "proxy"
proxy:
  controlPlaneUrl: "http://control-plane:8080"
  reconnectInterval: "5s"
```

| Field | Default | Description |
|-------|---------|-------------|
| `proxy.controlPlaneUrl` | required | Base URL of the control plane |
| `proxy.reconnectInterval` | `5s` | SSE reconnection delay after disconnect |

Scale proxies freely — they're disposable. If the control plane is unavailable, proxies keep routing with their last snapshot.

## Logging

```yaml
log:
  format: "json"
  level: "info"
```

| Field | Default | Description |
|-------|---------|-------------|
| `log.format` | `console` | `console` (human-readable) or `json` (structured) |
| `log.level` | `info` | `debug`, `info`, `warn`, `error` |

Use `json` in production for log aggregation. Use `console` for local development.

## Session store

Required only if you use `STICKY` load balancing (destination or endpoint level). Without it, `STICKY` falls back to consistent hash.

```yaml
sessionStore:
  type: "redis"
  redis:
    address: "redis:6379"
    password: ""
    db: 0
```

| Field | Default | Description |
|-------|---------|-------------|
| `sessionStore.type` | — | Only `redis` is supported |
| `sessionStore.redis.address` | `localhost:6379` | Redis address |
| `sessionStore.redis.password` | — | Redis password |
| `sessionStore.redis.db` | `0` | Redis database number |

## Environment variables

All string values support `${ENV_VAR}` substitution with optional defaults:

```yaml
controlPlane:
  address: "${SERVER_ADDRESS:-:8080}"
  storePath: "${STORE_PATH:-/data}"

proxy:
  controlPlaneUrl: "${CONTROL_PLANE_URL}"
```

The raw YAML is expanded before parsing, so `${VAR}` works anywhere.
