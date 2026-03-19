---
title: "SuperHTTPRoute"
weight: 4
---

## The problem

Gateway API's HTTPRoute has hard limits enforced via OpenAPI validation:

- **16 hostnames** per HTTPRoute
- **64 rules** per HTTPRoute
- CEL validations that prevent larger objects

These limits come from etcd's object size constraint. If you serve the same app on 74 country subdomains, you need 5 identical HTTPRoutes just to shard hostnames.

## The solution

SuperHTTPRoute is a custom CRD (`vrata.io/v1`) that is byte-for-byte identical to HTTPRoute but without `maxItems` and CEL validation constraints. The spec is exactly the same — same fields, same structure, same semantics. Only the limits are removed.

## Generating the CRD

The CRD is generated from Go types that wrap `gwapiv1.HTTPRouteSpec`:

```bash
make controller-generate-crd
```

This runs:
1. `controller-gen` generates the raw CRD from the Go types
2. `scripts/crdclean` strips all `maxItems` and `x-kubernetes-validations`
3. `scripts/helmwrap` adds Helm template guards

Output: `charts/vrata/templates/controller/superhttproute-crd.yaml`

## Installing

Via Helm:

```yaml
controller:
  enabled: true
  installCRDs: true
```

Or manually:

```bash
make controller-deploy-crd
```

## Using it

Create a SuperHTTPRoute exactly like an HTTPRoute, just with a different API group:

```yaml
apiVersion: vrata.io/v1
kind: SuperHTTPRoute
metadata:
  name: all-locales
  namespace: frontend
spec:
  hostnames:
    - fr.example.com
    - de.example.com
    - es.example.com
    # ... 74 more hostnames, no limit
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: frontend-svc
          port: 80
```

The controller handles SuperHTTPRoutes and HTTPRoutes with the same mapper — the reconciliation logic is identical.

## Enable in config

```yaml
watch:
  httpRoutes: true
  superHttpRoutes: true
  gateways: true
```
