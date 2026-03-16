# Vrata

A programmable reverse proxy you control entirely through a REST API. Create routes, point them at upstreams, attach middlewares, and Vrata reconfigures itself on the fly — no restarts, no config files to manage, no external proxy.

One binary. Zero dependencies.

## What can it do?

→ **Routing** — path, headers, methods, query params, hostnames, gRPC, regex, and [CEL expressions](https://github.com/google/cel-go) for when static matchers aren't enough.

→ **Actions** — forward traffic to weighted backends, return redirects, or serve fixed responses. Forwarding supports retries with backoff, request timeouts, URL rewriting (prefix and regex), request mirroring, and WebSocket upgrades.

→ **Load balancing** — round robin, ring hash, maglev, least request, random. Health checks, circuit breakers, and outlier detection per destination.

→ **Middlewares** — CORS, JWT validation (RSA / EC / Ed25519), external authorization (HTTP and gRPC), external processing (bidirectional, with its own proto), header manipulation, rate limiting, and access logging. Create a middleware once, attach it to any route or group by ID.

→ **Versioned snapshots** — configuration changes don't go live until you say so. Capture a snapshot, activate it, and every connected proxy picks it up instantly. Bad deploy? Activate the previous snapshot and you're back.

→ **Kubernetes native** — discovers pod IPs via EndpointSlice watches. Also supports ExternalName Services. If there's no kubeconfig, the watcher disables itself silently.

→ **Two modes** — run everything in one process (control plane + proxy) for development, or split the control plane from N proxy instances for production. Proxies connect via SSE and sync automatically.

## Quick start

The repo ships with a `config.yaml` at the root with sensible defaults. Copy it and adjust if needed, or use it as-is:

```bash
make build
./bin/vrata --config config.yaml
```

That starts Vrata in control plane mode: the REST API listens on `:8080` and proxied traffic goes through whatever listeners you create.

Now configure it:

```bash
# Create a listener on port 3000
curl -s -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{"name":"main","port":3000}'

# Create a destination
DEST=$(curl -s -X POST localhost:8080/api/v1/destinations \
  -H 'Content-Type: application/json' \
  -d '{"name":"httpbin","host":"httpbin.org","port":80}' | jq -r .id)

# Create a route
curl -s -X POST localhost:8080/api/v1/routes \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"test\",\"match\":{\"pathPrefix\":\"/\"},\"forward\":{\"backends\":[{\"destinationId\":\"$DEST\",\"weight\":100}]}}"

# Capture the config and push it live
SNAP=$(curl -s -X POST localhost:8080/api/v1/snapshots \
  -H 'Content-Type: application/json' \
  -d '{"name":"v1"}' | jq -r .id)
curl -s -X POST localhost:8080/api/v1/snapshots/$SNAP/activate

# Done — traffic flows
curl localhost:3000/get
```

Swagger UI is available at `http://localhost:8080/api/v1/docs/`.

## Configuration

Vrata reads a YAML config file passed via `--config`. The repo includes [`config.yaml`](config.yaml) with all available options and commented defaults. Every string value supports `${ENV_VAR:-default}` substitution.

```yaml
# "controlplane" (default) — API + store + proxy in one process
# "proxy" — connects to a remote control plane, no local API or store
mode: "${VRATA_MODE:-controlplane}"

server:
  address: "${SERVER_ADDRESS:-:8080}"

# Only used in proxy mode
controlPlane:
  address: "${CONTROLPLANE_ADDRESS:-}"
  reconnectInterval: "5s"

log:
  format: "console"   # or "json"
  level: "info"        # debug, info, warn, error
```

The persistent state lives in a single bbolt file. Pass `--store-path` to control where it goes (default: in-memory for development).

## Build & test

```bash
make build          # Binary at ./bin/vrata
make test           # Unit + e2e tests
make proto          # Regenerate protobuf Go code
make docs           # Regenerate OpenAPI spec
make docker-build   # Docker image
```

## Deploy

**Single node** (default) — control plane + proxy in one process:

```bash
./bin/vrata --config config.yaml --store-path /data/vrata.db
```

**Multi node** — one control plane, N stateless proxies:

```yaml
# proxy.yaml
mode: "proxy"
controlPlane:
  address: "http://control-plane:8080"
```

Proxies reconnect automatically on disconnect. They hold no state — fully disposable.

**Docker:**

```bash
docker run -d \
  -v ./config.yaml:/config.yaml \
  -v vrata-data:/data \
  -p 8080:8080 -p 3000:3000 \
  achetronic/vrata:latest \
  --config /config.yaml --store-path /data/vrata.db
```

## Extending Vrata

### External processor

Vrata defines its own gRPC protocol for bidirectional request/response processing at [`server/proto/extproc/v1/extproc.proto`](server/proto/extproc/v1/extproc.proto). Processors can also run in HTTP mode (JSON) with full feature parity. Write a processor in any language, point a middleware at it, and Vrata sends every request phase through it.

### External authorization

Authorization services implement [`server/proto/extauthz/v1/extauthz.proto`](server/proto/extauthz/v1/extauthz.proto) (gRPC) or respond to plain HTTP check requests. Vrata forwards configurable headers, evaluates the response, and either allows the request through or returns the denial to the client.

## Contributing

Contributions are welcome. The codebase is Go, standard library where possible, minimal dependencies.

```bash
git clone https://github.com/achetronic/vrata.git
cd vrata
make test
```

The project follows a few rules documented in [`.agents/CONVENTIONS.md`](.agents/CONVENTIONS.md):

→ `net/http` only — no external routers
→ `log/slog` only — no third-party loggers
→ Errors bubble up — handlers decide what to do with them
→ No manual `ResponseWriter` wrappers — use [`httpsnoop`](https://github.com/felixge/httpsnoop)
→ A broken route never takes down other routes

Architecture and technical decisions live in [`.agents/`](.agents/).

## License

Apache 2.0
