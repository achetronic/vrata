###############################################################################
## Server build
###############################################################################
FROM golang:1.25-alpine AS server-builder

ARG VERSION=dev

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION}" -o /bin/server ./cmd/vrata

###############################################################################
## Controller build
###############################################################################
FROM golang:1.25-alpine AS controller-builder

WORKDIR /src/controller
COPY clients/controller/go.mod clients/controller/go.sum ./
RUN go mod download
COPY clients/controller/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/controller ./cmd/controller

###############################################################################
## Final image
###############################################################################
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server-builder /bin/server /server
COPY --from=controller-builder /bin/controller /controller
EXPOSE 8080 7000 8081
ENTRYPOINT ["/server"]
