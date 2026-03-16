BINARY      := rutoso
IMAGE       := achetronic/$(BINARY)
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
CONFIG      ?= config.yaml
STORE_PATH  ?= /tmp/rutoso.db

GO          := go
GOFLAGS     := -ldflags="-s -w -X main.version=$(VERSION)"
BUILD_DIR   := ./bin
SERVER_DIR  := ./server

SWAG        := swag
SWAG_FLAGS  := --generalInfo main.go \
	--dir $(SERVER_DIR)/cmd/rutoso,$(SERVER_DIR)/internal/api/handlers,$(SERVER_DIR)/internal/api/respond,$(SERVER_DIR)/internal/model \
	--parseInternal \
	--output $(SERVER_DIR)/docs \
	--outputTypes go,json,yaml

# ─── Kind dev environment ─────────────────────────────────────────────────────
KIND_CLUSTER   := rutoso-dev
KIND_CTX       := kind-$(KIND_CLUSTER)
KIND_NAMESPACE := default
KIND_CONFIG    := dev/kind-cluster.yaml
ENVOY_TMPL     := dev/envoy.yaml.tmpl
ENVOY_IMAGE    := envoyproxy/envoy:v1.33-latest
ENVOY_NODE_ID  := envoy-dev-0

# Host IP as seen from inside Kind pods (Docker 'kind' network gateway).
# Auto-detected after cluster creation. Override: make dev-up HOST_IP=1.2.3.4
HOST_IP ?= $(shell docker network inspect kind --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null)

.PHONY: build docs docker-build docker-push run run-dev clean \
        dev-up dev-down dev-envoy-logs dev-envoy-admin

# ─── Standard targets ─────────────────────────────────────────────────────────

## docs: regenerate OpenAPI docs from handler annotations (requires swag v2 in PATH)
docs:
	$(SWAG) init $(SWAG_FLAGS)

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

## run: build and run with the default config
run: build
	$(BUILD_DIR)/$(BINARY) --config $(CONFIG) --store-path $(STORE_PATH)

## run-dev: alias for run (kept for backwards compatibility)
run-dev: run

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

# ─── Kind dev environment ─────────────────────────────────────────────────────

## dev-up: spin up a Kind cluster with Envoy inside, then start Rutoso locally.
##
##   Steps:
##     1. Creates Kind cluster '$(KIND_CLUSTER)' if it doesn't exist yet.
##     2. Auto-detects the host IP visible from inside the cluster.
##     3. Deploys Envoy configured to reach Rutoso xDS on :18000.
##     4. Starts Rutoso in the foreground. Ctrl+C stops Rutoso; cluster keeps running.
##
##   Access points:
##     Rutoso REST  → http://localhost:8080/api/v1/
##     Rutoso docs  → http://localhost:8080/api/v1/docs/
##     Envoy proxy  → http://localhost:30000  (after Rutoso pushes a listener)
##     Envoy admin  → http://localhost:30901
##
##   Overrides:
##     make dev-up CONFIG=other.yaml STORE_PATH=/tmp/x.db HOST_IP=1.2.3.4
dev-up: build
	@# ── 1. Create cluster if it doesn't exist ──────────────────────────────
	@if kind get clusters 2>/dev/null | grep -qx "$(KIND_CLUSTER)"; then \
		echo "Kind cluster '$(KIND_CLUSTER)' already exists."; \
	else \
		echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
		kind create cluster --name $(KIND_CLUSTER) --config $(KIND_CONFIG); \
	fi
	@# ── 2. Detect host IP, substitute in template, deploy Envoy ───────────
	@HOST_IP="$(HOST_IP)"; \
	if [ -z "$$HOST_IP" ]; then \
		HOST_IP=$$(docker network inspect kind --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null); \
	fi; \
	if [ -z "$$HOST_IP" ]; then \
		echo "ERROR: could not detect host IP from Docker 'kind' network."; \
		echo "       Pass it manually: make dev-up HOST_IP=x.x.x.x"; \
		exit 1; \
	fi; \
	echo "Host IP (visible from Kind): $$HOST_IP"; \
	echo "Deploying Envoy (xDS → $$HOST_IP:18000)..."; \
	sed \
		-e "s|__HOST_IP__|$$HOST_IP|g" \
		-e "s|__ENVOY_IMAGE__|$(ENVOY_IMAGE)|g" \
		-e "s|__ENVOY_NODE_ID__|$(ENVOY_NODE_ID)|g" \
		-e "s|__KIND_NAMESPACE__|$(KIND_NAMESPACE)|g" \
		$(ENVOY_TMPL) \
	| kubectl --context $(KIND_CTX) apply -n $(KIND_NAMESPACE) -f -
	@# ── 3. Wait for Envoy pod ───────────────────────────────────────────────
	@kubectl --context $(KIND_CTX) rollout status deployment/envoy \
		-n $(KIND_NAMESPACE) --timeout=90s
	@# ── 4. Summary + start Rutoso ──────────────────────────────────────────
	@echo ""
	@echo "────────────────────────────────────────────────────────"
	@echo "  cluster     : $(KIND_CLUSTER)"
	@echo "  Rutoso REST : http://localhost:8080/api/v1/"
	@echo "  Rutoso docs : http://localhost:8080/api/v1/docs/"
	@echo "  Envoy proxy : http://localhost:30000"
	@echo "  Envoy admin : http://localhost:30901"
	@echo "────────────────────────────────────────────────────────"
	@echo "  Ctrl+C stops Rutoso. Cluster keeps running."
	@echo "  Destroy cluster: make dev-down"
	@echo "────────────────────────────────────────────────────────"
	@echo ""
	$(BUILD_DIR)/$(BINARY) --config $(CONFIG) --store-path $(STORE_PATH)

## dev-down: destroy the Kind dev cluster
dev-down:
	kind delete cluster --name $(KIND_CLUSTER)
	@echo "Cluster '$(KIND_CLUSTER)' deleted."

## dev-envoy-logs: tail Envoy logs in real time (xDS stream events visible here)
dev-envoy-logs:
	kubectl --context $(KIND_CTX) logs -f deploy/envoy -n $(KIND_NAMESPACE)

## dev-envoy-admin: port-forward Envoy admin to localhost:9901
##   http://localhost:9901/config_dump → inspect everything Rutoso has pushed
dev-envoy-admin:
	@echo "Envoy admin → http://localhost:9901"
	@echo "              http://localhost:9901/config_dump"
	kubectl --context $(KIND_CTX) port-forward svc/envoy-admin 9901:9901 -n $(KIND_NAMESPACE)
