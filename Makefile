.PHONY: all build test clean install run-daemon run-client lint vet

GIT_VERSION?=$(shell git describe --tags --always --abbrev=21 --dirty)
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
GOFLAGS=-ldflags "-X github.com/athoune/clickhouse-watcher/version.version=$(GIT_VERSION)"

all: test build

build: build-daemon build-client

build-daemon:
	@echo "Building $(BINARY_DAEMON)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_DAEMON) $(GOFLAGS) ./cmd/daemon

build-client:
	@echo "Building $(BINARY_CLIENT)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_CLIENT) $(GOFLAGS) ./cmd/client

clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	$(GOCLEAN)

TEST_TIMEOUT=60s

test: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -timeout $(TEST_TIMEOUT) ./rrd/... ./daemon/... ./client/... ./ui/...

test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -timeout $(TEST_TIMEOUT) ./tests/...

test-all:
	@echo "Running all tests..."
	$(GOTEST) -v -timeout $(TEST_TIMEOUT) ./...

lint:
	@echo "Linting..."
	$(GOVET) ./...

vet: vet

install: build
	@echo "Installing..."
	@mkdir -p /usr/local/bin
	@cp $(BUILD_DIR)/$(BINARY_DAEMON) /usr/local/bin/
	@cp $(BUILD_DIR)/$(BINARY_CLIENT) /usr/local/bin/

install-systemd: install
	@echo "Installing systemd files..."
	@mkdir -p /etc/clickhouse-watcher
	@mkdir -p /var/lib/clickhouse-watcher
	@mkdir -p /etc/systemd/system
	@mkdir -p /etc/tmpfiles.d
	@cp systemd/clickhouse-watcherd.service /etc/systemd/system/
	@cp systemd/clickhouse-watcherd.conf /etc/tmpfiles.d/
	@systemctl daemon-reload
	@systemd-tmpfiles --create
	@echo "Systemd files installed. Run: sudo systemctl enable --now clickhouse-watcherd"

# Cross-compilation targets
PLATFORMS=darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

build-all: $(PLATFORMS)

$(PLATFORMS):
	@echo "Building for $@..."
	@mkdir -p $(DIST_DIR)/$@
	GOOS=$(word 1,$(subst /, ,$@)) GOARCH=$(word 2,$(subst /, ,$@)) \
		$(GOBUILD) -o $(DIST_DIR)/$@/$(BINARY_DAEMON) $(GOFLAGS) ./cmd/daemon
	GOOS=$(word 1,$(subst /, ,$@)) GOARCH=$(word 2,$(subst /, ,$@)) \
		$(GOBUILD) -o $(DIST_DIR)/$@/$(BINARY_CLIENT) $(GOFLAGS) ./cmd/client

dist: build-all
	@echo "Creating distribution archives..."
	@mkdir -p $(DIST_DIR)/archives
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		tar -czf $(DIST_DIR)/archives/clickhouse-watcher-$(GIT_VERSION)-$${os}-$${arch}.tar.gz \
			-C $(DIST_DIR)/$$platform $(BINARY_DAEMON) $(BINARY_CLIENT); \
		echo "Created: $(DIST_DIR)/archives/clickhouse-watcher-$(GIT_VERSION)-$${os}-$${arch}.tar.gz"; \
	done

dist-clean: clean
	@rm -rf $(DIST_DIR)

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
	$(GOTEST) -v -timeout $(TEST_TIMEOUT) ./tests/...

# Development helpers
fmt:
	$(GOCMD) fmt ./...

tidy:
	$(GOMOD) tidy

dev: tidy test lint build

.DEFAULT_GOAL := all
