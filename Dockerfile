# syntax=docker/dockerfile:1

# ---- builder stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Download dependencies first (cached layer).
COPY server/go.mod server/go.sum ./
RUN go mod download

# Copy source and build a static binary.
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/rutoso \
    ./cmd/rutoso

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/rutoso /rutoso

# Management API (default :8080). Proxy listeners are dynamic.
EXPOSE 8080

ENTRYPOINT ["/rutoso"]
