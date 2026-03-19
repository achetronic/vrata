---
title: "Binary"
weight: 1
---

Download a pre-built binary from the GitHub release and run it directly.

## Download

Grab the latest release for your platform:

```bash
# Linux (amd64)
curl -Lo vrata https://github.com/achetronic/vrata/releases/latest/download/vrata-linux-amd64
chmod +x vrata

# Linux (arm64)
curl -Lo vrata https://github.com/achetronic/vrata/releases/latest/download/vrata-linux-arm64
chmod +x vrata

# macOS (Apple Silicon)
curl -Lo vrata https://github.com/achetronic/vrata/releases/latest/download/vrata-darwin-arm64
chmod +x vrata

# macOS (Intel)
curl -Lo vrata https://github.com/achetronic/vrata/releases/latest/download/vrata-darwin-amd64
chmod +x vrata
```

Or browse all releases at [github.com/achetronic/vrata/releases](https://github.com/achetronic/vrata/releases).

## Configure

Create a `config.yaml`:

```yaml
mode: "controlplane"

controlPlane:
  address: ":8080"
  storePath: "./data"

log:
  format: "console"
  level: "info"
```

This starts the control plane on port 8080 with bbolt storage in `./data/`. The control plane also runs the proxy internally — one process does everything for development.

## Run

```bash
./vrata --config config.yaml
```

The REST API is live at `http://localhost:8080`. Swagger UI at [http://localhost:8080/api/v1/docs/](http://localhost:8080/api/v1/docs/).

## Verify

```bash
# Create a listener
curl -s -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{"name": "main", "port": 3000}'

# Create a destination
DEST_ID=$(curl -s -X POST localhost:8080/api/v1/destinations \
  -H 'Content-Type: application/json' \
  -d '{"name": "httpbin", "host": "httpbin.org", "port": 80}' | jq -r .id)

# Create a route
curl -s -X POST localhost:8080/api/v1/routes \
  -H 'Content-Type: application/json' \
  -d "{\"name\": \"catch-all\", \"match\": {\"pathPrefix\": \"/\"}, \"forward\": {\"destinations\": [{\"destinationId\": \"$DEST_ID\", \"weight\": 100}]}}"

# Snapshot and activate
SNAP_ID=$(curl -s -X POST localhost:8080/api/v1/snapshots \
  -H 'Content-Type: application/json' \
  -d '{"name": "v1"}' | jq -r .id)

curl -s -X POST localhost:8080/api/v1/snapshots/$SNAP_ID/activate

# Test
curl localhost:3000/get
```

## Build from source

If you prefer to build from source:

```bash
git clone https://github.com/achetronic/vrata.git
cd vrata
make build
```

Binaries are written to `./bin/vrata` (server) and `./bin/controller` (Kubernetes controller).
