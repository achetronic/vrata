---
title: "Installation"
weight: 2
---

The controller is included in the Vrata Helm chart — no separate install.

## Prerequisites

**Gateway API CRDs** must be installed before the controller can watch HTTPRoute and Gateway resources:

```bash
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml
```

## Install via Helm

```bash
helm install vrata oci://ghcr.io/achetronic/vrata/helm-chart/vrata \
  --namespace vrata \
  --create-namespace \
  --set controller.enabled=true \
  --set controller.installCRDs=true
```

This deploys:
- Controller Deployment with ServiceAccount and RBAC (read HTTPRoutes/Gateways, write HTTPRoute status)
- SuperHTTPRoute CRD (if `installCRDs: true`)
- ConfigMap with the controller config

## Configuration

The controller reads a YAML config from the ConfigMap. Set it via `controller.config` in your Helm values:

```yaml
controller:
  enabled: true
  installCRDs: true
  config:
    controlPlaneUrl: "http://vrata-control-plane:8080"
    watch:
      httpRoutes: true
      superHttpRoutes: false
      gateways: true
      gatewayClassName: "vrata"
    snapshot:
      debounce: "5s"
      maxBatch: 100
    duplicates:
      mode: "warn"
```

See [Configuration]({{< relref "configuration" >}}) for the full reference of all fields.
