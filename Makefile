VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO          := go
BUILD_DIR   := ./bin
TOOLS_DIR   := ./bin/tools
SERVER_DIR  := ./server
KC_DIR      := ./clients/controller

# Tool versions — pinned for reproducibility.
CONTROLLER_GEN_VERSION := v0.17.3
KIND_VERSION           := v0.27.0
SWAG_VERSION           := v2.0.0-rc5

SWAG        := $(TOOLS_DIR)/swag
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

.PHONY: build test e2e e2e-all clean deps docker-build docker-push kind-up kind-down kind-deploy-all \
	server-build server-run server-test server-e2e server-e2e-cluster \
	server-docs server-proto server-port-forward-podinfo \
	controller-build controller-test controller-e2e controller-generate-crd controller-deploy-crd \
	deploy-gateway-api-crds

# ═══════════════════════════════════════════════════════════════════════════════
# Common
# ═══════════════════════════════════════════════════════════════════════════════

## build: compile both binaries into ./bin/
build: server-build controller-build

## test: run all unit tests (server + controller)
test: server-test controller-test

## deps: install all development tools into ./bin/tools/
##
##   Installs: controller-gen, kind, swag, protoc-gen-go, protoc-gen-go-grpc
##   All pinned to specific versions for reproducibility.
deps:
	@mkdir -p $(TOOLS_DIR)
	@echo "→ Installing controller-gen $(CONTROLLER_GEN_VERSION)..."
	@GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
	@echo "→ Installing kind $(KIND_VERSION)..."
	@GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install sigs.k8s.io/kind@$(KIND_VERSION)
	@echo "→ Installing swag $(SWAG_VERSION)..."
	@GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install github.com/swaggo/swag/v2/cmd/swag@$(SWAG_VERSION)
	@echo "→ Installing protoc-gen-go..."
	@GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@echo "→ Installing protoc-gen-go-grpc..."
	@GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "✓ All tools installed in $(TOOLS_DIR)/"

## e2e: run all e2e tests (server proxy + controller)
e2e: server-e2e controller-e2e

## e2e-all: create kind cluster, install CRDs, run every e2e suite
e2e-all: kind-up deploy-gateway-api-crds server-e2e server-e2e-cluster controller-e2e

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## kind-deploy-all: build image, load into kind, install all three components via Helm
##
##   Creates the kind cluster if needed, installs Gateway API CRDs,
##   builds the unified Docker image, loads it into kind, and deploys
##   control plane + proxy + controller via Helm.
kind-deploy-all: kind-up deploy-gateway-api-crds docker-build
	@KIND=$$(command -v $(TOOLS_DIR)/kind || command -v kind) && \
		$$KIND load docker-image $(IMAGE):$(VERSION) --name $(KIND_CLUSTER)
	@HELM=$$(command -v $(TOOLS_DIR)/helm || command -v helm) && \
		$$HELM upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		--kube-context kind-$(KIND_CLUSTER) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		--set image.repository=$(IMAGE) \
		--set image.tag=$(VERSION) \
		--set image.pullPolicy=Never \
		--set controlPlane.enabled=true \
		--set proxy.enabled=true \
		--set controller.enabled=true \
		--wait --timeout 3m
	@echo "✓ All components deployed to kind cluster $(KIND_CLUSTER)"

_check-kind:
	@(test -f $(TOOLS_DIR)/kind || command -v kind > /dev/null 2>&1) || (echo "ERROR: kind not found. Run 'make deps' first." && exit 1)
	@command -v kubectl > /dev/null 2>&1 || (echo "ERROR: kubectl not found." && exit 1)
	@(test -f $(TOOLS_DIR)/helm || command -v helm > /dev/null 2>&1) || (echo "ERROR: helm not found." && exit 1)
	@KIND=$$(command -v $(TOOLS_DIR)/kind || command -v kind) && \
		$$KIND get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" || \
		(echo "ERROR: kind cluster '$(KIND_CLUSTER)' not found. Run 'make kind-up' first." && exit 1)
	@echo "✓ kind cluster $(KIND_CLUSTER) found"

## kind-up: create the kind cluster for e2e tests (idempotent)
kind-up:
	@KIND=$$(command -v $(TOOLS_DIR)/kind || command -v kind) && \
		$$KIND get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" \
		&& echo "✓ kind cluster $(KIND_CLUSTER) already exists" \
		|| (echo "→ Creating kind cluster $(KIND_CLUSTER)..." && $$KIND create cluster --name $(KIND_CLUSTER))

## kind-down: delete the kind cluster
kind-down:
	@KIND=$$(command -v $(TOOLS_DIR)/kind || command -v kind) && \
		$$KIND delete cluster --name $(KIND_CLUSTER) 2>/dev/null || true

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
server-e2e-cluster: _check-kind docker-build
	@echo "→ Loading image into kind..."
	@KIND=$$(command -v $(TOOLS_DIR)/kind || command -v kind) && \
		$$KIND load docker-image $(IMAGE):$(VERSION) --name $(KIND_CLUSTER)
	@echo "→ Installing Helm chart..."
	@HELM=$$(command -v $(TOOLS_DIR)/helm || command -v helm) && \
		$$HELM upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
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

## docker-build: build the Docker image (server + controller in one image)
docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## docker-push: push the Docker image
docker-push:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

## server-docs: regenerate OpenAPI docs from handler annotations
server-docs:
	$(SWAG) init $(SWAG_FLAGS)
	cd $(SERVER_DIR) && $(GO) run ./cmd/swag-reorder docs/swagger.json

## server-proto: regenerate Go code from .proto files
server-proto:
	PATH=$(abspath $(TOOLS_DIR)):$$PATH protoc \
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

## controller-generate-crd: generate the SuperHTTPRoute CRD and strip maxItems/CEL
##
##   Requires: controller-gen (go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest)
controller-generate-crd:
	@echo "→ Generating SuperHTTPRoute CRD..."
	@cd $(KC_DIR) && ../../$(TOOLS_DIR)/controller-gen crd paths=./apis/... output:crd:dir=./config/crd
	@echo "→ Cleaning maxItems and CEL validations..."
	@cd $(KC_DIR) && $(GO) run ./cmd/crdclean config/crd/vrata.io_superhttproutes.yaml config/crd/vrata.io_superhttproutes.yaml
	@echo "✓ CRD generated at $(KC_DIR)/config/crd/vrata.io_superhttproutes.yaml"

## controller-deploy-crd: install the SuperHTTPRoute CRD into the current k8s cluster
controller-deploy-crd:
	@echo "→ Installing SuperHTTPRoute CRD..."
	@kubectl apply --server-side -f $(KC_DIR)/config/crd/vrata.io_superhttproutes.yaml
	@kubectl wait --for condition=Established crd/superhttproutes.vrata.io --timeout=30s
	@echo "✓ SuperHTTPRoute CRD installed"

## deploy-gateway-api-crds: install Gateway API CRDs into the current k8s cluster
##
##   Override the version:
##     make controller-deploy-gateway-api-crds GATEWAY_API_VERSION=v1.4.0
controller-deploy-gateway-api-crds:
	@echo "→ Installing Gateway API CRDs $(GATEWAY_API_VERSION)..."
	@kubectl apply --server-side -f $(GATEWAY_API_CRDS_URL)
	@echo "→ Waiting for CRDs to be established..."
	@kubectl wait --for condition=Established crd/gateways.gateway.networking.k8s.io --timeout=30s
	@kubectl wait --for condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=30s
	@echo "✓ Gateway API CRDs $(GATEWAY_API_VERSION) installed"
