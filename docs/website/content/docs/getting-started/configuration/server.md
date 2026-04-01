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
  # tls:                        # Uncomment for HTTPS / mTLS
  #   cert: "${CP_TLS_CERT}"
  #   key: "${CP_TLS_KEY}"
  #   ca: "${MTLS_CA}"          # CA for client cert verification
  #   clientAuth: "optional"    # none | optional | require
  # auth:                       # Uncomment for API key auth
  #   apiKeys:
  #     - name: "proxy-fleet"
  #       key: "${PROXY_API_KEY}"
  #     - name: "controller"
  #       key: "${CONTROLLER_API_KEY}"

#############################
## Proxy mode
#############################
proxy:
  controlPlaneUrl: "http://control-plane:8080"  # Required in proxy mode
  reconnectInterval: "5s"                        # SSE reconnection delay
  # tls:                                         # Uncomment for TLS / mTLS
  #   cert: "${PROXY_TLS_CERT}"                  # Client cert for mTLS
  #   key: "${PROXY_TLS_KEY}"
  #   ca: "${CP_CA}"                             # CA to verify the CP
  # apiKey: "${PROXY_API_KEY}"                   # Bearer token for auth

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
| `controlPlane.tls` | — | TLS/mTLS config (see [TLS & Auth](#tls--authentication)) |
| `controlPlane.auth` | — | API key auth (see [TLS & Auth](#tls--authentication)) |

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
| `proxy.tls` | — | TLS config for the CP connection (see [TLS & Auth](#tls--authentication)) |
| `proxy.apiKey` | — | Bearer token sent to the CP on every request |

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

## TLS & authentication

The control plane API supports TLS encryption, mutual TLS (mTLS), and API key authentication. These are configured via `controlPlane.tls` and `controlPlane.auth`. Clients (proxy, controller, operators) configure their side via `proxy.tls` / `proxy.apiKey`.

### TLS on the control plane (server)

```yaml
controlPlane:
  tls:
    cert: "${CP_TLS_CERT}"       # PEM server certificate
    key: "${CP_TLS_KEY}"         # PEM private key
    ca: "${MTLS_CA}"             # PEM CA bundle for client certs
    clientAuth: "optional"       # none | optional | require
```

| Field | Required | Description |
|-------|----------|-------------|
| `tls.cert` | yes (if tls set) | PEM-encoded server certificate |
| `tls.key` | yes (if tls set) | PEM-encoded private key |
| `tls.ca` | when clientAuth is optional/require | PEM CA bundle to verify client certificates |
| `tls.clientAuth` | no | `none` (default) — TLS only. `optional` — request client cert, allow without. `require` — reject without valid client cert. |

### TLS on the proxy (client)

```yaml
proxy:
  controlPlaneUrl: "https://cp:8080"
  tls:
    cert: "${PROXY_TLS_CERT}"    # Client cert for mTLS
    key: "${PROXY_TLS_KEY}"      # Client private key
    ca: "${CP_CA}"               # CA to verify the CP server cert
```

| Field | Required | Description |
|-------|----------|-------------|
| `tls.cert` + `tls.key` | for mTLS | Client certificate presented to the CP |
| `tls.ca` | no | CA bundle to verify the CP cert. Uses system CAs if empty. |

### API key authentication

```yaml
controlPlane:
  auth:
    apiKeys:
      - name: "proxy-fleet"
        key: "${PROXY_API_KEY}"
      - name: "operator"
        key: "${OPERATOR_API_KEY}"
```

Clients send `Authorization: Bearer <key>` on every request. When `auth` is absent, no authentication is required (dev mode). The proxy configures its key via `proxy.apiKey`.

| Field | Required | Description |
|-------|----------|-------------|
| `auth.apiKeys[].name` | yes | Human-readable label |
| `auth.apiKeys[].key` | yes | Bearer token value |

### Deployment modes

| Mode | CP config | Proxy config | Security |
|------|-----------|--------------|----------|
| **Dev** | no `tls`, no `auth` | `http://` URL | None — plain HTTP, no auth |
| **TLS + API key** | `tls` (cert+key), `auth` | `tls` (ca), `apiKey` | Encrypted + identified |
| **Full mTLS + API key** | `tls` (cert+key+ca, clientAuth), `auth` | `tls` (cert+key+ca), `apiKey` | Encrypted + transport-auth + identified |

## At-rest encryption

Secrets and snapshots in bbolt can be encrypted with AES-256-GCM. When absent, data is stored in plaintext (dev mode).

```yaml
controlPlane:
  encryption:
    key: "${ENCRYPTION_KEY}"   # base64-encoded 32-byte key
```

Generate a key:

```bash
openssl rand -base64 32
```

| Field | Required | Description |
|-------|----------|-------------|
| `encryption.key` | yes (if encryption set) | Base64-encoded 32-byte AES-256 key |

### Mode detection

On startup, Vrata checks whether the data in bbolt is encrypted or not and compares with the config:

| Config | Data | Result |
|--------|------|--------|
| No `encryption` | Plaintext | Dev mode, works |
| No `encryption` | Encrypted | Error, exit |
| `encryption.key` set | Encrypted | Production mode, works |
| `encryption.key` set | Plaintext | Error, exit |

If you need to switch modes, dump the data, wipe the bbolt file, and restore with the new config.

### What is encrypted

Only sensitive buckets are encrypted:
- **Secrets** — the full Secret entity (ID, Name, Value)
- **Snapshots** — the full snapshot payload (contains resolved secret values)

Routes, groups, listeners, destinations, and middlewares are stored in plaintext — they contain no sensitive material.
