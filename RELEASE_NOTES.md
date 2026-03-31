# v0.2.0

## New features

### Inline authorization middleware

New `inlineAuthz` middleware type. Write authorization rules as CEL expressions that run locally in the proxy — no external service needed. First matching rule wins.

### CEL body access

CEL expressions can now read the request body via `request.body.raw` (any content type) and `request.body.json` (parsed JSON). Available everywhere CEL is used: route matching, skipWhen/onlyWhen, and inlineAuthz rules. Body is only read when a rule actually needs it.

### mTLS on listeners

Listeners support client certificate authentication (`none`, `optional`, `require`). Client cert metadata (URI SANs, DNS SANs, subject, serial) is exposed in CEL. Automatic `X-Forwarded-Client-Cert` header injection with spoof protection.

## Improvements

- Response header middleware works correctly when upstream skips `WriteHeader()`
- Per-attempt retry timeouts no longer leak contexts
- Outlier detection tracks errors per endpoint instead of per destination
- Destination weights validated on route creation (must sum to 100)
- Error handling on `w.Write()` across all middlewares
- Controller health/metrics servers hardened with timeouts

## Helm chart fixes

- Raft `nodeId` fixed to `${POD_IP}:7000` — cluster elections now work in kind
- Corrected headless service DNS and image tagging in E2E setup

## Breaking changes

- `maxRequestsPerConnection` renamed to `maxConnsPerHost` on destination options
- Upstream timeouts return 504 instead of 503

## Docs

- New pages: [Inline Authorization](/docs/concepts/middlewares/inline-authz/), [Client Certificates (mTLS)](/docs/concepts/listeners/mtls/)
- Route matching docs updated with `request.body` and `request.tls` variables
