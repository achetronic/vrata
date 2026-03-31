---
title: "Redirect"
weight: 6
---

Return an HTTP redirect to the client without contacting any upstream. Vrata constructs the `Location` header from the route configuration and sends the redirect response directly.

## Configuration

```json
{
  "name": "http-to-https",
  "match": {"pathPrefix": "/"},
  "redirect": {
    "scheme": "https",
    "code": 301
  }
}
```

## All fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | — | Complete target URL. When set, all other fields are ignored |
| `scheme` | string | — | Override only the scheme (`"http"` → `"https"`) |
| `host` | string | — | Override only the hostname |
| `path` | string | — | Replace the path component |
| `stripQuery` | bool | `false` | Remove the query string from the redirect target |
| `code` | number | `301` | HTTP status code. Accepted values: `301`, `302`, `303`, `307`, `308` |

`redirect` is mutually exclusive with `forward` and `directResponse`. Setting more than one is a validation error.

When `url` is not set, Vrata builds the target URL by taking the original request URL and applying `scheme`, `host`, and `path` overrides in that order. Fields that are not set keep their original value from the request.

## Status codes

| Code | Name | Use |
|------|------|-----|
| `301` | Moved Permanently | Resource has permanently moved. Browsers and crawlers cache this. Default. |
| `302` | Found | Temporary redirect. Not cached. |
| `303` | See Other | Redirect to a different resource after a POST. Browser changes method to GET. |
| `307` | Temporary Redirect | Temporary, method preserved. Browser repeats the original method (POST stays POST). |
| `308` | Permanent Redirect | Permanent, method preserved. Like 301 but method is not changed to GET. |

## Examples

### HTTP → HTTPS upgrade

```json
{
  "name": "force-https",
  "match": {"pathPrefix": "/"},
  "redirect": {
    "scheme": "https",
    "code": 301
  }
}
```

Permanently redirects all plain HTTP traffic to HTTPS. The path and query string are preserved.

| Client request | Client receives |
|---|---|
| `GET http://example.com/api/users?page=2` | `301 Location: https://example.com/api/users?page=2` |

### Redirect a removed path

```json
{
  "name": "old-api",
  "match": {"pathPrefix": "/api/v1"},
  "redirect": {
    "path": "/api/v2",
    "code": 301
  }
}
```

Permanently redirects the old API prefix to the new one.

| Client request | Client receives |
|---|---|
| `GET /api/v1/users` | `301 Location: /api/v2/users` |
| `GET /api/v1/orders/42` | `301 Location: /api/v2/orders/42` |

### Redirect to a new domain

```json
{
  "name": "domain-migration",
  "match": {"pathPrefix": "/"},
  "redirect": {
    "host": "new.example.com",
    "code": 301
  }
}
```

Redirects all traffic to a new domain preserving the original path and query string.

### Redirect to a fixed URL

```json
{
  "name": "docs-shortcut",
  "match": {"path": "/docs"},
  "redirect": {
    "url": "https://docs.example.com/getting-started",
    "code": 302
  }
}
```

When `url` is set, it is used verbatim as the `Location` header. All other fields (`scheme`, `host`, `path`, `stripQuery`) are ignored.

### Strip query string on redirect

```json
{
  "name": "clean-redirect",
  "match": {"pathPrefix": "/search"},
  "redirect": {
    "path": "/find",
    "stripQuery": true,
    "code": 302
  }
}
```

| Client request | Client receives |
|---|---|
| `GET /search?q=vrata&page=3` | `302 Location: /find` |

### Combine scheme + host + path

```json
{
  "name": "full-migration",
  "match": {"pathPrefix": "/app"},
  "redirect": {
    "scheme": "https",
    "host": "app.example.com",
    "path": "/",
    "code": 308
  }
}
```

All three overrides apply together. The client is sent to `https://app.example.com/` regardless of the original path. Method is preserved (308).

### Temporary maintenance redirect

```json
{
  "name": "maintenance-redirect",
  "match": {"pathPrefix": "/"},
  "redirect": {
    "url": "https://status.example.com",
    "code": 302
  }
}
```

Temporarily sends all traffic to a status page. Use 302 so browsers don't cache it — when maintenance is over, swap back via a snapshot and clients will follow the real routes again.
