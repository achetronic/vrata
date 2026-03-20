---
title: "HA with Raft"
weight: 2
---

Run 3-5 control plane nodes with embedded Raft consensus for high availability. No external etcd, no external database.

## Configuration

```yaml
mode: "controlplane"
controlPlane:
  address: ":8080"
  storePath: "/data"
  raft:
    nodeId: "${POD_NAME}"
    bindAddress: ":7000"
    advertiseAddress: "${POD_IP}:7000"
    discovery:
      dns: "vrata-headless.vrata.svc.cluster.local"
```

## All Raft fields

| Field | Default | Description |
|-------|---------|-------------|
| `raft.nodeId` | required | Unique node identifier (use `${POD_NAME}` in Kubernetes) |
| `raft.bindAddress` | required | Address for Raft inter-node communication |
| `raft.advertiseAddress` | — | Address other nodes use to reach this one (use `${POD_IP}:7000` in Kubernetes) |
| `raft.discovery.dns` | — | Headless Service FQDN for automatic peer discovery |
| `raft.peers` | — | Static peer list (alternative to DNS discovery) |

Either `discovery.dns` or `peers` must be set — not both.

## How it works

- Every node has a full copy of the config in its local bbolt.
- One node is the Raft leader. Only the leader commits writes.
- Followers that receive writes forward them transparently to the leader.
- Reads go directly to the local bbolt — no Raft round-trip.
- If the leader dies, Raft elects a new one in seconds.
- Proxies connect via a Kubernetes Service that load-balances across all nodes.

## Peer discovery

### DNS discovery (recommended for Kubernetes)

```yaml
raft:
  discovery:
    dns: "vrata-headless.vrata.svc.cluster.local"
```

Vrata resolves the headless Service and discovers all peers automatically. New nodes join the cluster as they appear in DNS.

### Static peers

```yaml
raft:
  peers:
    - "cp-0=10.0.0.1:7000"
    - "cp-1=10.0.0.2:7000"
    - "cp-2=10.0.0.3:7000"
```

For bare metal or environments without DNS service discovery.

## Scaling

| Nodes | Tolerates | Recommendation |
|-------|-----------|---------------|
| 1 | 0 failures | Development only |
| 3 | 1 failure | Minimum for production |
| 5 | 2 failures | Maximum recommended |

Avoid even numbers — Raft needs a majority for quorum (2 of 3, 3 of 5). Beyond 5 nodes, write latency increases without meaningful benefit.

## Kubernetes example

```yaml
# Helm values
controlPlane:
  replicas: 3
  config:
    controlPlane:
      address: ":8080"
      storePath: "/data"
      raft:
        nodeId: "${POD_NAME}"
        bindAddress: ":7000"
        advertiseAddress: "${POD_IP}:7000"
        discovery:
          dns: "vrata-control-plane-headless.vrata.svc.cluster.local"
```
