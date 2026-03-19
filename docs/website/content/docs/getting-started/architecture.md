---
title: "Architecture"
weight: 3
---

Vrata has two components packaged in one binary. You choose the mode at startup.

{{< img src="images/architecture.svg" alt="Vrata architecture" >}}

## Control Plane

The control plane exposes the REST API, stores configuration in an embedded bbolt database, and pushes active snapshots to connected proxies via SSE (Server-Sent Events). It also serves the Swagger UI for API exploration.

```yaml
mode: "controlplane"
controlPlane:
  address: ":8080"
  storePath: "/data"
```

The control plane is **off the data path** — it only pushes configuration. If it goes down, proxies keep routing with their last snapshot.

## Proxy

The proxy is stateless. It connects to a control plane via SSE, receives configuration snapshots, builds a routing table, and routes traffic. Run 1 or 100 — they're disposable.

```yaml
mode: "proxy"
proxy:
  controlPlaneUrl: "http://control-plane:8080"
  reconnectInterval: "5s"
```

Each proxy opens its own listeners, applies middlewares, balances across destinations, and exposes Prometheus metrics — all driven by the snapshot it received.

## HA Control Plane

For production, run 3-5 control plane nodes with embedded Raft consensus:

```yaml
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

Any node accepts reads and writes. Followers forward writes to the leader transparently. If the leader dies, Raft elects a new one. Proxies reconnect to any surviving node.

## Clients

The REST API is the only interface. Anything that speaks HTTP can be a client:

- **curl / scripts** — manual operations
- **Kubernetes Controller** — watches Gateway API resources and syncs them to Vrata
- **CI/CD pipelines** — programmatic route management
- **UI** — planned
