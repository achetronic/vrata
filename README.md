<p align="center">
  <img src="docs/images/header.svg" alt="Vrata — The last gateway you'll configure" width="100%"/>
</p>

<p align="center">
  A programmable HTTP reverse proxy. One binary, zero dependencies.<br/>
  Configure everything through a REST API. Changes apply instantly.
</p>

---

## What is Vrata?

Vrata is a modern API gateway built from scratch. Instead of covering every possible use case with hundreds of features and plugins, it borrows the best ideas from existing proxies, discards what most API gateways never need, and redesigns the rest with a clean, minimal API.

You configure it entirely through a REST API — no config files, no CRDs, no reloads. Create listeners, destinations, routes, and middlewares via HTTP calls. Capture a versioned snapshot. Activate it. All proxies receive the new config atomically via SSE. Bad deploy? Activate the previous snapshot. One call, instant rollback.

## Features

<details>
<summary>🔹 Smart routing with CEL expressions</summary>
<br/>

Match on path, headers, methods, query params, hostnames, gRPC — or write [CEL expressions](https://github.com/google/cel-go) for cross-field logic that static matchers can't express. Every regex is compiled once at build time.

```
request.path.startsWith("/api") && "admin" in request.headers["x-role"] && request.method != "DELETE"
```

</details>

<details>
<summary>🔹 Two-level load balancing with proper sticky sessions</summary>
<br/>

Two independent balancing levels — the first picks which service (3 algorithms), the second picks which pod (6 algorithms):

| | Destination (which service?) | Endpoint (which pod?) |
|---|---|---|
| **Simple** | Weighted random | Round robin, random |
| **Sticky** | Consistent hash (cookie) | Ring hash, maglev (header/cookie/IP) |
| **Zero-disruption** | Redis-backed sticky | Redis-backed sticky |
| **Smart** | — | Least request (power of two choices) |

</details>

<details>
<summary>🔹 Request and response interception</summary>
<br/>

- **External processor** — your gRPC or HTTP service receives each request/response phase and can mutate headers, replace bodies, or reject. Supports buffered, partial-buffered, and streamed body modes.
- **External authorization** — delegate auth decisions to an external service (HTTP or gRPC).
- **Header manipulation** — add, remove, or replace request/response headers with variable interpolation.
- **Request mirroring** — shadow traffic to a secondary destination for testing or debugging. Configurable percentage.
- **Access log** — per-request structured logging with method, path, status, duration, and original path preservation.

</details>

<details>
<summary>🔹 Security and access control</summary>
<br/>

- **JWT validation** — RSA, ECDSA, Ed25519. Remote JWKS or inline keys. CEL-based claim assertions. Claim-to-header injection.
- **Inline authorization** — ordered CEL rules with first-match-wins semantics. Allow/deny based on path, headers, method, body content, or client certificate identity — no external service needed.
- **Rate limiting** — token bucket per client IP with trusted proxy support.
- **CORS** — origin matching (exact, regex, wildcard), preflight, credentials.
- **CEL conditions on any middleware** — `skipWhen` / `onlyWhen` control exactly when a middleware runs.
- **mTLS on listeners** — optional or required client certificates. Client cert metadata (`request.tls.peerCertificate.*`) available in CEL. Automatic XFCC header injection.
- **CEL body access** — `request.body.raw` and `request.body.json` for request body inspection in route matching, conditions, and authorization rules. Lazy buffering, zero overhead when unused.
- **Secrets** — first-class entities for sensitive values (TLS certs, keys, tokens). Referenced via `{{secret:value/env/file}}` and resolved at snapshot time. The proxy never sees unresolved references.
- **At-rest encryption** — AES-256-GCM for secrets and snapshots in bbolt. Optional, key via config.
- **Control plane TLS + mTLS + API keys** — secure the CP↔proxy/controller channel with TLS, optional mutual TLS, and bearer token authentication.

</details>

<details>
<summary>🔹 Resilience — retries, circuit breakers, structured error responses</summary>
<br/>

- **Retries** with exponential backoff and configurable conditions
- **Circuit breaker** per destination with half-open probe
- **Health checks** — active HTTP probes per endpoint
- **Outlier detection** — passive ejection based on consecutive errors
- **Structured proxy errors** — configurable detail level per listener (`minimal`, `standard`, `full`) for all infrastructure failures

</details>

<details>
<summary>🔹 Versioned snapshots with instant rollback</summary>
<br/>

Changes are staged via the API. Nothing goes live until you capture a snapshot and activate it. All proxies receive the new config atomically. Rollback is one API call.

</details>

<details>
<summary>🔹 22 Prometheus metrics across 5 dimensions</summary>
<br/>

Route, destination, endpoint, middleware, and listener metrics — each independently toggleable per listener. Custom histogram buckets. Endpoint dimension off by default to control cardinality.

</details>

<details>
<summary>🔹 Kubernetes native</summary>
<br/>

- **EndpointSlice watching** for automatic pod discovery
- **Helm chart** with control plane, proxy fleet, and optional Gateway API controller
- **Gateway API controller** that syncs HTTPRoute, Gateway, and SuperHTTPRoute resources

</details>

<details>
<summary>🔹 HA control plane with embedded Raft</summary>
<br/>

3-5 node Raft consensus. Any node accepts reads and writes. DNS peer discovery. Automatic failover. No external dependencies.

</details>

## Documentation

Full documentation is available at **[achetronic.github.io/vrata](https://achetronic.github.io/vrata/)**.

Covers getting started, installation (binary, Docker, Helm), configuration, all concepts in depth, API reference, and the Kubernetes controller.

## Contributing

```bash
git clone https://github.com/achetronic/vrata.git
cd vrata
make build
make test
```

Please read the [conventions](.agents/CONVENTIONS.md) before submitting code. The `.agents/` directory contains the full architecture documentation and design decisions.

## License

Apache 2.0
