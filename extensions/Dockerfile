# Dockerfile for Envoy with Vrata Go filter extensions.
#
# Build:
#   docker build -t vrata-envoy -f extensions/Dockerfile .
#
# The extensions are compiled as Go shared objects (.so) and mounted into
# the Envoy container at /etc/envoy/extensions/.
#
# Envoy loads them via the http_golang filter in the listener config:
#
#   http_filters:
#   - name: envoy.filters.http.golang
#     typed_config:
#       "@type": type.googleapis.com/envoy.extensions.filters.http.golang.v3alpha.Config
#       library_id: sticky
#       library_path: /etc/envoy/extensions/sticky.so
#       plugin_name: sticky
#       plugin_config: {}

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: Build Go extensions
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:1.24 AS builder

WORKDIR /build

# Copy each extension and build as .so plugin.
# Each extension is a separate Go module in extensions/<name>/.

COPY extensions/sticky/ ./sticky/
RUN cd sticky && go mod tidy && \
    go build -buildmode=plugin -o /out/sticky.so .

COPY extensions/inlineauthz/ ./inlineauthz/
RUN cd inlineauthz && go mod tidy && \
    go build -buildmode=plugin -o /out/inlineauthz.so .

COPY extensions/xfcc/ ./xfcc/
RUN cd xfcc && go mod tidy && \
    go build -buildmode=plugin -o /out/xfcc.so .

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Envoy with extensions
# ─────────────────────────────────────────────────────────────────────────────
FROM envoyproxy/envoy:v1.34-latest

COPY --from=builder /out/ /etc/envoy/extensions/

# Default entrypoint from the base image. Override with your envoy.yaml:
#   docker run -v ./envoy.yaml:/etc/envoy/envoy.yaml vrata-envoy
