---
title: "Helm"
weight: 3
---

Deploy Vrata on Kubernetes using the official Helm chart. Includes the control plane, proxy fleet, and optional Kubernetes controller.

The chart is published as an OCI artifact in the GitHub Container Registry — no repo to add.

## Install

```bash
helm install vrata oci://ghcr.io/achetronic/vrata/helm-chart/vrata \
  --namespace vrata \
  --create-namespace
```

This deploys a single control plane node with a persistent volume, one proxy replica, and the controller disabled.

## Minimal values

```yaml
controlPlane:
  enabled: true
  replicas: 1
  config:
    controlPlane:
      address: ":8080"
      storePath: "/data"

proxy:
  enabled: true
  replicas: 2
  config:
    proxy:
      controlPlaneUrl: "http://vrata-control-plane:8080"

controller:
  enabled: false
```

The `config` sections are free YAML maps — they're rendered as-is into the ConfigMap that the pods mount.

## With the Gateway API controller

```yaml
controlPlane:
  enabled: true
  replicas: 1
  config:
    controlPlane:
      address: ":8080"
      storePath: "/data"

proxy:
  enabled: true
  replicas: 2
  config:
    proxy:
      controlPlaneUrl: "http://vrata-control-plane:8080"

controller:
  enabled: true
  installCRDs: true
  config:
    controlPlaneUrl: "http://vrata-control-plane:8080"
    watch:
      gatewayClassName: "vrata"
```

This also installs the SuperHTTPRoute CRD and sets up RBAC for the controller.

## HA control plane (Raft)

```yaml
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

Use 3 or 5 replicas. Avoid even numbers — Raft needs a majority for quorum.

## Verify

```bash
# Port-forward the control plane
kubectl port-forward -n vrata svc/vrata-control-plane 8080:8080

# Swagger UI
open http://localhost:8080/api/v1/docs/

# Create entities via the API
curl -s -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{"name": "main", "port": 3000}'
```

## Uninstall

```bash
helm uninstall vrata -n vrata
```

PersistentVolumeClaims are not deleted automatically. Remove them manually if you want a clean slate:

```bash
kubectl delete pvc -n vrata -l app.kubernetes.io/instance=vrata
```
