# ClickHouse Watcher - Agent Guide

## Project Overview

A Go-based TUI application for monitoring ClickHouse databases with RRD-style circular buffer persistence.

**Architecture:**
- Single `clickhouse-watch` binary with three modes:
  - `clickhouse-watch serve` — Daemon that connects to ClickHouse, polls metrics, serves REST API over Unix socket
  - `clickhouse-watch client` — TUI client that connects to the daemon via Unix socket
  - `clickhouse-watch` (no args) — Standalone mode: daemon + client in one process (no socket, in-memory HTTP)
- Symlink aliases: `clickhouse-watch-serve` → serve, `clickhouse-watch-client` → client

## Quick Commands

```bash
# Build single binary
make build

# Development workflow (tidy + test + lint + build)
make dev

# Run tests
make test                    # All tests
make test-unit              # Unit tests only (RRD, daemon, client, ui)
make test-integration       # Integration tests (requires Docker)

# Lint
make lint                   # Runs go vet

# Docker for integration tests
make docker-up              # Start ClickHouse container
make docker-down            # Stop container
make docker-test            # Run integration tests with Docker

# Install locally
make install                # Copies to /usr/local/bin + creates symlinks
```

## Project Structure

```
cmd/
  clickhouse-watch/main.go # Unified entry point (serve/client/standalone)
daemon/
  server.go                # HTTP server over Unix socket
  state.go                 # ClickHouse connection & polling
client/
  client.go                # Unix socket client (also in-memory for standalone)
ui/
  model.go                 # Bubble Tea TUI model
  styles/styles.go         # Lipgloss styles
internal/transport/
  memory.go                # In-memory HTTP transport for standalone mode
rrd/
  rrd.go                   # Circular buffer implementation
  rrd_persist.go           # Persistence layer
  scheduler.go             # Polling scheduler
internal/clickhouse/
  client.go                # ClickHouse connection wrapper
config/
  config.go                # Viper-based config loading
tests/
  integration_test.go      # Integration tests
  client_server_test.go    # Client-server tests
```

## Configuration

**Config search order:**
1. Current directory: `config.yaml`
2. User config: `~/.config/clickhouse-watcher/config.yaml`

**Default paths:**
- Config: current dir or `~/.config/clickhouse-watcher/`
- Data: `~/.local/share/clickhouse-watcher/`
- Socket: `/tmp/clickhouse-watcher.sock`

**Production paths (systemd):**
- Config: `/etc/clickhouse-watcher/config.yaml`
- Data: `/var/lib/clickhouse-watcher/`
- Socket: `/run/clickhouse-watcher/clickhouse-watcher.sock`

## Testing

**Unit tests:** No external dependencies
```bash
make test-unit
```

**Integration tests:** Requires Docker
```bash
make docker-test    # Starts ClickHouse, runs tests, stops container
```

Test ClickHouse available at:
- HTTP: `localhost:8124`
- Native: `localhost:9001`
- User: `test` / Password: `test123`

## Key Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/ClickHouse/clickhouse-go/v2` - ClickHouse driver
- `github.com/spf13/viper` - Configuration management
- `github.com/rs/zerolog` - Structured logging

## Build & Release

**Cross-compilation:**
```bash
make dist           # Builds for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
```

**Debian packages:**
```bash
make deb            # Creates .deb for amd64 and arm64
make deb-from-docker # Build in Docker (for reproducibility)
```

**Release workflow:**
- Pushing tags `v*` triggers GitHub Actions
- Builds all platforms + Debian packages
- Creates GitHub release with artifacts

## Development Notes

**RRD Data Retention:**
- Day: 720 slots × 2 min = 24 hours
- Week: 672 slots × 15 min = 7 days
- Month: 744 slots × 1 hour = 31 days

**Daemon flags:**
```bash
clickhouse-watch serve -config /path/to/config.yaml \
                       -socket /path/to/socket.sock \
                       -data /path/to/data
```

**Client behavior:**
- Logs ERROR+ to stderr by default
- If `log.path` configured, writes INFO+ to file
- Uses Bubble Tea's alt-screen mode

**Standalone mode:**
- No Unix socket is created; all communication happens in-process via `internal/transport.MemoryTransport`
- The daemon's HTTP handler is called directly by the client — zero network I/O
- RRD polling and the TUI run in the same process; shutting down the TUI stops the daemon

## Common Tasks

**Add new API endpoint:**
1. Add handler in `daemon/server.go` (implement `http.Handler`)
2. Add corresponding client method in `client/client.go`
3. Add UI view in `ui/model.go`

**Modify RRD:**
- Edit `rrd/rrd.go` for buffer logic
- Edit `rrd/rrd_persist.go` for storage
- Run `make test-unit` to validate

**Add ClickHouse query:**
- Add query method in `internal/clickhouse/client.go`
- Call from `daemon/state.go` poll loop
- Expose via `daemon/server.go` endpoint

## GitHub Integration

**OpenCode:** Comment `/oc` or `/opencode` on issues/PRs to trigger agent

**Release:** Push version tags (`v*`) to trigger release workflow
