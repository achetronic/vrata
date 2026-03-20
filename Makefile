VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO          := go
BUILD_DIR   := ./bin
SERVER_DIR  := ./server
KC_DIR      := ./clients/controller

SWAG        := swag
SWAG_FLAGS  := --generalInfo main.go \
	--dir $(SERVER_DIR)/cmd/vrata,$(SERVER_DIR)/internal/api/handlers,$(SERVER_DIR)/internal/api/respond,$(SERVER_DIR)/internal/model \
	--parseInternal \
	--output $(SERVER_DIR)/docs \
	--outputTypes go,json,yaml

IMAGE       := achetronic/vrata
CONFIG      ?= config.yaml

KIND_CLUSTER   := vrata-dev
KIND_IMAGE     := vrata:e2e-cluster
HELM_RELEASE   := vrata-e2e
HELM_NAMESPACE := vrata-e2e
HELM_CHART     := charts/vrata
HELM_NODEPORT  := 31081
HELM_VALUES    := $(HELM_CHART)/ci/kind-values.yaml
KIND_NODE_IP   := $(shell kubectl --context kind-$(KIND_CLUSTER) get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)

GATEWAY_API_VERSION ?= v1.5.1
GATEWAY_API_CRDS_URL := https://github.com/kubernetes-sigs/gateway-api/releases/download/$(GATEWAY_API_VERSION)/standard-install.yaml

.PHONY: build test e2e e2e-all clean kind-up kind-down \
	server-build server-run server-test server-e2e server-e2e-cluster \
	server-docker-build server-docker-push server-docs server-proto server-port-forward-podinfo \
	controller-build controller-test controller-e2e \
	deploy-gateway-api-crds

# ═══════════════════════════════════════════════════════════════════════════════
# Common
# ═══════════════════════════════════════════════════════════════════════════════

## build: compile both binaries into ./bin/
build: server-build controller-build

## test: run all unit tests (server + controller)
test: server-test controller-test

## e2e: run all e2e tests (server proxy + controller)
e2e: server-e2e controller-e2e

## e2e-all: create kind cluster, install CRDs, run every e2e suite
e2e-all: kind-up deploy-gateway-api-crds server-e2e server-e2e-cluster controller-e2e

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## kind-up: create the kind cluster for e2e tests (idempotent)
kind-up:
	@which kind > /dev/null 2>&1 || (echo "ERROR: kind not found. Install: https://kind.sigs.k8s.io/" && exit 1)
	@kind get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" \
		&& echo "✓ kind cluster $(KIND_CLUSTER) already exists" \
		|| (echo "→ Creating kind cluster $(KIND_CLUSTER)..." && kind create cluster --name $(KIND_CLUSTER))

## kind-down: delete the kind cluster
kind-down:
	@kind delete cluster --name $(KIND_CLUSTER) 2>/dev/null || true

## deploy-gateway-api-crds: install Gateway API CRDs into the current k8s cluster
##
##   Override the version:
##     make deploy-gateway-api-crds GATEWAY_API_VERSION=v1.4.0
deploy-gateway-api-crds:
	@echo "→ Installing Gateway API CRDs $(GATEWAY_API_VERSION)..."
	@kubectl apply --server-side -f $(GATEWAY_API_CRDS_URL)
	@echo "→ Waiting for CRDs to be established..."
	@kubectl wait --for condition=Established crd/gateways.gateway.networking.k8s.io --timeout=30s
	@kubectl wait --for condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=30s
	@echo "✓ Gateway API CRDs $(GATEWAY_API_VERSION) installed"

# ═══════════════════════════════════════════════════════════════════════════════
# Server
# ═══════════════════════════════════════════════════════════════════════════════

## server-build: compile the server binary into ./bin/vrata
server-build:
	mkdir -p $(BUILD_DIR)
	cd $(SERVER_DIR) && CGO_ENABLED=0 $(GO) build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD_DIR)/vrata ./cmd/vrata

## server-run: build and run the server with the default config
server-run: server-build
	$(BUILD_DIR)/vrata --config $(CONFIG)

## server-test: run server unit tests
server-test:
	cd $(SERVER_DIR) && $(GO) test ./internal/... -v -race -count=1

## server-e2e: run server proxy e2e tests
##
##   Requires:
##     - Control plane running on localhost:8080 (make server-run)
##     - Proxy running on localhost:3000
server-e2e:
	cd $(SERVER_DIR) && $(GO) test ./test/e2e/ -v -timeout 300s -count=1 -run 'TestE2E_'

## server-e2e-cluster: run Raft cluster e2e tests against kind
##
##   Requires: kind, kubectl, helm, docker
##   Cluster name: vrata-dev (must exist)
server-e2e-cluster: _check-kind server-build
	@echo "→ Building cluster image..."
	docker build -t $(KIND_IMAGE) .
	@echo "→ Loading image into kind..."
	kind load docker-image $(KIND_IMAGE) --name $(KIND_CLUSTER)
	@echo "→ Installing Helm chart..."
	helm upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		--kube-context kind-$(KIND_CLUSTER) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		-f $(HELM_VALUES) --wait --timeout 3m
	@echo "→ Running cluster e2e tests (KIND_NODE_IP=$(KIND_NODE_IP))..."
	cd $(SERVER_DIR) && KIND_NODE_IP=$(KIND_NODE_IP) \
		CLUSTER_NAMESPACE=$(HELM_NAMESPACE) \
		HELM_RELEASE=$(HELM_RELEASE) \
		CLUSTER_NODEPORT=$(HELM_NODEPORT) \
		KUBE_CONTEXT=kind-$(KIND_CLUSTER) \
		$(GO) test ./test/e2e/ -v -timeout 180s -count=1 -tags kind -run 'TestCluster_'

## server-docker-build: build the server Docker image
server-docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## server-docker-push: push the server Docker image
server-docker-push:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

## server-docs: regenerate OpenAPI docs from handler annotations
server-docs:
	$(SWAG) init $(SWAG_FLAGS)
	cd $(SERVER_DIR) && $(GO) run ./cmd/swag-reorder docs/swagger.json

## server-proto: regenerate Go code from .proto files
server-proto:
	protoc \
		--proto_path=$(SERVER_DIR)/proto \
		--go_out=$(SERVER_DIR)/proto --go_opt=paths=source_relative \
		--go-grpc_out=$(SERVER_DIR)/proto --go-grpc_opt=paths=source_relative \
		$(SERVER_DIR)/proto/extproc/v1/extproc.proto \
		$(SERVER_DIR)/proto/extauthz/v1/extauthz.proto

## server-port-forward-podinfo: forward podinfo from kind (needed for server-e2e)
server-port-forward-podinfo: _check-kind
	kubectl --context kind-$(KIND_CLUSTER) -n application-01 \
		wait --for=condition=Available deployment/application-podinfo --timeout=60s
	kubectl --context kind-$(KIND_CLUSTER) -n application-01 \
		port-forward svc/application-podinfo 9898:9898

# ═══════════════════════════════════════════════════════════════════════════════
# Controller
# ═══════════════════════════════════════════════════════════════════════════════

## controller-build: compile the controller binary into ./bin/controller
controller-build:
	mkdir -p $(BUILD_DIR)
	cd $(KC_DIR) && CGO_ENABLED=0 $(GO) build -ldflags="-s -w" -o ../../$(BUILD_DIR)/controller ./cmd/controller

## controller-test: run controller unit tests
controller-test:
	cd $(KC_DIR) && $(GO) test ./internal/... -v -race -count=1

## controller-e2e: run controller e2e tests
##
##   Requires:
##     - kubectl pointing to a cluster with Gateway API CRDs
##     - Vrata control plane on localhost:8080
controller-e2e:
	cd $(KC_DIR) && $(GO) test ./test/e2e/ -v -timeout 120s -count=1

_check-kind:
	@which kind > /dev/null 2>&1 || (echo "ERROR: kind not found. Install: https://kind.sigs.k8s.io/" && exit 1)
	@which kubectl > /dev/null 2>&1 || (echo "ERROR: kubectl not found." && exit 1)
	@which helm > /dev/null 2>&1 || (echo "ERROR: helm not found. Install: https://helm.sh/" && exit 1)
	@kind get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" || \
		(echo "ERROR: kind cluster '$(KIND_CLUSTER)' not found." && exit 1)
	@echo "✓ kind cluster $(KIND_CLUSTER) found"
