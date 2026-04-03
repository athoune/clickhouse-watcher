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

### Quick Start (Default Behavior)

By default, the application works without any system configuration:

```bash
# 1. Build
make build

# 2. Create a config.yaml in the current directory (optional)
cat > config.yaml << 'EOF'
connections:
  local:
    name: "Local ClickHouse"
    host: "localhost"
    port: 9000
    database: "default"
    username: "default"
    password: ""
EOF

# 3. Start the daemon (uses /tmp/clickhouse-watcher.sock and ~/.local/share/clickhouse-watcher/)
./build/clickhouse-watcherd

# 4. In another terminal, start the client
./build/clickhouse-watch
```

**Default paths:**
- Config: searches in current directory, then `~/.config/clickhouse-watcher/`
- Data: `~/.local/share/clickhouse-watcher/`
- Socket: `/tmp/clickhouse-watcher.sock`

### Production Installation (systemd)

For production deployment with systemd, follow these steps:

#### 1. Create System User and Group

```bash
# Create system user for the daemon
sudo useradd --system --home-dir /var/lib/clickhouse-watcher \
  --shell /usr/sbin/nologin clickhouse_watcher

# Create group for authorized clients
sudo groupadd --system clickhouse_watcherd

# Add your user to the group to allow client connections
sudo usermod -aG clickhouse_watcherd $USER
# Log out and log back in for group changes to take effect
```

#### 2. Install Binaries

```bash
# Build the project
make build

# Install binaries to system location
sudo cp build/clickhouse-watcherd /usr/local/bin/
sudo cp build/clickhouse-watch /usr/local/bin/
sudo chmod 755 /usr/local/bin/clickhouse-watcherd /usr/local/bin/clickhouse-watch

# Set ownership for daemon binary
sudo chown root:root /usr/local/bin/clickhouse-watcherd
```

#### 3. Create Directories

```bash
# Create configuration directory
sudo mkdir -p /etc/clickhouse-watcher
sudo chmod 755 /etc/clickhouse-watcher

# Create data directory for RRD storage
sudo mkdir -p /var/lib/clickhouse-watcher
sudo chown clickhouse_watcher:clickhouse_watcherd /var/lib/clickhouse-watcher
sudo chmod 750 /var/lib/clickhouse-watcher

# Create runtime directory (will be managed by systemd)
sudo mkdir -p /run/clickhouse-watcher
sudo chown clickhouse_watcher:clickhouse_watcherd /run/clickhouse-watcher
sudo chmod 2775 /run/clickhouse-watcher
```

#### 4. Create ClickHouse User

Connect to ClickHouse as admin and execute:

```sql
CREATE USER IF NOT EXISTS the_watcher IDENTIFIED BY 'secure_password';
GRANT SELECT ON system.* TO the_watcher;
GRANT SELECT ON *.* TO the_watcher;
GRANT SHOW DATABASES ON *.* TO the_watcher;
```

#### 5. Configure

Create `/etc/clickhouse-watcher/config.yaml`:

```yaml
connections:
  local:
    name: "Local ClickHouse"
    host: "localhost"
    port: 9000
    database: "default"
    username: "the_watcher"
    password: "secure_password"

log:
  level: "info"
  path: "/var/log/clickhouse-watcher/daemon.log"
  pretty: false
```

Set permissions:
```bash
sudo chown root:clickhouse_watcherd /etc/clickhouse-watcher/config.yaml
sudo chmod 640 /etc/clickhouse-watcher/config.yaml
```

#### 6. Install systemd Service

```bash
# Copy service file
sudo cp systemd/clickhouse-watcherd.service /etc/systemd/system/
sudo cp systemd/clickhouse-watcherd.conf /etc/tmpfiles.d/

# Reload systemd
sudo systemctl daemon-reload
sudo systemd-tmpfiles --create

# Enable and start service
sudo systemctl enable clickhouse-watcherd
sudo systemctl start clickhouse-watcherd

# Check status
sudo systemctl status clickhouse-watcherd
```

#### 7. Run Client

After installing and starting the daemon, any user in the `clickhouse_watcherd` group can connect:

```bash
clickhouse-watch
```

Note: The systemd service uses flags to override default paths:
```
clickhouse-watcherd -config /etc/clickhouse-watcher/config.yaml \
                    -socket /run/clickhouse-watcher/clickhouse-watcher.sock \
                    -data /var/lib/clickhouse-watcher
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
