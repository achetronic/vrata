# Envoy xDS Control Plane — Design & Status

**Branch**: `feat/envoy-xds-control-plane`
**Date**: 2026-03-29
**Status**: xDS translator feature-complete, extensions wired

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

## Model change: Listener.GroupIDs (pending redesign)

The only model change vs main branch: `model.Listener` gains a `GroupIDs []string` field.

- Empty = attach all groups (catch-all, same behaviour as native proxy).
- Non-empty = only the listed groups are attached to this listener.

This is needed because Envoy requires explicit routing topology (Listener → RouteConfiguration → VirtualHost), while the native proxy inferred it at runtime.

**Open issue**: the owner considers routes (not just groups) as first-class citizens,
so `GroupIDs` on Listener doesn't feel right. This needs redesigning before stabilising
the API. Possible directions: attach routes directly to listeners, or rethink the
listener→group→route topology entirely. Do not invest in GroupIDs-dependent features
until this is resolved.

---

## xDS Translation

| Vrata entity | Envoy resource |
|---|---|
| `Listener` (with `GroupIDs`) | `envoy.Listener` + HCM filter + RDS |
| `Listener.TLS` | `DownstreamTlsContext` on filter chain |
| `Listener.TLS.ClientAuth` | mTLS with `require_client_certificate` + CA validation |
| `RouteGroup` + `Route` | `RouteConfiguration` with `VirtualHost` per group |
| `Destination` (with Endpoints) | `Cluster` (EDS/STATIC/STRICT_DNS auto-derived) + `ClusterLoadAssignment` |
| `Destination.Options.TLS` | `UpstreamTlsContext` on cluster transport socket |
| `Destination.Options.EndpointBalancing` | Cluster `LbPolicy` (ROUND_ROBIN, LEAST_REQUEST, RING_HASH, MAGLEV, RANDOM) |
| `Destination.Options.CircuitBreaker` | Cluster `CircuitBreakers.Thresholds` |
| `Destination.Options.OutlierDetection` | Cluster `OutlierDetection` |
| `Destination.Options.HealthCheck` | Cluster `HealthChecks` (HTTP) |
| `Destination.Options.HTTP2` | Cluster `Http2ProtocolOptions` |
| Multiple `DestinationRef` with weights | `WeightedCluster` in route action |
| `Match.PathRegex` | `SafeRegex` (RE2, no restrictions unlike Istio) |
| `Match.Headers` | Envoy `HeaderMatcher` |
| `Forward.Timeouts.Request` | Route-level timeout |
| `Forward.Retry` | Route `RetryPolicy` (conditions, backoff, per-attempt timeout) |
| `Forward.Rewrite` | Route `PrefixRewrite` / `RegexRewrite` / host rewrite |
| `Forward.Mirror` | Route `RequestMirrorPolicies` |
| `Forward.DestinationBalancing` (WCH/STICKY) | Route `HashPolicy` (cookie-based) |
| `Route.Redirect` | Route `RedirectAction` |
| `Route.DirectResponse` | Route `DirectResponseAction` |
| Middleware `cors` | `envoy.filters.http.cors` (native) |
| Middleware `jwt` | `envoy.filters.http.jwt_authn` (native) |
| Middleware `extAuthz` | `envoy.filters.http.ext_authz` (native, HTTP + gRPC) |
| Middleware `rateLimit` | `envoy.filters.http.local_ratelimit` (native) |
| Middleware `headers` | `envoy.filters.http.header_mutation` (native) |
| Middleware `accessLog` | HCM `access_log` with `envoy.access_loggers.file` |
| Middleware `inlineAuthz` | Go plugin `vrata.inlineauthz` |
| xfcc (auto on mTLS) | Go plugin `vrata.xfcc` |

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

## Done

- [x] TLS termination in Envoy Listener (`DownstreamTlsContext` with cert/key file paths)
- [x] mTLS client auth in Envoy Listener (`require_client_certificate` + CA validation)
- [x] Filter factory/registration for sticky, inlineauthz, xfcc (all via `init()`)
- [x] Envoy bootstrap config example (`extensions/ENVOY_BOOTSTRAP.md`)
- [x] CORS as native Envoy filter (`envoy.filters.http.cors`)
- [x] JWT as native Envoy filter (`envoy.filters.http.jwt_authn`, local + remote JWKS)
- [x] ExtAuthz as native Envoy filter (`envoy.filters.http.ext_authz`, HTTP + gRPC)
- [x] RateLimit as native Envoy filter (`envoy.filters.http.local_ratelimit`, token bucket)
- [x] Headers as native Envoy filter (`envoy.filters.http.header_mutation`, request + response add/remove)
- [x] AccessLog via HCM (`envoy.access_loggers.file`, JSON + text, Vrata var → Envoy command operator mapping)
- [x] Circuit breaker via Envoy native (`Cluster.CircuitBreakers.Thresholds`)
- [x] Outlier detection via Envoy native (`Cluster.OutlierDetection`)
- [x] Health checks via Envoy native (`Cluster.HealthChecks`, HTTP)
- [x] Cluster type auto-derived (EDS / STATIC / STRICT_DNS from destination fields)
- [x] LB policy translation (ROUND_ROBIN, LEAST_REQUEST, RING_HASH, MAGLEV, RANDOM)
- [x] Ring hash / Maglev config (ring size, table size)
- [x] Upstream TLS (`UpstreamTlsContext` with SNI, CA, mTLS client certs)
- [x] HTTP/2 upstream (`Http2ProtocolOptions` on cluster)
- [x] Connect timeout from destination timeouts
- [x] Max requests per connection on cluster
- [x] Route redirect action (`RedirectAction` with scheme, host, path, code)
- [x] Route direct response action (`DirectResponseAction` with status + body)
- [x] Route retry policy (conditions, backoff, per-attempt timeout)
- [x] Route URL rewrite (prefix, regex, host literal/header/auto)
- [x] Route request mirror (cluster + runtime fraction percentage)
- [x] Route hash policy for WCH and STICKY destination balancing (cookie-based)
- [x] xfcc Go plugin auto-injected when listener has mTLS
- [x] inlineAuthz Go plugin injected for `inlineAuthz` middleware type

## Pending

- [ ] **Listener.GroupIDs redesign** — owner considers routes first-class, GroupIDs on Listener doesn't fit. Needs rethinking before stabilising API.
- [ ] sticky filter: response-side pinning (write session→destination to Redis on first response)
- [ ] extensions: go mod tidy + go.sum (needs network access to resolve deps)
- [ ] ExtProc as native Envoy filter (`envoy.filters.http.ext_proc`)
- [ ] sticky Go plugin injection in HCM when routes use STICKY destination balancing
- [ ] CEL match expressions via `envoy.filters.http.rbac` or custom Go plugin
- [ ] Query param matchers in route match
- [ ] Port matchers in route match
- [ ] gRPC content-type match in route match
- [ ] xDS translator unit tests
- [ ] E2E tests with Envoy
