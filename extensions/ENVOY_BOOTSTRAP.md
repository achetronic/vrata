## Envoy Bootstrap Configuration

This is an example static bootstrap config that connects Envoy to the Vrata
xDS control plane. Save it as `envoy.yaml` and mount it into the container.

The key parts:
- `dynamic_resources.ads_config` points to the Vrata xDS gRPC server.
- `node.id` identifies this Envoy node to the control plane.
- All listeners, routes, and clusters come from xDS dynamically.

```yaml
node:
  cluster: vrata-fleet
  id: envoy-1  # Unique per node. Use pod name in Kubernetes.

dynamic_resources:
  ads_config:
    api_type: GRPC
    transport_api_version: V3
    grpc_services:
    - envoy_grpc:
        cluster_name: vrata_xds
  lds_config:
    resource_api_version: V3
    ads: {}
  cds_config:
    resource_api_version: V3
    ads: {}

static_resources:
  clusters:
  # The xDS control plane cluster. This one is static — everything else is dynamic.
  - name: vrata_xds
    type: STRICT_DNS
    connect_timeout: 5s
    http2_protocol_options: {}
    load_assignment:
      cluster_name: vrata_xds
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                # Replace with your Vrata control plane address
                address: vrata-control-plane
                port_value: 18000

admin:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 9901
```

## Using Go filter extensions

To use the sticky, inlineauthz, or xfcc extensions, the .so files must be
present in the Envoy container (see extensions/Dockerfile), and the listener
config pushed via xDS must reference them.

The xDS translator will inject Go filter configs into HCM when the
corresponding middlewares are configured in Vrata. This is pending
implementation (tracked in ENVOY_XDS.md).

Until then, you can reference them manually in a static Envoy config:

```yaml
http_filters:
- name: envoy.filters.http.golang
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.golang.v3alpha.Config
    library_id: sticky
    library_path: /etc/envoy/extensions/sticky.so
    plugin_name: vrata.sticky
    plugin_config:
      "@type": type.googleapis.com/google.protobuf.Any
      value: {}
- name: envoy.filters.http.router
  typed_config:
    "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```
