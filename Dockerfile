FROM golang:1.25-alpine AS builder

ARG VERSION=dev

WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION}" -o /bin/vrata ./cmd/vrata

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bin/vrata /vrata
EXPOSE 8080 7000
ENTRYPOINT ["/vrata"]
