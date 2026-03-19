---
title: "JWT Authentication"
weight: 1
---

Validate JSON Web Tokens on incoming requests. Supports RSA, ECDSA, and Ed25519 signatures with remote or inline JWKS. Extract claims into headers for upstream consumption.

## Configuration

```json
{
  "name": "auth",
  "type": "jwt",
  "jwt": {
    "issuer": "https://auth.example.com",
    "audiences": ["api"],
    "jwksPath": "/.well-known/jwks.json",
    "jwksDestinationId": "<auth-server-destination>",
    "jwksRetrievalTimeout": "10s",
    "forwardJwt": true,
    "claimToHeaders": [
      {"expr": "claims.sub", "header": "X-User-ID"},
      {"expr": "claims.roles[0]", "header": "X-User-Role"},
      {"expr": "claims.orgs.map(o, o.name).join(',')", "header": "X-User-Orgs"}
    ],
    "assertClaims": [
      "claims.org == 'acme'",
      "'admin' in claims.roles"
    ]
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `issuer` | string | required | Expected `iss` claim value |
| `audiences` | string[] | — | Expected `aud` values (empty = skip audience check) |
| `jwksPath` | string | — | HTTP path on the destination to fetch JWKS |
| `jwksDestinationId` | string | — | Destination hosting the JWKS endpoint |
| `jwksInline` | string | — | Literal JWKS JSON string (alternative to remote) |
| `jwksRetrievalTimeout` | string | `10s` | Max time to download JWKS from remote |
| `forwardJwt` | bool | `false` | Forward the `Authorization` header to upstream |
| `claimToHeaders` | array | — | Extract claims via CEL and inject as request headers |
| `assertClaims` | string[] | — | CEL expressions that must all return `true` |

## Signature algorithms

| Algorithm family | Algorithms | Key type |
|-----------------|-----------|----------|
| RSA | RS256, RS384, RS512 | RSA public key |
| ECDSA | ES256, ES384, ES512 | EC public key (P-256, P-384, P-521) |
| EdDSA | EdDSA | Ed25519 public key |

The algorithm is determined by the JWKS key, not by configuration.

## Examples

### Basic JWT validation (Auth0, Okta, Keycloak)

```json
{
  "name": "auth",
  "type": "jwt",
  "jwt": {
    "issuer": "https://myapp.auth0.com/",
    "audiences": ["https://api.myapp.com"],
    "jwksPath": "/.well-known/jwks.json",
    "jwksDestinationId": "<auth0-destination>"
  }
}
```

The `auth0-destination` must be a Vrata destination pointing to your Auth0 domain. Vrata fetches JWKS from `https://myapp.auth0.com/.well-known/jwks.json` via that destination.

### Forward JWT + extract user ID

```json
{
  "name": "auth-with-user",
  "type": "jwt",
  "jwt": {
    "issuer": "https://auth.example.com",
    "audiences": ["api"],
    "jwksPath": "/.well-known/jwks.json",
    "jwksDestinationId": "<auth-dest>",
    "forwardJwt": true,
    "claimToHeaders": [
      {"expr": "claims.sub", "header": "X-User-ID"},
      {"expr": "claims.email", "header": "X-User-Email"}
    ]
  }
}
```

The upstream receives:
- `Authorization: Bearer <token>` (forwarded)
- `X-User-ID: user-123` (extracted from `sub` claim)
- `X-User-Email: user@example.com` (extracted from `email` claim)

### Claim assertions (RBAC)

```json
{
  "name": "admin-auth",
  "type": "jwt",
  "jwt": {
    "issuer": "https://auth.example.com",
    "jwksPath": "/.well-known/jwks.json",
    "jwksDestinationId": "<auth-dest>",
    "assertClaims": [
      "claims.org == 'acme'",
      "'admin' in claims.roles",
      "claims.exp > now"
    ]
  }
}
```

All assertions must evaluate to `true`. If any fails → 403 Forbidden.

### Complex claim extraction (CEL)

```json
{
  "claimToHeaders": [
    {"expr": "claims.sub", "header": "X-User-ID"},
    {"expr": "claims.roles[0]", "header": "X-User-Role"},
    {"expr": "claims.roles.join(',')", "header": "X-User-Roles"},
    {"expr": "claims.orgs.map(o, o.name).join(',')", "header": "X-User-Orgs"},
    {"expr": "claims.metadata.tier", "header": "X-User-Tier"},
    {"expr": "string(claims.iat)", "header": "X-Token-Issued-At"}
  ]
}
```

CEL expressions can navigate nested claims, index arrays, call string methods, and transform values.

### Inline JWKS (development / testing)

```json
{
  "name": "dev-auth",
  "type": "jwt",
  "jwt": {
    "issuer": "dev",
    "jwksInline": "{\"keys\":[{\"kty\":\"RSA\",\"kid\":\"dev-key\",\"n\":\"...\",\"e\":\"AQAB\"}]}"
  }
}
```

No external JWKS endpoint needed. Useful for tests or when the auth server is on the same network.

### Skip JWT on health endpoints

Attach the middleware to a group and skip it on specific paths:

```json
{
  "middlewareOverrides": {
    "auth": {
      "skipWhen": ["request.path == '/health'", "request.path == '/ready'"]
    }
  }
}
```

## Error responses

| Status | When |
|--------|------|
| `401 Unauthorized` | Missing token, invalid format, bad signature, expired, unknown key ID |
| `403 Forbidden` | Token is valid but a claim assertion failed |

All errors return JSON:
```json
{"error": "jwt: token expired"}
```
