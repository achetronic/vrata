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

.PHONY: build docs docker-build docker-push run clean test e2e e2e-cluster e2e-all proto

KIND_CLUSTER   := rutoso-dev
KIND_IMAGE     := vrata:e2e-cluster
HELM_RELEASE   := vrata-e2e
HELM_NAMESPACE := vrata-e2e
HELM_NODEPORT  := 31081
HELM_VALUES    := $(HELM_CHART)/ci/kind-values.yaml
KIND_NODE_IP   := $(shell kubectl --context kind-$(KIND_CLUSTER) get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)

# ─── Standard targets ─────────────────────────────────────────────────────────

## docs: regenerate OpenAPI docs from handler annotations
docs:
	$(SWAG) init $(SWAG_FLAGS)
	cd $(SERVER_DIR) && $(GO) run ./cmd/swag-reorder docs/swagger.json

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
##
##   Rutoso starts as both:
##     - Management API on the configured address (default :8080)
##     - Proxy on whatever listeners you create via the API
##
##   Quick start:
##     1. make run
##     2. Create a listener:  curl -X POST localhost:8080/api/v1/listeners -H 'Content-Type: application/json' -d '{"name":"main","port":3000}'
##     3. Create a destination: curl -X POST localhost:8080/api/v1/destinations -H 'Content-Type: application/json' -d '{"name":"app","host":"httpbin.org","port":80}'
##     4. Create a route: curl -X POST localhost:8080/api/v1/routes -H 'Content-Type: application/json' -d '{"name":"test","match":{"pathPrefix":"/"},"forward":{"backends":[{"destinationId":"<ID>","weight":100}]}}'
##     5. Test: curl localhost:3000/get
##
##   Swagger UI: http://localhost:8080/api/v1/docs/
##
##   Overrides:
##     make run CONFIG=other.yaml STORE_PATH=/tmp/x.db
run: build
	$(BUILD_DIR)/$(BINARY) --config $(CONFIG) --store-path $(STORE_PATH)

## test: run unit tests (no external services required)
test:
	cd $(SERVER_DIR) && $(GO) test ./internal/... -v -race -count=1

## e2e: run proxy e2e tests
##
##   Requires:
##     - Control plane running on localhost:8080 (make run)
##     - Proxy running on localhost:3000
##
##   Run all e2e tests except cluster (kind not required):
e2e:
	cd $(SERVER_DIR) && $(GO) test ./test/e2e/ -v -timeout 300s -count=1 -run 'TestE2E_'

## e2e-cluster: run Raft cluster e2e tests against kind
##
##   Requires: kind, kubectl, helm, docker
##   Cluster name: rutoso-dev (must exist)
e2e-cluster: _check-kind build
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
		$(GO) test ./test/e2e/ -v -timeout 180s -count=1 -tags kind -run 'TestCluster_'

## e2e-all: run all e2e tests (proxy + cluster)
e2e-all: e2e e2e-cluster

## _check-kind: verify kind and kubectl are installed and cluster exists (internal)
_check-kind:
	@which kind > /dev/null 2>&1 || (echo "ERROR: kind not found. Install: https://kind.sigs.k8s.io/" && exit 1)
	@which kubectl > /dev/null 2>&1 || (echo "ERROR: kubectl not found." && exit 1)
	@which helm > /dev/null 2>&1 || (echo "ERROR: helm not found. Install: https://helm.sh/" && exit 1)
	@kind get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" || \
		(echo "ERROR: kind cluster '$(KIND_CLUSTER)' not found." && exit 1)
	@echo "✓ kind cluster $(KIND_CLUSTER) found"

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## proto: regenerate Go code from .proto files
##
##   Requires: protoc, protoc-gen-go, protoc-gen-go-grpc
##   Install:
##     go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
##     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
proto:
	protoc \
		--proto_path=$(SERVER_DIR)/proto \
		--go_out=$(SERVER_DIR)/proto --go_opt=paths=source_relative \
		--go-grpc_out=$(SERVER_DIR)/proto --go-grpc_opt=paths=source_relative \
		$(SERVER_DIR)/proto/extproc/v1/extproc.proto \
		$(SERVER_DIR)/proto/extauthz/v1/extauthz.proto
