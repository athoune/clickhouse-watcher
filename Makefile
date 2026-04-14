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

.cache-go:
	mkdir .cache-go

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

# Debian/Ubuntu package creation
DEB_VERSION=$(shell echo $(GIT_VERSION) | sed 's/^v//')
DEB_ARCH_AMD64=amd64
DEB_ARCH_ARM64=arm64

deb: deb-amd64 deb-arm64

deb-from-docker: .cache-go
	@echo "Building Debian packages in Docker with cache persistence..."
	docker run -ti --rm \
		-u `id -u`:`id -g` \
		-v `pwd`:/usr/src \
		-v `pwd`/.cache-go:/go/cache \
		-w /usr/src \
		-e GOCACHE=/go/cache \
		-e GOMODCACHE=/go/cache/mod \
		-e HOME=/tmp \
		golang:1.26-trixie \
		make deb

deb-amd64: build-linux-amd64
	@echo "Creating Debian package for amd64..."
	@mkdir -p $(DIST_DIR)/deb/amd64/DEBIAN
	@mkdir -p $(DIST_DIR)/deb/amd64/usr/local/bin
	@mkdir -p $(DIST_DIR)/deb/amd64/etc/clickhouse-watcher
	@mkdir -p $(DIST_DIR)/deb/amd64/etc/systemd/system
	@mkdir -p $(DIST_DIR)/deb/amd64/etc/tmpfiles.d
	@mkdir -p $(DIST_DIR)/deb/amd64/var/lib/clickhouse-watcher
	@cp $(DIST_DIR)/linux/amd64/$(BINARY_DAEMON) $(DIST_DIR)/deb/amd64/usr/local/bin/
	@cp $(DIST_DIR)/linux/amd64/$(BINARY_CLIENT) $(DIST_DIR)/deb/amd64/usr/local/bin/
	@cp systemd/clickhouse-watcherd.service $(DIST_DIR)/deb/amd64/etc/systemd/system/
	@cp systemd/clickhouse-watcherd.conf $(DIST_DIR)/deb/amd64/etc/tmpfiles.d/
	@echo "Package: clickhouse-watcher" > $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "Version: $(DEB_VERSION)" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "Section: utils" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "Priority: optional" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "Architecture: $(DEB_ARCH_AMD64)" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "Maintainer: Mathieu Lecarme <mathieu@garambrogne.net>" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "Description: ClickHouse Watcher - Monitor and manage ClickHouse clusters" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo " ClickHouse Watcher provides a TUI interface to monitor ClickHouse" >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo " metrics, tables, processes, and manage TTL and truncation." >> $(DIST_DIR)/deb/amd64/DEBIAN/control
	@echo "#!/bin/bash" > $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "set -e" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "# Create system user if it doesn't exist" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "if ! id -u clickhouse_watcher >/dev/null 2>&1; then" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "    useradd --system --home-dir /var/lib/clickhouse-watcher --shell /usr/sbin/nologin clickhouse_watcher" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "fi" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "# Create group if it doesn't exist" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "if ! getent group clickhouse_watcherd >/dev/null 2>&1; then" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "    groupadd --system clickhouse_watcherd" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "fi" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "# Set permissions" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "chown -R clickhouse_watcher:clickhouse_watcherd /var/lib/clickhouse-watcher" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "chmod 750 /var/lib/clickhouse-watcher" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "# Reload systemd" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "systemctl daemon-reload" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@echo "systemd-tmpfiles --create" >> $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@chmod 755 $(DIST_DIR)/deb/amd64/DEBIAN/postinst
	@mkdir -p $(DIST_DIR)/archives
	@dpkg-deb --build $(DIST_DIR)/deb/amd64 $(DIST_DIR)/archives/clickhouse-watcher_$(DEB_VERSION)_$(DEB_ARCH_AMD64).deb
	@echo "Created: $(DIST_DIR)/archives/clickhouse-watcher_$(DEB_VERSION)_$(DEB_ARCH_AMD64).deb"

deb-arm64: build-linux-arm64
	@echo "Creating Debian package for arm64..."
	@mkdir -p $(DIST_DIR)/deb/arm64/DEBIAN
	@mkdir -p $(DIST_DIR)/deb/arm64/usr/local/bin
	@mkdir -p $(DIST_DIR)/deb/arm64/etc/clickhouse-watcher
	@mkdir -p $(DIST_DIR)/deb/arm64/etc/systemd/system
	@mkdir -p $(DIST_DIR)/deb/arm64/etc/tmpfiles.d
	@mkdir -p $(DIST_DIR)/deb/arm64/var/lib/clickhouse-watcher
	@cp $(DIST_DIR)/linux/arm64/$(BINARY_DAEMON) $(DIST_DIR)/deb/arm64/usr/local/bin/
	@cp $(DIST_DIR)/linux/arm64/$(BINARY_CLIENT) $(DIST_DIR)/deb/arm64/usr/local/bin/
	@cp systemd/clickhouse-watcherd.service $(DIST_DIR)/deb/arm64/etc/systemd/system/
	@cp systemd/clickhouse-watcherd.conf $(DIST_DIR)/deb/arm64/etc/tmpfiles.d/
	@echo "Package: clickhouse-watcher" > $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "Version: $(DEB_VERSION)" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "Section: utils" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "Priority: optional" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "Architecture: $(DEB_ARCH_ARM64)" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "Maintainer: Mathieu Lecarme <mathieu@garambrogne.net>" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "Description: ClickHouse Watcher - Monitor and manage ClickHouse clusters" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo " ClickHouse Watcher provides a TUI interface to monitor ClickHouse" >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo " metrics, tables, processes, and manage TTL and truncation." >> $(DIST_DIR)/deb/arm64/DEBIAN/control
	@echo "#!/bin/bash" > $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "set -e" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "# Create system user if it doesn't exist" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "if ! id -u clickhouse_watcher >/dev/null 2>&1; then" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "    useradd --system --home-dir /var/lib/clickhouse-watcher --shell /usr/sbin/nologin clickhouse_watcher" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "fi" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "# Create group if it doesn't exist" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "if ! getent group clickhouse_watcherd >/dev/null 2>&1; then" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "    groupadd --system clickhouse_watcherd" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "fi" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "# Set permissions" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "chown -R clickhouse_watcher:clickhouse_watcherd /var/lib/clickhouse-watcher" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "chmod 750 /var/lib/clickhouse-watcher" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "# Reload systemd" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "systemctl daemon-reload" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@echo "systemd-tmpfiles --create" >> $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@chmod 755 $(DIST_DIR)/deb/arm64/DEBIAN/postinst
	@mkdir -p $(DIST_DIR)/archives
	@dpkg-deb --build $(DIST_DIR)/deb/arm64 $(DIST_DIR)/archives/clickhouse-watcher_$(DEB_VERSION)_$(DEB_ARCH_ARM64).deb
	@echo "Created: $(DIST_DIR)/archives/clickhouse-watcher_$(DEB_VERSION)_$(DEB_ARCH_ARM64).deb"

build-linux-amd64: .cache-go
	@echo "Building for linux/amd64..."
	@mkdir -p $(DIST_DIR)/linux/amd64
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)/linux/amd64/$(BINARY_DAEMON) $(GOFLAGS) ./cmd/daemon
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)/linux/amd64/$(BINARY_CLIENT) $(GOFLAGS) ./cmd/client

build-linux-arm64: .cache-go
	@echo "Building for linux/arm64..."
	@mkdir -p $(DIST_DIR)/linux/arm64
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(DIST_DIR)/linux/arm64/$(BINARY_DAEMON) $(GOFLAGS) ./cmd/daemon
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(DIST_DIR)/linux/arm64/$(BINARY_CLIENT) $(GOFLAGS) ./cmd/client

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
