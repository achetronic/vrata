# WIP — Secrets as First-Class Citizens

Working document for the secrets feature. Will be consolidated into
`SERVER_DECISIONS.md`, `SERVER_TODO.md`, `CONTROLLER_TODO.md`, etc. once
the design is final.

**Prerequisite**: Control plane security (TLS + mTLS + API keys) —
**DONE**. Implemented and tested. See `SERVER_DECISIONS.md`.

---

## Problem

Vrata entities reference sensitive material (TLS certs, private keys, CA
bundles) via file paths. This works when the files are mounted locally,
but breaks the control-plane → proxy distribution model:

- The control plane knows the secret values but has no way to send them
  to proxies.
- Proxies may or may not have the files locally — depends on deployment
  topology.
- There is no universal mechanism to reference a secret from an entity.

The existing `os.ExpandEnv` in config loading is local-only and limited
to the config file itself — it does not extend to API-created entities
(Listeners, Destinations, Middlewares).

---

## Design Principles

1. **Secret is a first-class entity** — flat, simple, no nesting. Stored
   in bbolt, CRUD via `/api/v1/secrets`, referenced by ID.
2. **The reference pattern is the mechanism** — `{{secret:<source>:<ref>}}`
   embedded in any string field of any entity. The source determines
   how the value is obtained.
3. **All resolution happens on the control plane** — the proxy is
   stateless and dumb. It receives a fully-resolved snapshot with
   literal values. It never sees `{{secret:...}}` patterns, never reads
   env vars for secrets, never opens secret files. One resolution site,
   one place to debug, one place to fail.
4. **Model fields change from paths to values** — fields like `certPath`,
   `keyFile`, `caFile` become `cert`, `key`, `ca` and hold the actual
   content (or a `{{secret:...}}` reference that the CP resolves before
   the proxy ever sees it).

---

## Secret Entity

```go
type Secret struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Value string `json:"value"`
}
```

- Flat. One secret = one value. No `map[string]string`, no `data` bag.
- If you need cert + key + CA, that is three separate Secrets.
- Stored in bbolt bucket `secrets`. CRUD via REST API.
- Secrets do NOT travel in the snapshot as a separate array — they are
  resolved into entity fields before the snapshot is serialized and
  sent to proxies.

---

## Reference Pattern

```
{{secret:<source>:<ref>}}
```

### Sources

| Source  | `<ref>`           | Resolved by    | What travels in the snapshot |
| ------- | ----------------- | -------------- | ---------------------------- |
| `value` | Secret UUID/ID    | Control plane  | The literal secret value     |
| `env`   | Env variable name | Control plane  | The literal env var value    |
| `file`  | File path         | Control plane  | The literal file contents    |

All three sources are resolved by the control plane at snapshot build
time. The proxy always receives final, literal values.

### Resolution timing

All resolution happens in `buildSnapshot()` on the control plane,
before JSON serialization and SSE transmission:

- **`value`**: looks up the Secret by ID in the store, substitutes its
  `Value`. If the Secret does not exist → snapshot build fails with a
  clear error.
- **`env`**: calls `os.Getenv(ref)` on the control plane process. If
  the variable is not set → snapshot build fails with a clear error.
- **`file`**: calls `os.ReadFile(ref)` on the control plane filesystem.
  If the file does not exist → snapshot build fails with a clear error.

The proxy never sees any `{{secret:...}}` pattern. It receives a
snapshot where every field already contains its final value.

### Why the proxy does not resolve anything

- **Stateless proxy**: the proxy's only input is the snapshot JSON. It
  does not need env vars with secret material, cert file mounts, or any
  local state beyond the binary itself. This simplifies deployment and
  reduces attack surface.
- **Single point of failure**: if a secret reference is broken, it fails
  loudly at snapshot build time on the control plane — not silently on
  N proxy instances that might each have different env vars or file
  mounts.
- **Debugging**: one place to check logs, one place to fix.
- **Security**: secrets never need to be distributed to proxy pods via
  k8s Secrets, env vars, or volume mounts. The CP pushes resolved
  values over the SSE channel (which should be TLS-protected).

### Examples

```json
// TLS cert from a Vrata Secret entity:
"cert": "{{secret:value:sec-tls-prod-cert}}"

// TLS key from an env var on the control plane:
"key": "{{secret:env:TLS_PRIVATE_KEY}}"

// CA bundle from a file on the control plane filesystem:
"ca": "{{secret:file:/etc/ssl/custom-ca.pem}}"

// Direct PEM inline (no secret reference, always works):
"cert": "-----BEGIN CERTIFICATE-----\nMIID..."
```

---

## Affected Entities

Audit of all fields across all model entities that hold sensitive data
or are susceptible to carrying secrets.

### Confirmed — file paths that become value fields

| Entity          | Struct               | Field      | Current JSON tag | Holds today                          |
| --------------- | -------------------- | ---------- | ---------------- | ------------------------------------ |
| **Listener**    | `ListenerTLS`        | `CertPath` | `certPath`       | Path → TLS server certificate PEM    |
| **Listener**    | `ListenerTLS`        | `KeyPath`  | `keyPath`        | Path → TLS server private key PEM    |
| **Listener**    | `ListenerClientAuth` | `CAFile`   | `caFile`         | Path → CA bundle for mTLS clients    |
| **Destination** | `TLSOptions`         | `CertFile` | `certFile`       | Path → upstream client cert PEM      |
| **Destination** | `TLSOptions`         | `KeyFile`  | `keyFile`        | Path → upstream client key PEM       |
| **Destination** | `TLSOptions`         | `CAFile`   | `caFile`         | Path → CA bundle for upstream verify |

### Confirmed — inline sensitive data

| Entity         | Struct      | Field        | Current JSON tag | Holds today             |
| -------------- | ----------- | ------------ | ---------------- | ----------------------- |
| **Middleware**  | `JWTConfig` | `JWKsInline` | `jwksInline`     | Literal JWKS JSON (key material) |

### Plausible future — could carry secrets

| Entity         | Struct       | Field   | JSON tag | Rationale                                          |
| -------------- | ------------ | ------- | -------- | -------------------------------------------------- |
| **Middleware**  | `HeaderValue`| `Value` | `value`  | Users inject `Authorization: Bearer <token>` etc.  |

### Not affected

| Entity         | Reason                                    |
| -------------- | ----------------------------------------- |
| **Route**      | Only matching rules and actions           |
| **RouteGroup** | Only route composition and matchers       |
| **Config**     | Redis password already handled by `os.ExpandEnv` on config YAML — outside this feature's scope |

---

## Model Changes

Fields that today hold file paths become value fields. The field holds
either a literal value, or a `{{secret:...}}` reference that the CP
resolves before the snapshot reaches the proxy.

### ListenerTLS

| Before                   | After                  | JSON tag   |
| ------------------------ | ---------------------- | ---------- |
| `CertPath string`        | `Cert string`          | `"cert"`   |
| `KeyPath string`         | `Key string`           | `"key"`    |

### ListenerClientAuth

| Before                   | After                  | JSON tag   |
| ------------------------ | ---------------------- | ---------- |
| `CAFile string`          | `CA string`            | `"ca"`     |

### TLSOptions (Destination)

| Before                   | After                  | JSON tag   |
| ------------------------ | ---------------------- | ---------- |
| `CertFile string`        | `Cert string`          | `"cert"`   |
| `KeyFile string`         | `Key string`           | `"key"`    |
| `CAFile string`          | `CA string`            | `"ca"`     |

### JWTConfig (Middleware)

No rename needed — `JWKsInline` already holds inline content. It can
carry a `{{secret:value:...}}` reference as-is.

### All renamed fields accept

- A literal value (PEM string, JWKS document, etc.)
- A `{{secret:value:<id>}}` reference → CP resolves from Secret entity
- A `{{secret:env:<var>}}` reference → CP resolves from its own env
- A `{{secret:file:<path>}}` reference → CP resolves from its own filesystem
- Empty string (no TLS / use system defaults)

---

## Resolution Implementation

### In `buildSnapshot()`

After assembling the Snapshot from store entities:

1. Serialize the Snapshot to JSON (as today).
2. Scan for all `{{secret:<source>:<ref>}}` patterns via regex.
3. For each match, resolve based on source:
   - `value` → `store.GetSecret(ctx, ref)` → use `secret.Value`
   - `env` → `os.Getenv(ref)`
   - `file` → `os.ReadFile(ref)`
4. Replace each pattern with the resolved literal value.
5. If **any** reference fails to resolve (missing Secret, unset env var,
   missing file) → **fail the entire snapshot creation** with a clear
   error message listing all unresolved references. Do not produce a
   partially-resolved snapshot.
6. The resolved JSON is what gets stored as the VersionedSnapshot and
   what gets pushed via SSE.

This is a single `regexp.ReplaceAllStringFunc` over the serialized JSON.
No changes to any model struct, handler, or store method needed for the
resolution itself.

### Proxy side

No changes to resolution logic. The proxy receives a snapshot where all
fields already contain literal values. The only proxy changes are in TLS
wiring — switching from file-reading functions to in-memory functions:

- `tls.LoadX509KeyPair(file, file)` → `tls.X509KeyPair([]byte, []byte)`
- `os.ReadFile(caFile)` → use the field value directly as `[]byte`

---

## Store & API

- New bbolt bucket: `secrets`
- New Store interface methods: `ListSecrets`, `GetSecret`, `SaveSecret`,
  `DeleteSecret`
- New REST endpoints:
  - `GET    /api/v1/secrets`         — list (returns ID + Name only, no Value)
  - `GET    /api/v1/secrets/{id}`    — get (returns Value — requires future API auth)
  - `POST   /api/v1/secrets`         — create
  - `PUT    /api/v1/secrets/{id}`    — update
  - `DELETE /api/v1/secrets/{id}`    — delete
- New `ResourceType`: `"secret"` for store events
- Secrets are **not** included in the Snapshot struct — they are resolved
  into entity fields before the snapshot is stored and transmitted

---

## Security Considerations

- **Logging**: Secret `Value` must NEVER appear in logs. The `slog`
  output for Secret CRUD operations logs the ID and Name only.
- **API list response**: `GET /api/v1/secrets` returns only ID and Name,
  never the Value. This prevents accidental bulk exposure.
- **API get response**: `GET /api/v1/secrets/{id}` returns the Value.
  When API auth is implemented (SERVER_TODO), this endpoint must require
  authentication. Until then, the API is assumed to be on a private
  network (same as today for all endpoints).
- **SSE transit**: resolved secret values are embedded in the snapshot
  JSON. The SSE channel should be TLS-protected (CP listener with TLS).
  This is already the recommended production setup.
- **Raft replication**: Secrets are stored in bbolt and replicated via
  Raft like all other entities. The Raft log contains the raw values.
  Same security boundary as the rest of the config.
- **Snapshot persistence**: resolved snapshots in bbolt contain literal
  secret values. Same security boundary as the bbolt file itself.
  Operators must protect the bbolt file (file permissions, encrypted
  volume, etc.).
- **Proxy attack surface**: proxies do not need secret file mounts or
  env vars. They receive resolved values over SSE only. No local secret
  material to exfiltrate.

---

## Controller Impact (TLS Gap)

With this mechanism, the controller closes the TLS gap:

1. Reads the Secret referenced by `Gateway.spec.listeners[].tls.certificateRefs`
   from Kubernetes.
2. Creates two Vrata Secrets via the API:
   - `k8s:{ns}/{secret-name}:cert` with the PEM certificate
   - `k8s:{ns}/{secret-name}:key` with the PEM private key
3. Creates the Listener with:
   ```json
   "tls": {
     "cert": "{{secret:value:<cert-secret-id>}}",
     "key": "{{secret:value:<key-secret-id>}}"
   }
   ```
4. On snapshot creation, the CP resolves the references → proxy receives
   PEM inline → listener starts with TLS → gap closed.

---

## Proxy-side TLS Wiring Changes

### listener.go — `startListener`

Before:
```go
cert, err := tls.LoadX509KeyPair(l.TLS.CertPath, l.TLS.KeyPath)
```

After:
```go
cert, err := tls.X509KeyPair([]byte(l.TLS.Cert), []byte(l.TLS.Key))
```

### listener.go — mTLS CA loading

Before:
```go
caCert, err := os.ReadFile(ca.CAFile)
```

After:
```go
caCert := []byte(ca.CA)
```

### endpoint.go — `buildTLSConfig`

Before:
```go
caCert, err := os.ReadFile(tlsOpts.CAFile)
// ...
cert, err := tls.LoadX509KeyPair(tlsOpts.CertFile, tlsOpts.KeyFile)
```

After:
```go
caCert := []byte(tlsOpts.CA)
// ...
cert, err := tls.X509KeyPair([]byte(tlsOpts.Cert), []byte(tlsOpts.Key))
```

---

## Open Questions

1. **Secret rotation**: when a `value`-source Secret is updated via the
   API, existing activated snapshots contain the old resolved value. A
   new snapshot must be created and activated to pick up the change.
   Should the Secret update trigger an automatic re-snapshot? Or leave
   it manual/controller-driven? Leaning manual — the controller's next
   sync cycle handles it naturally.

2. **Validation**: should `SaveListener` validate that
   `{{secret:value:X}}` references an existing Secret at entity creation
   time? Or defer to snapshot build time? Leaning toward snapshot-build-
   time only — keeps CRUD fast and avoids ordering issues (create Secret
   first, then Listener, or vice versa).

3. **Naming**: `{{secret:value:...}}` vs `{{secret:vrata:...}}` vs
   `{{secret:store:...}}`? `value` is the simplest — the secret's
   value is what gets substituted.

4. **CA fallback**: `TLSOptions.CAFile` today falls back to
   `/etc/ssl/certs/ca-certificates.crt` when empty. After the rename
   to `CA`, an empty value should still mean "use system CA bundle".
   The proxy must handle this (use system pool when `CA` is empty).

---

## Implementation Order

1. Secret model + store + API (CRUD)
2. Model field renames (CertPath → Cert, etc.)
3. CP-side resolution in `buildSnapshot()`
4. Proxy TLS wiring changes (LoadX509KeyPair → X509KeyPair)
5. Tests (unit + e2e)
6. Controller: Secret CRUD from k8s Secrets + Listener wiring
7. Documentation
