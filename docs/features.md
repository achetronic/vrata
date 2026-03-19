# Vrata — Feature Reference

Vrata is a programmable HTTP reverse proxy with a REST API. You create listeners, destinations, routes, groups, and middlewares — all via API calls. Changes apply instantly, no restarts.

**Base URL**: `http://localhost:8080/api/v1`

---

## Table of Contents

- [Listeners](#listeners)
- [Destinations](#destinations)
  - [Static Endpoints](#static-endpoints)
  - [Kubernetes Discovery](#kubernetes-discovery)
  - [Endpoint Balancing](#endpoint-balancing)
  - [TLS to Upstream](#tls-to-upstream)
  - [Circuit Breaker](#circuit-breaker)
  - [Health Checks](#health-checks)
  - [Outlier Detection](#outlier-detection)
- [Routes](#routes)
  - [Match Rules](#match-rules)
  - [Forward Action](#forward-action)
  - [Destination Balancing](#destination-balancing)
  - [Retry](#retry)
  - [Timeouts](#timeouts)
  - [URL Rewrite](#url-rewrite)
  - [Traffic Mirror](#traffic-mirror)
  - [Redirect](#redirect)
  - [Direct Response](#direct-response)
- [Groups](#groups)
- [Snapshots](#snapshots)
- [Middlewares](#middlewares)
  - [CORS](#cors)
  - [Headers](#headers)
  - [Rate Limit](#rate-limit)
  - [JWT](#jwt)
  - [External Authorization (ExtAuthz)](#external-authorization)
  - [External Processor (ExtProc)](#external-processor)
  - [Access Log](#access-log)
- [Middleware Overrides](#middleware-overrides)
- [Metrics](#metrics)

---

## Listeners

A listener opens a port and accepts HTTP traffic. The proxy routes requests arriving on that port.

```console
POST /api/v1/listeners
GET  /api/v1/listeners
GET  /api/v1/listeners/{id}
PUT  /api/v1/listeners/{id}
DELETE /api/v1/listeners/{id}
```

```json
{
  "name": "main",
  "address": "0.0.0.0",
  "port": 3000
}
```

With TLS:

```json
{
  "name": "secure",
  "port": 443,
  "tls": {
    "certPath": "/certs/tls.crt",
    "keyPath": "/certs/tls.key",
    "minVersion": "TLSv1_2"
  }
}
```

| Field                 | Type   | Description                      |
| --------------------- | ------ | -------------------------------- |
| `name`                | string | Human-readable label             |
| `address`             | string | Bind address. Default: `0.0.0.0` |
| `port`                | number | **Required**. TCP port           |
| `tls`                 | object | TLS termination config           |
| `http2`               | bool   | Enable HTTP/2                    |
| `serverName`          | string | Server header value              |
| `maxRequestHeadersKB` | number | Max header size in KB            |

---

## Destinations

A destination is a backend service your routes forward traffic to. It can be a single host, a list of endpoints, or a Kubernetes Service with auto-discovered pods.

```console
POST /api/v1/destinations
GET  /api/v1/destinations
GET  /api/v1/destinations/{id}
PUT  /api/v1/destinations/{id}
DELETE /api/v1/destinations/{id}
```

Simplest destination — one host:

```json
{
  "name": "backend-api",
  "host": "10.0.0.5",
  "port": 8080
}
```

### Static Endpoints

When your backend has multiple instances and you want Vrata to balance between them directly (without Kubernetes), list them explicitly:

```json
{
  "name": "backend-api",
  "host": "backend.internal",
  "port": 8080,
  "endpoints": [
    { "host": "10.0.0.1", "port": 8080 },
    { "host": "10.0.0.2", "port": 8080 },
    { "host": "10.0.0.3", "port": 8080 }
  ]
}
```

When `endpoints` is set, Vrata routes to those addresses instead of `host:port`. The `host` field is still used for TLS SNI and display purposes.

If `endpoints` is empty or absent, Vrata uses `host:port` as the sole endpoint.

### Kubernetes Discovery

For Kubernetes Services, Vrata can watch EndpointSlices and route directly to pod IPs:

```json
{
  "name": "k8s-app",
  "host": "my-svc.my-namespace.svc.cluster.local",
  "port": 8080,
  "options": {
    "discovery": { "type": "kubernetes" }
  }
}
```

Vrata parses the FQDN to extract namespace and service name, watches EndpointSlices, and updates the endpoint list automatically when pods scale up or down.

### Endpoint Balancing

When a destination has multiple endpoints (static or discovered), `endpointBalancing` controls which endpoint receives each request.

```json
{
  "name": "backend",
  "host": "backend.internal",
  "port": 8080,
  "endpoints": [
    { "host": "10.0.0.1", "port": 8080 },
    { "host": "10.0.0.2", "port": 8080 },
    { "host": "10.0.0.3", "port": 8080 }
  ],
  "options": {
    "endpointBalancing": {
      "algorithm": "ROUND_ROBIN"
    }
  }
}
```

#### Algorithms

**ROUND_ROBIN** — Cycles through endpoints in order. Even distribution.

```json
{ "algorithm": "ROUND_ROBIN" }
```

**RANDOM** — Picks a random endpoint each time.

```json
{ "algorithm": "RANDOM" }
```

**LEAST_REQUEST** — Picks the endpoint with the fewest active requests. Best under concurrent load.

```json
{
  "algorithm": "LEAST_REQUEST",
  "leastRequest": {
    "choiceCount": 2
  }
}
```

**RING_HASH** — Consistent hash. Same input → same endpoint. Supports header, cookie, or source IP as hash key.

```json
{
  "algorithm": "RING_HASH",
  "ringHash": {
    "ringSize": { "min": 1024, "max": 8388608 },
    "hashPolicy": [{ "header": { "name": "X-User-ID" } }]
  }
}
```

With auto-generated cookie (endpoint stickiness):

```json
{
  "algorithm": "RING_HASH",
  "ringHash": {
    "hashPolicy": [{ "cookie": { "name": "_vrata_endpoint_pin", "ttl": "1h" } }]
  }
}
```

With source IP:

```json
{
  "algorithm": "RING_HASH",
  "ringHash": {
    "hashPolicy": [{ "sourceIP": { "enabled": true } }]
  }
}
```

**MAGLEV** — Google's Maglev consistent hash. Similar to RING_HASH but with better distribution uniformity.

```json
{
  "algorithm": "MAGLEV",
  "maglev": {
    "tableSize": 65537,
    "hashPolicy": [{ "header": { "name": "X-User-ID" } }]
  }
}
```

**STICKY** — Zero disruption. Uses an external session store (Redis) to remember which endpoint each client was assigned to. New clients get a random endpoint; existing clients always return to the same one.

```json
{
  "algorithm": "STICKY",
  "sticky": {
    "cookie": { "name": "_vrata_endpoint_pin", "ttl": "24h" }
  }
}
```

Requires `sessionStore` configured in `config.yaml`:

```yaml
sessionStore:
  type: "redis"
  redis:
    address: "${REDIS_ADDRESS}"
    password: "${REDIS_PASSWORD}"
    db: 0
```

#### Hash Policy Reference

Hash policies define what data from the request feeds the hash function. Evaluated in order — the first one that produces a value wins.

| Type       | Description                                      |
| ---------- | ------------------------------------------------ |
| `header`   | Hash on a request header value                   |
| `cookie`   | Hash on a cookie value. Auto-generated if absent |
| `sourceIP` | Hash on the client IP address                    |

All types are objects for consistency:

```json
{ "header":   { "name": "X-User-ID" } }
{ "cookie":   { "name": "_session", "ttl": "1h" } }
{ "sourceIP": { "enabled": true } }
```

### TLS to Upstream

```json
{
  "options": {
    "tls": {
      "mode": "tls",
      "caFile": "/certs/ca.pem",
      "sni": "backend.example.com",
      "minVersion": "TLSv1_2"
    }
  }
}
```

For mutual TLS (mTLS):

```json
{
  "options": {
    "tls": {
      "mode": "mtls",
      "certFile": "/certs/client.crt",
      "keyFile": "/certs/client.key",
      "caFile": "/certs/ca.pem"
    }
  }
}
```

| Mode   | Description                             |
| ------ | --------------------------------------- |
| `none` | Plaintext (default)                     |
| `tls`  | TLS — verify server certificate         |
| `mtls` | Mutual TLS — present client certificate |

### Circuit Breaker

Protects the upstream from overload:

```json
{
  "options": {
    "circuitBreaker": {
      "maxConnections": 100,
      "maxPendingRequests": 50,
      "maxRequests": 200,
      "maxRetries": 3
    }
  }
}
```

### Health Checks

Active HTTP health probes:

```json
{
  "options": {
    "healthCheck": {
      "path": "/healthz",
      "interval": "10s",
      "timeout": "5s",
      "unhealthyThreshold": 3,
      "healthyThreshold": 2
    }
  }
}
```

### Outlier Detection

Passive ejection based on error patterns:

```json
{
  "options": {
    "outlierDetection": {
      "consecutive5xx": 5,
      "consecutiveGatewayErrors": 3,
      "interval": "10s",
      "baseEjectionTime": "30s",
      "maxEjectionPercent": 10
    }
  }
}
```

### Other Options

| Field                      | Description                           |
| -------------------------- | ------------------------------------- |
| `connectTimeout`           | TCP connection timeout. Default: `5s` |
| `http2`                    | Enable HTTP/2 to upstream             |
| `maxRequestsPerConnection` | Drain after N requests. 0 = unlimited |

---

## Routes

A route matches incoming requests and decides what to do: forward to a destination, redirect, or return a fixed response.

```console
POST /api/v1/routes
GET  /api/v1/routes
GET  /api/v1/routes/{id}
PUT  /api/v1/routes/{id}
DELETE /api/v1/routes/{id}
```

Every route has exactly one action: `forward`, `redirect`, or `directResponse`.

### Match Rules

```json
{
  "name": "api-route",
  "match": {
    "pathPrefix": "/api/v1"
  },
  "forward": { "..." }
}
```

| Field         | Description                                 |
| ------------- | ------------------------------------------- |
| `path`        | Exact path match                            |
| `pathPrefix`  | Path prefix match                           |
| `pathRegex`   | RE2 regex match                             |
| `methods`     | HTTP methods: `["GET", "POST"]`             |
| `hostnames`   | Match by Host header: `["api.example.com"]` |
| `headers`     | Match by headers (see below)                |
| `queryParams` | Match by query parameters                   |
| `grpc`        | Match only `application/grpc` content type  |
| `cel`         | CEL expression for complex logic            |

Header matching:

```json
{
  "match": {
    "pathPrefix": "/admin",
    "headers": [{ "name": "X-Role", "value": "admin" }]
  }
}
```

Regex header matching:

```json
{
  "headers": [{ "name": "Authorization", "value": "Bearer .*", "regex": true }]
}
```

Query parameter matching:

```json
{
  "match": {
    "pathPrefix": "/search",
    "queryParams": [{ "name": "beta", "value": "true" }]
  }
}
```

CEL expression (evaluated after all static matchers pass):

```json
{
  "match": {
    "pathPrefix": "/api",
    "cel": "request.method == 'DELETE' && 'x-admin' in request.headers"
  }
}
```

The CEL expression receives a `request` object with: `method`, `path`, `host`, `scheme`, `headers`, `queryParams`, `clientIp`.

### Forward Action

Forward traffic to one or more destinations:

```json
{
  "name": "api-route",
  "match": { "pathPrefix": "/api" },
  "forward": {
    "destinations": [{ "destinationId": "dest-1", "weight": 100 }]
  }
}
```

Traffic splitting (canary):

```json
{
  "forward": {
    "destinations": [
      { "destinationId": "stable", "weight": 90 },
      { "destinationId": "canary", "weight": 10 }
    ]
  }
}
```

Weights must sum to 100 when there are multiple destinations.

### Destination Balancing

Controls how Vrata picks a destination when there are multiple. This is **level 1** — choosing between destinations. [Endpoint balancing](#endpoint-balancing) is level 2 — choosing within a destination.

#### Why two levels?

Destination balancing answers: "should this user go to stable or canary?" Endpoint balancing answers: "which pod in stable should handle this request?" Different concerns, different configs, different places.

#### WEIGHTED_RANDOM (default)

Each request picks a destination by weighted random. No stickiness:

```json
{
  "forward": {
    "destinations": [
      { "destinationId": "a", "weight": 70 },
      { "destinationId": "b", "weight": 30 }
    ]
  }
}
```

No `destinationBalancing` field needed — it defaults to `WEIGHTED_RANDOM`.

#### WEIGHTED_CONSISTENT_HASH

Pins each client to a destination using a session cookie and a consistent hash ring. Minimal disruption when weights change:

```json
{
  "forward": {
    "destinations": [
      { "destinationId": "stable", "weight": 90 },
      { "destinationId": "canary", "weight": 10 }
    ],
    "destinationBalancing": {
      "algorithm": "WEIGHTED_CONSISTENT_HASH",
      "weightedConsistentHash": {
        "cookie": {
          "name": "_vrata_destination_pin",
          "ttl": "24h"
        }
      }
    }
  }
}
```

The cookie holds an opaque session ID (UUID). Vrata computes `hash(sessionID + routeID)` → same client always lands on the same destination. Different routes are isolated even with the same cookie name.

When weights change, some clients move (proportional to the weight delta). For a gradual canary rollout (5pp increments), ~5% disruption per step.

#### STICKY

Zero disruption. Uses Redis to remember which destination each client was assigned to. New clients get weighted random; existing clients always return to the same destination, even after weight changes:

```json
{
  "forward": {
    "destinations": [
      { "destinationId": "stable", "weight": 90 },
      { "destinationId": "canary", "weight": 10 }
    ],
    "destinationBalancing": {
      "algorithm": "STICKY",
      "sticky": {
        "cookie": {
          "name": "_vrata_destination_pin",
          "ttl": "24h"
        }
      }
    }
  }
}
```

Requires `sessionStore` in `config.yaml`. Falls back to `WEIGHTED_CONSISTENT_HASH` if Redis is unavailable.

### Retry

Automatic retries on upstream failures:

```json
{
  "forward": {
    "destinations": [{ "destinationId": "api", "weight": 100 }],
    "retry": {
      "attempts": 3,
      "perAttemptTimeout": "5s",
      "on": ["server-error", "connection-failure"],
      "backoff": {
        "base": "100ms",
        "max": "1s"
      }
    }
  }
}
```

| Condition            | Triggers on                                  |
| -------------------- | -------------------------------------------- |
| `server-error`       | 5xx responses                                |
| `connection-failure` | Connection reset, refused                    |
| `gateway-error`      | 502, 503, 504                                |
| `retriable-codes`    | Specific status codes (see `retriableCodes`) |

### Timeouts

```json
{
  "forward": {
    "timeouts": {
      "request": "30s",
      "idle": "5m"
    }
  }
}
```

| Field     | Description                       |
| --------- | --------------------------------- |
| `request` | Total time for the entire request |
| `idle`    | Max time with no data flowing     |

### URL Rewrite

Prefix rewrite:

```json
{
  "forward": {
    "rewrite": { "path": "/internal" }
  }
}
```

Regex rewrite:

```json
{
  "forward": {
    "rewrite": {
      "pathRegex": {
        "pattern": "^/api/v2(.*)",
        "substitution": "/internal$1"
      }
    }
  }
}
```

Host rewrite:

```json
{
  "forward": {
    "rewrite": { "host": "backend.internal.local" }
  }
}
```

Host from header:

```json
{
  "forward": {
    "rewrite": { "hostFromHeader": "X-Original-Host" }
  }
}
```

### Traffic Mirror

Send a copy of traffic to another destination for testing or observability. The mirror response is discarded:

```json
{
  "forward": {
    "destinations": [{ "destinationId": "prod", "weight": 100 }],
    "mirror": {
      "destinationId": "shadow",
      "percentage": 10
    }
  }
}
```

### Redirect

Return an HTTP redirect to the client:

```json
{
  "name": "legacy-redirect",
  "match": { "pathPrefix": "/old" },
  "redirect": {
    "url": "https://app.example.com/new",
    "code": 301
  }
}
```

Partial redirect (change just the scheme):

```json
{
  "redirect": {
    "scheme": "https",
    "code": 308
  }
}
```

### Direct Response

Return a fixed response without contacting any upstream:

```json
{
  "name": "maintenance",
  "match": { "pathPrefix": "/maintenance" },
  "directResponse": {
    "status": 503,
    "body": "Service temporarily unavailable"
  }
}
```

---

## Groups

A group adds a shared prefix, hostnames, or headers on top of multiple routes. Routes are independent entities — a group references them by ID.

```console
POST /api/v1/groups
GET  /api/v1/groups
GET  /api/v1/groups/{id}
PUT  /api/v1/groups/{id}
DELETE /api/v1/groups/{id}
```

Prefix group:

```json
{
  "name": "api-v1",
  "pathPrefix": "/api/v1",
  "routeIds": ["route-1", "route-2"]
}
```

Regex group (i18n):

```json
{
  "name": "i18n-storefront",
  "pathRegex": "/(en|es|fr)",
  "routeIds": ["products-route", "checkout-route"]
}
```

A request to `/es/products` matches because the group regex `/(en|es|fr)` matches `/es` and the route's `pathPrefix: /products` matches `/products`.

Hostname group:

```json
{
  "name": "tenant-acme",
  "hostnames": ["acme.example.com"],
  "routeIds": ["tenant-api"]
}
```

Groups can also carry shared middleware and retry defaults:

```json
{
  "name": "api-v1",
  "pathPrefix": "/api/v1",
  "routeIds": ["r1", "r2"],
  "middlewareIds": ["cors-mw", "auth-mw"],
  "retryDefault": {
    "attempts": 2,
    "on": ["connection-failure"]
  }
}
```

---

## Snapshots

Config changes via the API are staged. Proxies only receive config when an active snapshot exists. This gives you a "publish" step — review before deploying.

```console
POST /api/v1/snapshots              # capture current config
GET  /api/v1/snapshots              # list all
GET  /api/v1/snapshots/{id}         # get one
DELETE /api/v1/snapshots/{id}       # delete
POST /api/v1/snapshots/{id}/activate  # make it live
```

```console
# Create a snapshot
curl -X POST localhost:8080/api/v1/snapshots \
  -H 'Content-Type: application/json' \
  -d '{"name": "v1.2.0"}'

# Activate it
curl -X POST localhost:8080/api/v1/snapshots/{id}/activate
```

Rollback: activate a previous snapshot.

---

## Middlewares

Middlewares process requests before they reach the upstream. Create them as standalone entities, then attach to routes or groups via `middlewareIds`.

```console
POST /api/v1/middlewares
GET  /api/v1/middlewares
GET  /api/v1/middlewares/{id}
PUT  /api/v1/middlewares/{id}
DELETE /api/v1/middlewares/{id}
```

Each middleware has a `type` and a config block matching that type.

### CORS

```json
{
  "name": "cors-public",
  "type": "cors",
  "cors": {
    "allowOrigins": [{ "value": "https://app.example.com" }],
    "allowMethods": ["GET", "POST", "PUT", "DELETE"],
    "allowHeaders": ["Authorization", "Content-Type"],
    "exposeHeaders": ["X-Request-Id"],
    "maxAge": 86400,
    "allowCredentials": true
  }
}
```

Wildcard origin:

```json
{
  "cors": {
    "allowOrigins": [{ "value": "*" }]
  }
}
```

### Headers

Add or remove request/response headers:

```json
{
  "name": "security-headers",
  "type": "headers",
  "headers": {
    "responseHeadersToAdd": [
      { "key": "X-Frame-Options", "value": "DENY" },
      { "key": "X-Content-Type-Options", "value": "nosniff" }
    ],
    "requestHeadersToAdd": [{ "key": "X-Request-Source", "value": "vrata" }],
    "requestHeadersToRemove": ["X-Debug"]
  }
}
```

### Rate Limit

Token bucket per client IP:

```json
{
  "name": "api-ratelimit",
  "type": "rateLimit",
  "rateLimit": {
    "requestsPerSecond": 100,
    "burst": 200
  }
}
```

### JWT

Validates JWT tokens in the `Authorization` header. Each JWT middleware validates tokens from one issuer. For multi-provider setups, create multiple JWT middlewares and use `skipWhen`/`onlyWhen` to control which one runs.

```json
{
  "name": "jwt-auth0",
  "type": "jwt",
  "jwt": {
    "issuer": "https://myapp.auth0.com/",
    "audiences": ["https://api.example.com"],
    "jwksUri": "/.well-known/jwks.json",
    "jwksDestinationId": "auth0-server",
    "forwardJwt": true,
    "claimToHeaders": [
      { "expr": "claims.sub", "header": "X-User-ID" },
      { "expr": "claims.user.email", "header": "X-User-Email" }
    ],
    "assertClaims": [
      "claims.role in ['admin', 'editor']",
      "claims.env == 'production'"
    ]
  }
}
```

Supports RSA (RS256/384/512), ECDSA (ES256/384/512), and Ed25519. JWKS keys are fetched and cached automatically with background refresh.

#### Fields

| Field               | Type     | Description                                             |
| ------------------- | -------- | ------------------------------------------------------- |
| `issuer`            | string   | **Required**. Expected `iss` claim                      |
| `audiences`         | string[] | Expected `aud` values. Empty = skip audience check      |
| `jwksUri`           | string   | URL path to fetch JWKS from (via a Destination)         |
| `jwksDestinationId` | string   | Destination ID hosting the JWKS endpoint                |
| `jwksInline`        | string   | Literal JWKS JSON document (for testing or static keys) |
| `forwardJwt`        | bool     | Forward the `Authorization` header to the upstream      |
| `claimToHeaders`    | array    | Map claims to upstream request headers                  |
| `assertClaims`      | string[] | CEL expressions evaluated against decoded claims        |

Only one of `jwksUri` + `jwksDestinationId` or `jwksInline` is needed.

#### claimToHeaders

Each entry is a CEL expression evaluated against the decoded `claims` map. The result is injected as a request header forwarded to the upstream. Supports nested access, array indexing, and CEL functions.

| Field    | Type   | Description                                              |
| -------- | ------ | -------------------------------------------------------- |
| `expr`   | string | CEL expression against `claims` map. Must return a value |
| `header` | string | Request header name that receives the expression result  |

```json
{
  "claimToHeaders": [
    { "expr": "claims.sub", "header": "X-User-ID" },
    { "expr": "claims.user.id", "header": "X-Internal-ID" },
    { "expr": "claims.roles[0]", "header": "X-Primary-Role" },
    { "expr": "claims.orgs.map(o, o.name).join(',')", "header": "X-Orgs" },
    { "expr": "string(claims.user.tier)", "header": "X-Tier" }
  ]
}
```

#### assertClaims

CEL expressions evaluated against the decoded JWT payload after signature verification. Each expression receives a `claims` map (`string → any`). All must evaluate to `true` or the request gets 403.

```json
{
  "assertClaims": [
    "claims.role == 'admin'",
    "'write' in claims.permissions",
    "claims.org_id == 'acme'"
  ]
}
```

| Detail           | Value                                       |
| ---------------- | ------------------------------------------- |
| Input variable   | `claims` — the decoded JWT payload as a map |
| All must pass    | Yes (AND semantics)                         |
| Failure response | 403 Forbidden                               |
| Compile time     | At middleware build (not per-request)       |

#### JWKS sources

Via Destination:

```json
{
  "jwksUri": "/.well-known/jwks.json",
  "jwksDestinationId": "auth-server"
}
```

Inline (for testing):

```json
{
  "jwksInline": "{\"keys\":[{\"kty\":\"RSA\",\"n\":\"...\",\"e\":\"AQAB\",\"kid\":\"key-1\"}]}"
}
```

#### Multi-provider setup

Instead of configuring multiple providers in one middleware, create separate JWT middlewares and control them with `onlyWhen`:

```json
{
  "middlewareIds": ["jwt-auth0", "jwt-internal"],
  "middlewareOverrides": {
    "jwt-auth0-id": {
      "onlyWhen": ["request.path.startsWith('/api/external')"]
    },
    "jwt-internal-id": {
      "onlyWhen": ["request.path.startsWith('/api/internal')"]
    }
  }
}
```

### External Authorization

Delegates authorization to an external HTTP or gRPC service. The authz service receives the request metadata and returns allow/deny.

```json
{
  "name": "authz",
  "type": "extAuthz",
  "extAuthz": {
    "destinationId": "auth-service-dest",
    "mode": "http",
    "path": "/authorize",
    "timeout": "5s",
    "failureModeAllow": false,
    "includeBody": false,
    "onCheck": {
      "forwardHeaders": ["Authorization", "Cookie", "X-Request-ID"],
      "injectHeaders": [{ "key": "X-Vrata-Source", "value": "proxy" }]
    },
    "onAllow": {
      "copyToUpstream": ["x-auth-user", "x-auth-role"],
      "copyToClient": ["x-ratelimit-remaining"]
    },
    "onDeny": {
      "copyToClient": ["www-authenticate"]
    }
  }
}
```

#### Top-level fields

| Field              | Type   | Description                                          |
| ------------------ | ------ | ---------------------------------------------------- |
| `destinationId`    | string | **Required**. Destination hosting the authz service  |
| `mode`             | string | `http` or `grpc`. Default: `http`                    |
| `path`             | string | Authorization endpoint path (HTTP mode)              |
| `timeout`          | string | Request deadline. Default: `5s`                      |
| `failureModeAllow` | bool   | Allow requests when the authz service is unreachable |
| `includeBody`      | bool   | Forward the request body to the authz service        |
| `onCheck`          | object | What to send in the check request                    |
| `onAllow`          | object | What to do when authz allows                         |
| `onDeny`           | object | What to do when authz denies                         |

#### onCheck

| Field            | Type     | Description                                    |
| ---------------- | -------- | ---------------------------------------------- |
| `forwardHeaders` | string[] | Client headers to include in the check request |
| `injectHeaders`  | array    | Extra headers to add to the check request      |

`injectHeaders` values support **request variable interpolation**. You can reference request data in the header value:

| Placeholder              | Value                        |
| ------------------------ | ---------------------------- |
| `${request.host}`        | Host header without port     |
| `${request.path}`        | URL path                     |
| `${request.method}`      | HTTP method                  |
| `${request.scheme}`      | `http` or `https`            |
| `${request.authority}`   | Full Host header (with port) |
| `${request.header.NAME}` | Value of any request header  |

Example:

```json
{
  "onCheck": {
    "forwardHeaders": ["Authorization"],
    "injectHeaders": [
      { "key": "X-Original-Path", "value": "${request.path}" },
      { "key": "X-Original-Host", "value": "${request.host}" },
      { "key": "X-Forwarded-Method", "value": "${request.method}" },
      { "key": "X-Trace-ID", "value": "${request.header.X-Trace-ID}" }
    ]
  }
}
```

#### onAllow

| Field            | Type     | Description                                                    |
| ---------------- | -------- | -------------------------------------------------------------- |
| `copyToUpstream` | string[] | Headers from the authz response to add to the upstream request |
| `copyToClient`   | string[] | Headers from the authz response to add to the client response  |

#### onDeny

| Field          | Type     | Description                                                  |
| -------------- | -------- | ------------------------------------------------------------ |
| `copyToClient` | string[] | Headers from the authz deny response to return to the client |

### External Processor

Sends request/response data to an external HTTP or gRPC service for inspection or mutation. The processor can modify headers, body, or reject the request.

```json
{
  "name": "fraud-detector",
  "type": "extProc",
  "extProc": {
    "destinationId": "processor-dest",
    "mode": "http",
    "timeout": "10s",
    "phases": {
      "requestHeaders": "send",
      "responseHeaders": "send",
      "requestBody": "buffered",
      "responseBody": "none",
      "maxBodyBytes": 1048576
    },
    "allowOnError": true,
    "statusOnError": 500,
    "disableReject": false,
    "observeMode": {
      "enabled": false
    },
    "allowedMutations": {
      "allowHeaders": ["X-Fraud-Score", "X-Request-ID"],
      "denyHeaders": ["Authorization"]
    },
    "forwardRules": {
      "allowHeaders": ["Content-Type", "X-Request-ID"],
      "denyHeaders": ["Cookie"]
    },
    "metricsPrefix": "fraud_detector"
  }
}
```

#### Top-level fields

| Field              | Type   | Description                                            |
| ------------------ | ------ | ------------------------------------------------------ |
| `destinationId`    | string | **Required**. Destination hosting the processor        |
| `mode`             | string | `http` or `grpc`. Default: `http`                      |
| `timeout`          | string | Processing deadline. Default: `10s`                    |
| `phases`           | object | Which phases to send to the processor                  |
| `allowOnError`     | bool   | Continue if the processor fails                        |
| `statusOnError`    | number | HTTP status to return on processor error. Default: 500 |
| `disableReject`    | bool   | Prevent the processor from rejecting requests          |
| `observeMode`      | object | Read-only mode: mutations are logged but not applied   |
| `allowedMutations` | object | Restrict which headers the processor can modify        |
| `forwardRules`     | object | Restrict which headers are sent to the processor       |
| `metricsPrefix`    | string | Prefix for processor metrics                           |

#### phases

| Field             | Type   | Values                                            | Description                             |
| ----------------- | ------ | ------------------------------------------------- | --------------------------------------- |
| `requestHeaders`  | string | `send`, `skip`                                    | Send request headers to processor       |
| `responseHeaders` | string | `send`, `skip`                                    | Send response headers to processor      |
| `requestBody`     | string | `none`, `buffered`, `bufferedPartial`, `streamed` | Request body processing mode            |
| `responseBody`    | string | `none`, `buffered`, `bufferedPartial`, `streamed` | Response body processing mode           |
| `maxBodyBytes`    | number | —                                                 | Max body bytes to buffer before sending |

Body modes:

| Mode              | Description                                 |
| ----------------- | ------------------------------------------- |
| `none`            | Don't send body to processor                |
| `buffered`        | Buffer entire body, send at once            |
| `bufferedPartial` | Buffer up to `maxBodyBytes`, send what fits |
| `streamed`        | Stream body chunks as they arrive           |

#### allowedMutations / forwardRules

| Field          | Type     | Description                                   |
| -------------- | -------- | --------------------------------------------- |
| `allowHeaders` | string[] | Only these headers can be mutated / forwarded |
| `denyHeaders`  | string[] | These headers cannot be mutated / forwarded   |

#### observeMode

| Field       | Type   | Description                                             |
| ----------- | ------ | ------------------------------------------------------- |
| `enabled`   | bool   | When true, processor runs but mutations are not applied |
| `workers`   | number | Number of concurrent observer workers                   |
| `queueSize` | number | Buffer size for observe queue                           |

### Access Log

Logs requests to a file:

```json
{
  "name": "access-log",
  "type": "accessLog",
  "accessLog": {
    "path": "/var/log/vrata/access.log"
  }
}
```

---

## Middleware Overrides

Per-route or per-group overrides let you control each middleware's behavior on specific routes.

### disabled

Completely disable a middleware for a route:

```json
{
  "middlewareOverrides": {
    "jwt-auth-id": { "disabled": true }
  }
}
```

### skipWhen

Skip the middleware when a CEL condition matches the request. The middleware is **active by default**:

```json
{
  "middlewareOverrides": {
    "ext-authz-id": {
      "skipWhen": ["'x-authz-skip' in request.headers"]
    }
  }
}
```

Multiple expressions: if **any** evaluates to `true`, the middleware is skipped.

### onlyWhen

Only run the middleware when a CEL condition matches. The middleware is **inactive by default**:

```json
{
  "middlewareOverrides": {
    "fraud-check-id": {
      "onlyWhen": [
        "request.method == 'POST' && request.path.startsWith('/api/payments')"
      ]
    }
  }
}
```

Multiple expressions: the middleware runs if **at least one** evaluates to `true`.

`skipWhen` and `onlyWhen` are mutually exclusive on the same override.

### Real-world example: sandbox authz bypass

Two middlewares in the chain. In normal flow, ExtAuthz runs and JWT is skipped. When the `X-Authz-Skip` header is present, ExtAuthz is skipped and JWT takes over:

```json
{
  "middlewareIds": ["ext-authz", "jwt-auth"],
  "middlewareOverrides": {
    "ext-authz-id": {
      "skipWhen": ["'x-authz-skip' in request.headers"]
    },
    "jwt-auth-id": {
      "onlyWhen": ["'x-authz-skip' in request.headers"]
    }
  }
}
```

### CEL variables available

The `request` object available in `skipWhen`/`onlyWhen` expressions:

| Field                 | Type   | Example                            |
| --------------------- | ------ | ---------------------------------- |
| `request.method`      | string | `"GET"`, `"POST"`                  |
| `request.path`        | string | `"/api/v1/users"`                  |
| `request.host`        | string | `"api.example.com"`                |
| `request.scheme`      | string | `"https"`                          |
| `request.headers`     | map    | `request.headers["authorization"]` |
| `request.queryParams` | map    | `request.queryParams["token"]`     |
| `request.clientIp`    | string | `"10.0.0.1"`                       |

### Override fields reference

| Field         | Type     | Description                               |
| ------------- | -------- | ----------------------------------------- |
| `disabled`    | bool     | Completely disable the middleware         |
| `skipWhen`    | string[] | CEL expressions — skip if any matches     |
| `onlyWhen`    | string[] | CEL expressions — run only if one matches |
| `jwtProvider` | string   | Select specific JWT provider by name      |
| `headers`     | object   | Override header manipulation config       |
| `extProc`     | object   | Override ExtProc settings                 |

---

## Metrics

Prometheus metrics are configured per-listener. When the `metrics` field is present on a listener, Vrata collects request/response metrics and serves a Prometheus scrape endpoint on that listener.

```json
{
  "name": "production",
  "port": 8443,
  "tls": { "certPath": "/certs/tls.crt", "keyPath": "/certs/tls.key" },
  "metrics": {
    "path": "/metrics",
    "collect": {
      "route": true,
      "destination": true,
      "endpoint": false,
      "middleware": true,
      "listener": true
    },
    "histograms": {
      "durationBuckets": [
        0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
      ],
      "sizeBuckets": [100, 1000, 10000, 100000, 1000000]
    }
  }
}
```

Minimal (all defaults):

```json
{
  "name": "main",
  "port": 3000,
  "metrics": {}
}
```

This enables all default dimensions (route, destination, middleware, listener) with endpoint disabled, serves at `/metrics`, and uses default histogram buckets.

### Configuration

| Field                | Type   | Default      | Description                                     |
| -------------------- | ------ | ------------ | ----------------------------------------------- |
| `metrics`            | object | `null`       | Presence activates metrics. `null` = no metrics |
| `metrics.path`       | string | `"/metrics"` | Scrape endpoint path                            |
| `metrics.collect`    | object | see below    | Which dimensions are collected                  |
| `metrics.histograms` | object | see below    | Histogram bucket tuning                         |

### Collect dimensions

| Field                 | Type | Default | Description                                                                                     |
| --------------------- | ---- | ------- | ----------------------------------------------------------------------------------------------- |
| `collect.route`       | bool | `true`  | Per-route request counters, latency, size, retries, mirrors, inflight                           |
| `collect.destination` | bool | `true`  | Per-destination request counters, latency, inflight, circuit breaker state                      |
| `collect.endpoint`    | bool | `false` | Per-endpoint (pod) counters, latency, health, consecutive errors. **High cardinality — opt-in** |
| `collect.middleware`  | bool | `true`  | Per-middleware duration, pass/reject counters                                                   |
| `collect.listener`    | bool | `true`  | Per-listener connection counters, TLS errors                                                    |

### Histogram tuning

| Field                        | Type    | Default                                                     | Description                                     |
| ---------------------------- | ------- | ----------------------------------------------------------- | ----------------------------------------------- |
| `histograms.durationBuckets` | float[] | `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]` | Duration histogram bucket boundaries (seconds)  |
| `histograms.sizeBuckets`     | float[] | `[100, 1000, 10000, 100000, 1000000]`                       | Request/response size bucket boundaries (bytes) |

### Exposed metrics

**Route** (`collect.route: true`):

| Metric                             | Type      | Labels                                                    |
| ---------------------------------- | --------- | --------------------------------------------------------- |
| `vrata_route_requests_total`       | counter   | `route`, `group`, `method`, `status_code`, `status_class` |
| `vrata_route_duration_seconds`     | histogram | `route`, `group`, `method`                                |
| `vrata_route_request_bytes_total`  | counter   | `route`, `group`                                          |
| `vrata_route_response_bytes_total` | counter   | `route`, `group`                                          |
| `vrata_route_inflight_requests`    | gauge     | `route`, `group`                                          |
| `vrata_route_retries_total`        | counter   | `route`, `group`, `attempt`                               |
| `vrata_mirror_requests_total`      | counter   | `route`, `destination`                                    |

**Destination** (`collect.destination: true`):

| Metric                                    | Type      | Labels                                       |
| ----------------------------------------- | --------- | -------------------------------------------- |
| `vrata_destination_requests_total`        | counter   | `destination`, `status_code`, `status_class` |
| `vrata_destination_duration_seconds`      | histogram | `destination`                                |
| `vrata_destination_inflight_requests`     | gauge     | `destination`                                |
| `vrata_destination_circuit_breaker_state` | gauge     | `destination`                                |

**Endpoint** (`collect.endpoint: true`):

| Metric                            | Type      | Labels                                                   |
| --------------------------------- | --------- | -------------------------------------------------------- |
| `vrata_endpoint_requests_total`   | counter   | `destination`, `endpoint`, `status_code`, `status_class` |
| `vrata_endpoint_duration_seconds` | histogram | `destination`, `endpoint`                                |
| `vrata_endpoint_healthy`          | gauge     | `destination`, `endpoint`                                |
| `vrata_endpoint_consecutive_5xx`  | gauge     | `destination`, `endpoint`                                |

**Middleware** (`collect.middleware: true`):

| Metric                              | Type      | Labels                              |
| ----------------------------------- | --------- | ----------------------------------- |
| `vrata_middleware_duration_seconds` | histogram | `middleware`, `type`                |
| `vrata_middleware_rejections_total` | counter   | `middleware`, `type`, `status_code` |
| `vrata_middleware_passed_total`     | counter   | `middleware`, `type`                |

**Listener** (`collect.listener: true`):

| Metric                                      | Type    | Labels                |
| ------------------------------------------- | ------- | --------------------- |
| `vrata_listener_connections_total`          | counter | `listener`, `address` |
| `vrata_listener_active_connections`         | gauge   | `listener`, `address` |
| `vrata_listener_tls_handshake_errors_total` | counter | `listener`, `address` |

### Architecture

Metrics are not a middleware. They are infrastructure-level instrumentation:

- Each listener with `metrics` gets its own isolated `prometheus.Registry`
- Route metrics are recorded in `Router.ServeHTTP` via `httpsnoop`
- Destination/endpoint metrics are recorded in `recordEndpointResult`
- Middleware timing is recorded by an automatic wrapper around each middleware
- Retry and mirror counters are recorded inline in `forwardHandler`
- Health, circuit breaker, and consecutive error gauges are scraped every 5 seconds by a background goroutine
- The scrape endpoint is served on the listener itself at the configured path

All metrics are collected for all routes — there is no per-entity disable. Control cardinality with the `collect` toggles, especially `endpoint` (disabled by default).
