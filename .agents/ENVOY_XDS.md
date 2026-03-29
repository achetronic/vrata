# Envoy xDS Control Plane — Design & Status

**Branch**: `feat/envoy-xds-control-plane`
**Date**: 2026-03-29
**Status**: Scaffold complete, iterating

---

## What this branch does

Replaces the native Go proxy with Envoy as the data plane.
Vrata becomes a pure control plane that:

1. Exposes the same REST API (no API changes for users).
2. Translates Vrata model entities → Envoy xDS resources.
3. Pushes xDS snapshots to a fleet of Envoy instances via ADS (gRPC).
4. Provides Go filter extensions for features Envoy doesn't have natively.

---

## Architecture

```
┌─────────────────────────────────────────┐
│  Vrata Control Plane (server/)          │
│                                         │
│  REST API (:8080)  ←→  Store (BoltDB)   │
│       ↓                                 │
│  Gateway (store events → xDS push)      │
│       ↓                                 │
│  xDS Server (:18000) — ADS/gRPC         │
└──────────────┬──────────────────────────┘
               │  xDS (ADS)
    ┌──────────┴──────────┐
    │  Envoy fleet        │
    │  (DaemonSet/Gateway)│
    │  + Go filter .so    │
    └─────────────────────┘
```

---

## Model change: Listener.GroupIDs

The only model change vs main branch: `model.Listener` gains a `GroupIDs []string` field.

- Empty = attach all groups (catch-all, same behaviour as native proxy).
- Non-empty = only the listed groups are attached to this listener.

This is needed because Envoy requires explicit routing topology (Listener → RouteConfiguration → VirtualHost), while the native proxy inferred it at runtime.

---

## xDS Translation

| Vrata entity | Envoy resource |
|---|---|
| `Listener` (with `GroupIDs`) | `envoy.Listener` + HCM filter + RDS |
| `RouteGroup` + `Route` | `RouteConfiguration` with `VirtualHost` per group |
| `Destination` (with Endpoints) | `Cluster` (EDS) + `ClusterLoadAssignment` |
| Multiple `DestinationRef` with weights | `WeightedCluster` in route action |
| `Match.PathRegex` | `SafeRegex` (RE2, no restrictions unlike Istio) |
| `Match.Headers` | Envoy `HeaderMatcher` |
| `Forward.Timeouts.Request` | Route-level timeout |

---

## Go Filter Extensions (extensions/)

Each extension is an independent Go module compiled as `plugin` (.so).
Loaded by Envoy via `envoy.filters.http.golang` filter.

### extensions/sticky/
Redis-backed sticky session routing. Reads session cookie, looks up pinned
destination in Redis, injects `x-vrata-sticky-destination` header.
Config via env vars: `VRATA_STICKY_REDIS_ADDR`, `VRATA_STICKY_COOKIE_NAME`, etc.

### extensions/inlineauthz/
CEL-based authorization evaluated locally. Equivalent to Vrata's `inlineAuthz`
middleware. Lazy body buffering: only buffers when a rule references `request.body`.
Config via `VRATA_AUTHZ_RULES_JSON` (JSON array of `{cel, action}` rules).

### extensions/xfcc/
X-Forwarded-Client-Cert injection with spoof protection. Strips incoming XFCC,
injects new one from Envoy's verified TLS client cert metadata.
No configuration required.

---

## Building extensions

```bash
# Build all extensions as .so plugins
cd extensions/sticky   && go build -buildmode=plugin -o sticky.so .
cd extensions/inlineauthz && go build -buildmode=plugin -o inlineauthz.so .
cd extensions/xfcc     && go build -buildmode=plugin -o xfcc.so .

# Or build the Envoy image with extensions baked in
docker build -t vrata-envoy -f extensions/Dockerfile .
```

---

## Pending

- [ ] TLS termination in Envoy Listener (xDS translator needs to inject TLS context)
- [ ] mTLS client auth in Envoy Listener (inject `DownstreamTlsContext` with `require_client_certificate`)
- [ ] sticky filter: response-side pinning (write session→destination to Redis on first response)
- [ ] inlineauthz: wire filter factory/registration in main()
- [ ] xfcc: wire filter factory/registration in main()
- [ ] sticky: wire filter factory/registration in main()
- [ ] extensions: go mod tidy + go.sum (needs network access to resolve deps)
- [ ] Envoy bootstrap config example (static_resources with xDS cluster pointing to :18000)
- [ ] CORS, JWT, RateLimit, AccessLog as extensions or native Envoy filters
- [ ] circuit breaker / outlier detection via Envoy native (DestinationPolicy in xDS Cluster)
