# CacheGrid Makefile

BINARY_NAME := cachegrid
BUILD_DIR := ./build
CMD_DIR := ./cmd/cachegrid
DOCKER_IMAGE := cachegrid
DOCKER_TAG := latest

# Go settings
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: all build run run-disk test test-verbose test-race bench lint fmt vet clean \
        docker-build docker-run docker-compose-up docker-compose-down help

## ─── Build ──────────────────────────────────────────────

all: test build ## Run tests and build

build: ## Build the cachegrid binary
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME)"

install: ## Install cachegrid to GOPATH/bin
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_DIR)

## ─── Run ────────────────────────────────────────────────

run: build ## Run standalone server (in-memory mode)
	$(BUILD_DIR)/$(BINARY_NAME)

run-disk: build ## Run standalone server (disk mode)
	CACHEGRID_STORAGE_MODE=disk CACHEGRID_DISK_PATH=./cachegrid-data $(BUILD_DIR)/$(BINARY_NAME)

run-cluster: build ## Run a 3-node local cluster
	@echo "Starting node-1..."
	@CACHEGRID_NODE_NAME=node-1 CACHEGRID_LISTEN_ADDR=:7946 CACHEGRID_HTTP_PORT=6380 \
		CACHEGRID_RPC_PORT=7947 $(BUILD_DIR)/$(BINARY_NAME) &
	@sleep 1
	@echo "Starting node-2..."
	@CACHEGRID_NODE_NAME=node-2 CACHEGRID_LISTEN_ADDR=:7948 CACHEGRID_HTTP_PORT=6381 \
		CACHEGRID_RPC_PORT=7949 CACHEGRID_SEEDS=127.0.0.1:7946 $(BUILD_DIR)/$(BINARY_NAME) &
	@sleep 1
	@echo "Starting node-3..."
	@CACHEGRID_NODE_NAME=node-3 CACHEGRID_LISTEN_ADDR=:7950 CACHEGRID_HTTP_PORT=6382 \
		CACHEGRID_RPC_PORT=7951 CACHEGRID_SEEDS=127.0.0.1:7946 $(BUILD_DIR)/$(BINARY_NAME) &
	@echo "Cluster running: http://localhost:6380, :6381, :6382"

## ─── Test ───────────────────────────────────────────────

test: ## Run all tests
	go test ./... -count=1

test-verbose: ## Run all tests with verbose output
	go test ./... -v -count=1

test-race: ## Run tests with race detector
	go test ./... -race -count=1

test-cover: ## Run tests with coverage report
	@mkdir -p $(BUILD_DIR)
	go test ./... -coverprofile=$(BUILD_DIR)/coverage.out -count=1
	go tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@echo "Coverage report: $(BUILD_DIR)/coverage.html"

test-short: ## Run only fast tests (skip slow ones)
	go test ./... -short -count=1

## ─── Benchmark ──────────────────────────────────────────

bench: ## Run benchmarks
	go test -bench=. -benchmem -count=1

bench-cpu: ## Run benchmarks with CPU profile
	@mkdir -p $(BUILD_DIR)
	go test -bench=. -benchmem -cpuprofile=$(BUILD_DIR)/cpu.prof -count=1
	@echo "CPU profile: go tool pprof $(BUILD_DIR)/cpu.prof"

bench-mem: ## Run benchmarks with memory profile
	@mkdir -p $(BUILD_DIR)
	go test -bench=. -benchmem -memprofile=$(BUILD_DIR)/mem.prof -count=1
	@echo "Mem profile: go tool pprof $(BUILD_DIR)/mem.prof"

## ─── Code Quality ───────────────────────────────────────

fmt: ## Format all Go files
	gofmt -s -w .

vet: ## Run go vet
	go vet ./...

lint: vet ## Run linter (requires golangci-lint)
	@which golangci-lint > /dev/null 2>&1 || \
		(echo "Install golangci-lint: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

tidy: ## Tidy go.mod and go.sum
	go mod tidy

## ─── Docker ─────────────────────────────────────────────

docker-build: ## Build Docker image
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run: docker-build ## Run in Docker (single node)
	docker run -d --name cachegrid -p 6380:6380 -p 7946:7946 $(DOCKER_IMAGE):$(DOCKER_TAG)
	@echo "Running at http://localhost:6380"

docker-run-disk: docker-build ## Run in Docker with disk storage
	docker run -d --name cachegrid-disk \
		-p 6380:6380 -p 7946:7946 \
		-e CACHEGRID_STORAGE_MODE=disk \
		-e CACHEGRID_DISK_PATH=/data \
		-v cachegrid-data:/data \
		$(DOCKER_IMAGE):$(DOCKER_TAG)
	@echo "Running at http://localhost:6380 (disk mode, volume: cachegrid-data)"

docker-stop: ## Stop and remove Docker container
	docker stop cachegrid 2>/dev/null || true
	docker rm cachegrid 2>/dev/null || true
	docker stop cachegrid-disk 2>/dev/null || true
	docker rm cachegrid-disk 2>/dev/null || true

docker-compose-up: ## Start 3-node cluster via Docker Compose
	docker compose up -d
	@echo "Cluster running: http://localhost:6380, :6381, :6382"

docker-compose-down: ## Stop Docker Compose cluster
	docker compose down

## ─── Kubernetes ─────────────────────────────────────────

k8s-deploy: ## Deploy to Kubernetes
	kubectl apply -f kubernetes.yaml

k8s-delete: ## Delete Kubernetes deployment
	kubectl delete -f kubernetes.yaml

## ─── Utility ────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
	rm -rf ./cachegrid-data

deps: ## Download dependencies
	go mod download

check: fmt vet test ## Format, vet, and test (pre-commit check)

health: ## Check health of running server
	@curl -s http://localhost:6380/health | head -c 200
	@echo

smoke: build ## Quick smoke test: start server, set/get, stop
	@echo "Starting server..."
	@$(BUILD_DIR)/$(BINARY_NAME) &
	@SERVER_PID=$$!; \
	sleep 1; \
	echo "Setting key..."; \
	curl -s -X PUT http://localhost:6380/cache/test -H "X-TTL: 1m" -d '"hello"'; \
	echo; \
	echo "Getting key..."; \
	curl -s http://localhost:6380/cache/test; \
	echo; \
	echo "Health check..."; \
	curl -s http://localhost:6380/health; \
	echo; \
	kill $$SERVER_PID 2>/dev/null; \
	echo "Smoke test passed."

## ─── Help ───────────────────────────────────────────────

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
