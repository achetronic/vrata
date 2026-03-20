---
title: "Docker"
weight: 2
---

Run Vrata using the official multi-arch Docker image. No binary download needed.

## Image

```
ghcr.io/achetronic/vrata:latest
```

Available for `linux/amd64` and `linux/arm64`. The image contains both `/server` and `/controller` binaries on a distroless base.

## Quick start

```bash
docker run -d \
  --name vrata \
  -p 8080:8080 \
  -p 3000:3000 \
  -v vrata-data:/data \
  ghcr.io/achetronic/vrata:latest \
  /server --config /dev/stdin <<'EOF'
mode: "controlplane"
controlPlane:
  address: ":8080"
  storePath: "/data"
log:
  format: "json"
  level: "info"
EOF
```

The REST API is on port 8080. Port 3000 is for a listener you'll create via the API.

## With a config file

Create `config.yaml` on your host:

```yaml
mode: "controlplane"

controlPlane:
  address: ":8080"
  storePath: "/data"

log:
  format: "json"
  level: "info"
```

Mount it into the container:

```bash
docker run -d \
  --name vrata \
  -p 8080:8080 \
  -p 3000:3000 \
  -v $(pwd)/config.yaml:/etc/vrata/config.yaml:ro \
  -v vrata-data:/data \
  ghcr.io/achetronic/vrata:latest \
  /server --config /etc/vrata/config.yaml
```

## Docker Compose

```yaml
services:
  vrata:
    image: ghcr.io/achetronic/vrata:latest
    command: ["/server", "--config", "/etc/vrata/config.yaml"]
    ports:
      - "8080:8080"
      - "3000:3000"
    volumes:
      - ./config.yaml:/etc/vrata/config.yaml:ro
      - vrata-data:/data

volumes:
  vrata-data:
```

```bash
docker compose up -d
```

## Verify

```bash
# Swagger UI
open http://localhost:8080/api/v1/docs/

# Create a listener + destination + route + snapshot (same as binary install)
curl -s -X POST localhost:8080/api/v1/listeners \
  -H 'Content-Type: application/json' \
  -d '{"name": "main", "port": 3000}'

# ... (see Binary install for the full verification flow)
```

## Pinning a version

Use a specific release tag instead of `latest`:

```bash
docker pull ghcr.io/achetronic/vrata:v0.1.0
```

## Running the controller

The same image contains the controller binary. Override the entrypoint:

```bash
docker run -d \
  --name vrata-controller \
  -v $(pwd)/controller-config.yaml:/etc/vrata/config.yaml:ro \
  ghcr.io/achetronic/vrata:latest \
  /controller --config /etc/vrata/config.yaml
```
