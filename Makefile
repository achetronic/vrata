###############################################################################
## Variables
###############################################################################

VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO              := go
BUILD_DIR       := ./bin
TOOLS_DIR       := $(BUILD_DIR)/tools
SERVER_DIR      := ./server
CONTROLLER_DIR  := ./clients/controller
IMAGE           := achetronic/vrata
CONFIG          ?= config.yaml

# Tool versions — pinned for reproducibility.
CONTROLLER_GEN_VERSION := v0.20.1
KIND_VERSION           := v0.31.0
SWAG_VERSION           := v2.0.0-rc5

# Kind / Helm settings for e2e.
KIND_CLUSTER   := vrata-dev
HELM_RELEASE   := vrata
HELM_NAMESPACE := vrata
HELM_CHART     := charts/vrata
HELM_NODEPORT  := 31081
HELM_VALUES    := $(HELM_CHART)/ci/kind-values.yaml
KIND_NODE_IP   := $(shell kubectl --context kind-$(KIND_CLUSTER) get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)

# Gateway API CRDs.
GATEWAY_API_VERSION  ?= v1.5.1
GATEWAY_API_CRDS_URL := https://github.com/kubernetes-sigs/gateway-api/releases/download/$(GATEWAY_API_VERSION)/standard-install.yaml

# Tool resolution — local ./bin/tools/ first, then system PATH.
KIND := $(shell command -v $(TOOLS_DIR)/kind 2>/dev/null || command -v kind 2>/dev/null)
HELM := $(shell command -v $(TOOLS_DIR)/helm 2>/dev/null || command -v helm 2>/dev/null)
SWAG := $(TOOLS_DIR)/swag

.PHONY: build test e2e e2e-all clean deps \
	docker-build docker-push \
	kind-up kind-down kind-deploy-all \
	server-build server-run server-test server-e2e server-e2e-cluster \
	server-docs server-proto server-port-forward-podinfo \
	controller-build controller-test controller-e2e \
	controller-generate-crd controller-deploy-crd controller-deploy-gateway-api-crds

###############################################################################
## Common
###############################################################################

## build: compile both binaries into ./bin/
build: server-build controller-build

## test: run all unit tests
test: server-test controller-test

## e2e: run all e2e tests (requires running infra)
e2e: server-e2e controller-e2e

## e2e-all: kind cluster + CRDs + every e2e suite
e2e-all: kind-up controller-deploy-gateway-api-crds server-e2e server-e2e-cluster controller-e2e

## clean: remove build artifacts and tools
clean:
	rm -rf $(BUILD_DIR)

## deps: install all development tools into ./bin/tools/
deps: $(TOOLS_DIR)/controller-gen $(TOOLS_DIR)/kind $(TOOLS_DIR)/swag $(TOOLS_DIR)/protoc-gen-go $(TOOLS_DIR)/protoc-gen-go-grpc
	@echo "✓ All tools installed in $(TOOLS_DIR)/"

$(TOOLS_DIR)/controller-gen:
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

$(TOOLS_DIR)/kind:
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install sigs.k8s.io/kind@$(KIND_VERSION)

$(TOOLS_DIR)/swag:
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install github.com/swaggo/swag/v2/cmd/swag@$(SWAG_VERSION)

$(TOOLS_DIR)/protoc-gen-go:
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest

$(TOOLS_DIR)/protoc-gen-go-grpc:
	@mkdir -p $(TOOLS_DIR)
	GOBIN=$(abspath $(TOOLS_DIR)) $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

###############################################################################
## Docker
###############################################################################

## docker-build: build the unified Docker image (server + controller)
docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## docker-push: push the Docker image to the registry
docker-push:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

###############################################################################
## Kind cluster
###############################################################################

## kind-up: create the kind cluster (idempotent)
kind-up:
	@$(KIND) get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" \
		&& echo "✓ kind cluster $(KIND_CLUSTER) already exists" \
		|| (echo "→ Creating kind cluster $(KIND_CLUSTER)..." && $(KIND) create cluster --name $(KIND_CLUSTER))

## kind-down: delete the kind cluster
kind-down:
	@$(KIND) delete cluster --name $(KIND_CLUSTER) 2>/dev/null || true

## kind-deploy-all: build, load, and deploy all three components into kind
kind-deploy-all: kind-up controller-deploy-gateway-api-crds docker-build
	@$(KIND) load docker-image $(IMAGE):$(VERSION) --name $(KIND_CLUSTER)
	@$(HELM) upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
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
	@test -n "$(KIND)" || (echo "ERROR: kind not found. Run 'make deps' first." && exit 1)
	@command -v kubectl > /dev/null 2>&1 || (echo "ERROR: kubectl not found." && exit 1)
	@test -n "$(HELM)" || (echo "ERROR: helm not found." && exit 1)
	@$(KIND) get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" || \
		(echo "ERROR: kind cluster '$(KIND_CLUSTER)' not found. Run 'make kind-up' first." && exit 1)
	@echo "✓ kind cluster $(KIND_CLUSTER) found"

###############################################################################
## Server
###############################################################################

## server-build: compile the server binary into ./bin/vrata
server-build:
	@mkdir -p $(BUILD_DIR)
	cd $(SERVER_DIR) && CGO_ENABLED=0 $(GO) build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD_DIR)/vrata ./cmd/vrata

## server-run: build and run with the default config
server-run: server-build
	$(BUILD_DIR)/vrata --config $(CONFIG)

## server-test: run server unit tests
server-test:
	cd $(SERVER_DIR) && $(GO) test ./internal/... -v -race -count=1

## server-e2e: run server proxy e2e tests
server-e2e:
	cd $(SERVER_DIR) && $(GO) test ./test/e2e/ -v -timeout 300s -count=1 -run 'TestE2E_'

## server-e2e-cluster: run Raft cluster e2e tests against kind
server-e2e-cluster: _check-kind docker-build
	@$(KIND) load docker-image $(IMAGE):$(VERSION) --name $(KIND_CLUSTER)
	@$(HELM) upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		--kube-context kind-$(KIND_CLUSTER) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		-f $(HELM_VALUES) --wait --timeout 3m
	cd $(SERVER_DIR) && KIND_NODE_IP=$(KIND_NODE_IP) \
		CLUSTER_NAMESPACE=$(HELM_NAMESPACE) \
		HELM_RELEASE=$(HELM_RELEASE) \
		CLUSTER_NODEPORT=$(HELM_NODEPORT) \
		KUBE_CONTEXT=kind-$(KIND_CLUSTER) \
		$(GO) test ./test/e2e/ -v -timeout 180s -count=1 -tags kind -run 'TestCluster_'

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

###############################################################################
## Controller
###############################################################################

## controller-build: compile the controller binary into ./bin/controller
controller-build:
	@mkdir -p $(BUILD_DIR)
	cd $(CONTROLLER_DIR) && CGO_ENABLED=0 $(GO) build -ldflags="-s -w" -o ../../$(BUILD_DIR)/controller ./cmd/controller

## controller-test: run controller unit tests
controller-test:
	cd $(CONTROLLER_DIR) && $(GO) test ./internal/... -v -race -count=1

## controller-e2e: run controller e2e tests
controller-e2e:
	cd $(CONTROLLER_DIR) && $(GO) test ./test/e2e/ -v -timeout 120s -count=1

## controller-generate-crd: generate SuperHTTPRoute CRD, strip maxItems/CEL, wrap for Helm
controller-generate-crd: $(TOOLS_DIR)/controller-gen
	@echo "→ Generating SuperHTTPRoute CRD..."
	@mkdir -p /tmp/vrata-crd
	@cd $(CONTROLLER_DIR) && ../../$(TOOLS_DIR)/controller-gen crd paths=./apis/... output:crd:dir=/tmp/vrata-crd
	@echo "→ Cleaning maxItems and CEL validations..."
	@cd $(CONTROLLER_DIR) && $(GO) run ./scripts/crdclean /tmp/vrata-crd/vrata.io_superhttproutes.yaml /tmp/vrata-crd/vrata.io_superhttproutes.yaml
	@echo "→ Wrapping for Helm..."
	@cd $(CONTROLLER_DIR) && $(GO) run ./scripts/helmwrap /tmp/vrata-crd/vrata.io_superhttproutes.yaml ../../$(HELM_CHART)/templates/controller/superhttproute-crd.yaml
	@rm -rf /tmp/vrata-crd
	@echo "✓ CRD generated at $(HELM_CHART)/templates/controller/superhttproute-crd.yaml"

## controller-deploy-crd: install controller CRDs via Helm
controller-deploy-crd: _check-kind
	@$(HELM) upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		--kube-context kind-$(KIND_CLUSTER) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		--set controller.enabled=true \
		--set controller.installCRDs=true \
		--wait --timeout 1m
	@kubectl wait --for condition=Established crd/superhttproutes.vrata.io --timeout=30s
	@echo "✓ SuperHTTPRoute CRD installed"

## controller-deploy-gateway-api-crds: install Gateway API CRDs into the current cluster
controller-deploy-gateway-api-crds:
	@echo "→ Installing Gateway API CRDs $(GATEWAY_API_VERSION)..."
	@kubectl apply --server-side -f $(GATEWAY_API_CRDS_URL)
	@kubectl wait --for condition=Established crd/gateways.gateway.networking.k8s.io --timeout=30s
	@kubectl wait --for condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=30s
	@echo "✓ Gateway API CRDs $(GATEWAY_API_VERSION) installed"
