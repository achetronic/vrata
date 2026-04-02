---
title: "Gateway Support"
weight: 5
---

The controller translates Gateway API Gateway resources into Vrata Listeners.

## GatewayClass

The controller claims a GatewayClass with `spec.controllerName: vrata.io/controller`. Only Gateways referencing this class are reconciled.

The default expected `gatewayClassName` is `vrata`. This is configurable:

```yaml
watch:
  gatewayClassName: "vrata"   # default
```

The controller writes `Accepted: True` on any GatewayClass matching its controller name.

## Gateway → Listener mapping

| Gateway field | Vrata Listener field | Notes |
|---------------|---------------------|-------|
| `spec.listeners[].name` | `name` (as `k8s:{ns}/{gw}/{listener}`) | Used for ownership |
| `spec.listeners[].port` | `port` | Direct mapping |
| `spec.listeners[].protocol` | TLS flag | `HTTPS`/`GRPCS`/`TLS` → TLS enabled |
| `spec.listeners[].hostname` | — | Read for future binding |
| `spec.addresses` | — | Not mapped (hardcoded `0.0.0.0`) |

## Supported protocols

| Protocol | Supported | TLS |
|----------|-----------|-----|
| `HTTP` | Yes | No |
| `HTTPS` | Yes | Yes |
| `GRPC` | Yes | No |
| `GRPCS` | Yes | Yes |
| `TCP` | No | — |
| `UDP` | No | — |

Unsupported protocols are logged with a listener condition `Accepted: False` / `UnsupportedProtocol`.

## Listener lifecycle

The controller creates, updates, and deletes Vrata Listeners to match the Gateway spec:

- **Create**: when a new Gateway listener appears
- **Update**: when port, protocol, or TLS configuration changes
- **Delete**: when a Gateway or listener is removed (orphan GC)

## Gateway status

The controller writes the following conditions on Gateway resources:

| Condition | Status | When |
|-----------|--------|------|
| `Accepted: True` | All listeners valid | Reason: `Accepted` |
| `Accepted: True` | Some listeners invalid | Reason: `ListenersNotValid` |
| `Programmed: True` | All listeners running in Vrata | Reason: `Programmed` |
| `Programmed: False` | Some listeners failed | Reason: `Invalid` |

Per-listener conditions:

| Condition | Status | When |
|-----------|--------|------|
| `Accepted: True` | Protocol supported | Reason: `Accepted` |
| `Accepted: False` | Protocol unsupported | Reason: `UnsupportedProtocol` |
| `Programmed: True` | Listener running | Reason: `Programmed` |

## Limitations

- **TLS certificates**: Gateway references Secrets for TLS, but Vrata Listeners expect inline PEM or file paths. TLS cert propagation is not yet implemented — see the [TLS gap](/docs/clients/controller/httproute/#known-gaps).
- **AllowedRoutes**: the `spec.listeners[].allowedRoutes` field (namespace filtering, kind restrictions) is not yet enforced.
- **Addresses**: `spec.addresses` is not mapped. Listeners always bind to `0.0.0.0`.
