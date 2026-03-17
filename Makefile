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

.PHONY: build docs docker-build docker-push run clean test proto

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

## test: run all tests
test:
	cd $(SERVER_DIR) && $(GO) test ./... -v -race

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
