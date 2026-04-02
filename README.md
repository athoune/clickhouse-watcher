# ClickHouse Watcher

A TUI application for monitoring ClickHouse databases with metrics persistence using RRD-style circular buffers.

## Architecture

```
┌──────────────────┐     ┌─────────────────────┐
│ clickhouse-watch │────▶│ clickhouse-watcherd │
│    (TUI Client)  │     │     (Daemon)        │
└──────────────────┘     └────────┬────────────┘
                                  │
                         REST API (HTTP over Unix Socket)
                                  │
                         ┌────────┴─────────┐
                         │                  │
                   ┌─────▼──────┐    ┌──────▼──────────┐
                   │ ClickHouse │    │ RRD Persistence │
                   └────────────┘    └─────────────────┘
```

- **clickhouse-watcherd**: Daemon that connects to ClickHouse, polls metrics, serves REST API
- **clickhouse-watch**: TUI client that connects to the daemon

## Quick Start

### Prerequisites

- Go 1.21+
- ClickHouse server (or use Docker Compose)

### Build

```bash
make build
```

This creates two binaries in the `build/` directory:
- `clickhouse-watcherd` - the daemon
- `clickhouse-watch` - the TUI client

### Run

1. Start ClickHouse:

```bash
make docker-up
# Wait for it to be ready
sleep 15
```

2. Configure your connection in `config.yaml`:

```yaml
connections:
  local:
    name: "Local ClickHouse"
    host: "localhost"
    port: 9000
    database: "default"
    username: "default"
    password: ""
```

3. Start the daemon:

```bash
./build/clickhouse-watcherd
```

4. In another terminal, start the client:

```bash
./build/clickhouse-watch
```

## Makefile Commands

| Command | Description |
|--------|-------------|
| `make build` | Build both binaries |
| `make build-daemon` | Build daemon only |
| `make build-client` | Build client only |
| `make test` | Run all tests |
| `make test-unit` | Run unit tests (RRD) |
| `make test-integration` | Run integration tests |
| `make docker-up` | Start ClickHouse container |
| `make docker-down` | Stop ClickHouse container |
| `make docker-test` | Run integration tests with Docker |
| `make install` | Install binaries to /usr/local/bin |
| `make clean` | Clean build artifacts |
| `make lint` | Run linter |
| `make dev` | tidy + test + lint + build |

## Usage

### Navigation

| Key | Action |
|-----|--------|
| `Tab` | Cycle through views |
| `↑/↓` | Navigate tables list |
| `Enter` | Select/Open |
| `Esc` | Back/Cancel |
| `Ctrl+C` | Quit |

### Views

#### Dashboard
- System metrics overview
- Press `h` to jump to History view

#### Tables
- List tables from `system.parts` with size and date ranges
- Press `Enter` to view table details

#### Table Details
- View engine, sorting key
- `t` - Truncate table
- `l` - Modify TTL

#### Processes
- Running queries monitoring

#### History
- Historical metrics (total_bytes, total_rows, uptime)
- `←/→` - Change period (day/week/month)

## Configuration

Configuration file: `config.yaml` (searched in current dir and `~/.config/clickhouse-watcher/`)

## Data Persistence

Metrics are persisted using RRD-style circular buffers:

- **Day**: 720 slots × 2 min = 24 hours
- **Week**: 672 slots × 15 min = 7 days
- **Month**: 744 slots × 1 hour = 31 days

Data stored in: `~/.local/share/clickhouse-watcher/`

## Project Structure

```
clickhouse-watcher/
├── Makefile
├── README.md
├── go.mod / go.sum
├── cmd/
│   ├── daemon/main.go     # Daemon entry point
│   └── client/main.go     # TUI client entry point
├── config/
│   └── config.go
├── daemon/
│   ├── state.go
│   └── server.go
├── client/
│   └── client.go
├── internal/
│   └── clickhouse/
│       └── client.go
├── rrd/
│   ├── rrd.go
│   ├── rrd_persist.go
│   ├── scheduler.go
│   └── rrd_test.go
├── ui/
│   ├── model.go
│   └── styles/
│       └── styles.go
├── tests/
│   └── integration_test.go
├── docker-compose.yaml
└── config.yaml
```

## License

MIT
