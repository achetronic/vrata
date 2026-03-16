BINARY      := rutoso
IMAGE       := achetronic/$(BINARY)
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
CONFIG      ?= config.yaml

# Local dev: the xDS gRPC address Envoys in Kubernetes should reach.
# Set XDS_ADDR to your machine's IP/hostname + port before running run-dev.
# Example: make run-dev XDS_ADDR=192.168.1.42:18000
XDS_ADDR    ?= 0.0.0.0:18000

GO          := go
GOFLAGS     := -ldflags="-s -w -X main.version=$(VERSION)"
BUILD_DIR   := ./bin
SERVER_DIR  := ./server

.PHONY: build docker-build docker-push run run-dev clean

## build: compile the binary into ./bin/rutoso
build:
	mkdir -p $(BUILD_DIR)
	cd $(SERVER_DIR) && CGO_ENABLED=0 $(GO) build $(GOFLAGS) -o ../$(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

## docker-build: build the Docker image
docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## docker-push: push the Docker image to the registry
docker-push:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

## run: run the binary with the default config file
run: build
	$(BUILD_DIR)/$(BINARY) --config $(CONFIG)

## run-dev: run locally with the xDS address overridden for Kubernetes-reachable dev mode.
## Envoys deployed in Kubernetes should point their xds_grpc.address to XDS_ADDR.
## Useful when developing against a real cluster: the controller runs on your laptop
## and Envoy pods reach it via NodePort, LoadBalancer, or a tunnel (e.g. telepresence).
run-dev: build
	XDS_ADDRESS=$(XDS_ADDR) $(BUILD_DIR)/$(BINARY) --config $(CONFIG)

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
