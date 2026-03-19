---
title: "CORS"
weight: 2
---

Configure Cross-Origin Resource Sharing for browser-based clients. Handles preflight OPTIONS requests automatically.

## Configuration

```json
{
  "name": "cors",
  "type": "cors",
  "cors": {
    "allowOrigins": [
      {"value": "https://app.example.com"},
      {"value": "https://.*.staging.example.com", "regex": true}
    ],
    "allowMethods": ["GET", "POST", "PUT", "DELETE"],
    "allowHeaders": ["Authorization", "Content-Type"],
    "exposeHeaders": ["X-Request-ID"],
    "maxAge": 3600,
    "allowCredentials": true
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `allowOrigins` | array | — | Origins to allow (exact string or regex) |
| `allowOrigins[].value` | string | required | Origin value or regex pattern |
| `allowOrigins[].regex` | bool | `false` | Whether `value` is a regex |
| `allowMethods` | string[] | — | HTTP methods allowed in CORS requests |
| `allowHeaders` | string[] | — | Request headers the browser can send |
| `exposeHeaders` | string[] | — | Response headers the browser can access |
| `maxAge` | number | `0` | Preflight cache duration in seconds |
| `allowCredentials` | bool | `false` | Allow cookies and auth headers |

## Examples

### Single origin

```json
{
  "name": "cors",
  "type": "cors",
  "cors": {
    "allowOrigins": [{"value": "https://app.example.com"}],
    "allowMethods": ["GET", "POST"],
    "allowHeaders": ["Content-Type"]
  }
}
```

Only `https://app.example.com` is allowed. Other origins get no CORS headers.

### Multiple exact origins

```json
{
  "cors": {
    "allowOrigins": [
      {"value": "https://app.example.com"},
      {"value": "https://admin.example.com"},
      {"value": "http://localhost:3000"}
    ]
  }
}
```

### Regex origins (staging, preview deploys)

```json
{
  "cors": {
    "allowOrigins": [
      {"value": "https://app.example.com"},
      {"value": "https://.*\\.staging\\.example\\.com", "regex": true},
      {"value": "https://deploy-preview-[0-9]+\\.netlify\\.app", "regex": true}
    ]
  }
}
```

Matches `https://app.staging.example.com`, `https://api.staging.example.com`, `https://deploy-preview-42.netlify.app`, etc.

### Wildcard (allow any origin)

```json
{
  "cors": {
    "allowOrigins": [{"value": "*"}],
    "allowMethods": ["GET"],
    "allowHeaders": ["Content-Type"]
  }
}
```

Allows any origin. Be careful: browsers reject `*` when `allowCredentials: true`.

### Full API CORS (with credentials)

```json
{
  "name": "api-cors",
  "type": "cors",
  "cors": {
    "allowOrigins": [
      {"value": "https://app.example.com"},
      {"value": "https://admin.example.com"}
    ],
    "allowMethods": ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
    "allowHeaders": ["Authorization", "Content-Type", "X-Request-ID", "X-Tenant"],
    "exposeHeaders": ["X-Request-ID", "X-RateLimit-Remaining"],
    "maxAge": 86400,
    "allowCredentials": true
  }
}
```

- `allowCredentials: true` — lets the browser send cookies and `Authorization` headers
- `maxAge: 86400` — browser caches preflight for 24h (reduces OPTIONS requests)
- `exposeHeaders` — lets JavaScript access `X-Request-ID` and `X-RateLimit-Remaining` from the response

### Public read-only API (no credentials)

```json
{
  "name": "public-cors",
  "type": "cors",
  "cors": {
    "allowOrigins": [{"value": "*"}],
    "allowMethods": ["GET", "HEAD"],
    "allowHeaders": ["Content-Type", "Accept"],
    "maxAge": 3600
  }
}
```

## Preflight handling

Browsers send an `OPTIONS` preflight request before making cross-origin requests with custom headers or methods. Vrata detects preflight requests (OPTIONS with `Origin` and `Access-Control-Request-Method` headers) and returns `204 No Content` with the appropriate CORS headers. The request never reaches your upstream.

## Response headers

Vrata adds these response headers based on your configuration:

| Header | Set when |
|--------|----------|
| `Access-Control-Allow-Origin` | Always (matching origin or `*`) |
| `Access-Control-Allow-Methods` | Preflight response |
| `Access-Control-Allow-Headers` | Preflight response |
| `Access-Control-Expose-Headers` | Normal response, if `exposeHeaders` is set |
| `Access-Control-Max-Age` | Preflight response, if `maxAge > 0` |
| `Access-Control-Allow-Credentials` | If `allowCredentials: true` |
| `Vary` | `Origin` (always added) |
