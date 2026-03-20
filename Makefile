.PHONY: all build test clean install run-daemon run-client lint vet

BINARY_DAEMON=clickhouse-watcherd
BINARY_CLIENT=clickhouse-watch
BUILD_DIR=build
DIST_DIR=dist

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod

all: test build

build: build-daemon build-client

build-daemon:
	@echo "Building $(BINARY_DAEMON)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_DAEMON) ./cmd/daemon

build-client:
	@echo "Building $(BINARY_CLIENT)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_CLIENT) ./cmd/client

clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	$(GOCLEAN)

test: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v ./rrd/...

test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v ./tests/...

test-all:
	@echo "Running all tests..."
	$(GOTEST) -v ./...

lint:
	@echo "Linting..."
	$(GOVET) ./...

vet: vet

install: build
	@echo "Installing..."
	@mkdir -p /usr/local/bin
	@cp $(BUILD_DIR)/$(BINARY_DAEMON) /usr/local/bin/
	@cp $(BUILD_DIR)/$(BINARY_CLIENT) /usr/local/bin/

run-daemon: build-daemon
	@echo "Starting daemon..."
	@$(BUILD_DIR)/$(BINARY_DAEMON)

run-client: build-client
	@echo "Starting client..."
	@$(BUILD_DIR)/$(BINARY_CLIENT)

# Docker commands
docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

docker-test: docker-up
	@sleep 15
	$(GOTEST) -v ./tests/...

# Development helpers
fmt:
	$(GOCMD) fmt ./...

tidy:
	$(GOMOD) tidy

dev: tidy test lint build

.DEFAULT_GOAL := all
