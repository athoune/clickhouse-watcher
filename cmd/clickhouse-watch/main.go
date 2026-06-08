package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/config"
	"github.com/athoune/clickhouse-watcher/daemon"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/internal/transport"
	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/ui"

	tea "github.com/charmbracelet/bubbletea"
)

const pollInterval = 5 * time.Second

type mode int

const (
	modeDaemon mode = iota
	modeClient
	modeStandalone
	modeUnknown
)

// resolveMode determines the run mode from os.Args[0] (binary name) and
// optional subcommand in os.Args[1].
//
// Supported invocations:
//   clickhouse-watch-serve  [...]     → daemon
//   clickhouse-watch-client [...]     → client
//   clickhouse-watch serve  [...]     → daemon
//   clickhouse-watch client [...]     → client
//   clickhouse-watch                  → standalone (daemon + client)
func resolveMode() mode {
	basename := filepath.Base(os.Args[0])

	// ARGV[0]-based detection (symlinks)
	if strings.HasPrefix(basename, "clickhouse-watch-") {
		suffix := basename[len("clickhouse-watch-"):]
		switch suffix {
		case "serve", "d":
			return modeDaemon
		case "client":
			return modeClient
		}
	}

	// Subcommand-based detection
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			return modeDaemon
		case "client":
			return modeClient
		}
		// Unknown subcommand
		return modeUnknown
	}

	// No args, no matching basename → standalone
	return modeStandalone
}

func printUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s [serve|client]\n", name)
	fmt.Fprintf(os.Stderr, "\nModes:\n")
	fmt.Fprintf(os.Stderr, "  serve    Start the daemon server\n")
	fmt.Fprintf(os.Stderr, "  client   Start the TUI client\n")
	fmt.Fprintf(os.Stderr, "  (none)   Standalone mode (daemon + client in one process)\n")
	fmt.Fprintf(os.Stderr, "\nAlso works via symlink names:\n")
	fmt.Fprintf(os.Stderr, "  clickhouse-watch-serve  → daemon mode\n")
	fmt.Fprintf(os.Stderr, "  clickhouse-watch-client → client mode\n")
}

func main() {
	m := resolveMode()
	switch m {
	case modeDaemon:
		var args []string
		if len(os.Args) > 1 && os.Args[1] == "serve" {
			args = os.Args[2:]
		}
		runDaemon(args)
	case modeClient:
		runClient()
	case modeStandalone:
		runStandalone()
	default:
		printUsage()
		os.Exit(1)
	}
}

func getDefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "clickhouse-watcher")
}

func initConfigAndLogger(configPath string) (*config.Config, error) {
	var cfg *config.Config
	var err error
	if configPath != "" {
		cfg, err = config.LoadFrom(configPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		return nil, err
	}

	logCfg := cfg.GetLogConfig()
	logger.InitWithConfig(logger.Config{
		Level:  logCfg.Level,
		Path:   logCfg.Path,
		Pretty: logCfg.Pretty,
	})
	return cfg, nil
}

// initClientLogger configures logging for the TUI client.
// Only ERROR+ goes to stderr so the terminal UI is not corrupted.
func initClientLogger(cfg *config.Config) {
	logCfg := cfg.GetLogConfig()
	if logCfg.Path != "" {
		level := logCfg.Level
		if level == "" {
			level = "info"
		}
		logger.InitWithConfig(logger.Config{
			Level:  level,
			Path:   logCfg.Path,
			Pretty: logCfg.Pretty,
		})
	} else {
		logger.InitWithConfig(logger.Config{
			Level:  "error",
			Path:   "stderr",
			Pretty: false,
		})
	}
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file (default: search in current dir and ~/.config/)")
	socketPath := fs.String("socket", "/tmp/clickhouse-watcher.sock", "Path to Unix socket")
	dataDir := fs.String("data", "", "Path to data directory (default: ~/.local/share/clickhouse-watcher)")
	fs.Parse(args)

	cfg, err := initConfigAndLogger(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.WithComponent("daemon-main")
	log.Info().Msg("Starting clickhouse-watcher daemon")

	dataDirPath := *dataDir
	if dataDirPath == "" {
		dataDirPath = getDefaultDataDir()
	}

	var conn clickhouse.Connection
	if cfgConn := cfg.GetFirstConnection(); cfgConn != nil {
		conn = clickhouse.Connection{
			Name:     cfgConn.Name,
			Host:     cfgConn.Host,
			Port:     cfgConn.Port,
			Database: cfgConn.Database,
			Username: cfgConn.Username,
			Password: cfgConn.Password,
		}
		log.Info().
			Str("name", conn.Name).
			Str("host", conn.Host).
			Int("port", conn.Port).
			Str("database", conn.Database).
			Msg("Using configured connection")
		fmt.Printf("Connecting to %s (%s:%d)...\n", conn.Name, conn.Host, conn.Port)
	} else {
		log.Error().Msg("No connection configured in config.yaml")
		fmt.Fprintf(os.Stderr, "No connection configured in config.yaml\n")
		os.Exit(1)
	}

	if dataDirPath != "" {
		if err = os.MkdirAll(dataDirPath, 0755); err != nil {
			log.Error().Err(err).Str("data_dir", dataDirPath).Msg("Failed to create data directory")
		} else {
			log.Info().Str("data_dir", dataDirPath).Msg("Data directory ready")
			fmt.Printf("Data directory: %s\n", dataDirPath)
		}
	}

	state := daemon.NewState(conn, dataDirPath)
	if err := state.Connect(); err != nil {
		log.Error().Err(err).Msg("Failed to connect to ClickHouse")
		fmt.Fprintf(os.Stderr, "Failed to connect to ClickHouse: %v\n", err)
		os.Exit(1)
	}
	log.Info().Msg("Connected to ClickHouse")
	fmt.Println("Connected to ClickHouse")

	server := daemon.NewServer(state, *socketPath, pollInterval)
	if err := server.Start(); err != nil {
		log.Error().Err(err).Msg("Failed to start daemon server")
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		state.Close()
		os.Exit(1)
	}
	log.Info().Str("socket", *socketPath).Msg("Daemon server started")
	fmt.Printf("Daemon listening on %s\n", *socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state.StartRRD(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Info().Msg("Daemon running. Press Ctrl+C to stop.")
	<-sigCh

	log.Info().Msg("Shutting down daemon...")
	fmt.Println("\nShutting down...")
	cancel()
	server.Stop()
	state.Close()
	os.Remove(*socketPath)
	log.Info().Msg("Daemon stopped")
	fmt.Println("Daemon stopped")
}

func runClient() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	initClientLogger(cfg)

	log := logger.WithComponent("client-main")
	log.Info().Str("socket", client.DefaultPath()).Msg("Starting clickhouse-watch client")

	m := ui.New(client.DefaultPath())
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		log.Error().Err(err).Msg("Error running TUI program")
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}

	log.Info().Msg("Client exited normally")
}

func runStandalone() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	initClientLogger(cfg)

	log := logger.WithComponent("standalone-main")
	log.Info().Msg("Starting clickhouse-watcher in standalone mode")

	dataDirPath := getDefaultDataDir()

	var conn clickhouse.Connection
	if cfgConn := cfg.GetFirstConnection(); cfgConn != nil {
		conn = clickhouse.Connection{
			Name:     cfgConn.Name,
			Host:     cfgConn.Host,
			Port:     cfgConn.Port,
			Database: cfgConn.Database,
			Username: cfgConn.Username,
			Password: cfgConn.Password,
		}
		log.Info().
			Str("name", conn.Name).
			Str("host", conn.Host).
			Int("port", conn.Port).
			Str("database", conn.Database).
			Msg("Using configured connection")
		fmt.Printf("Connecting to %s (%s:%d)...\n", conn.Name, conn.Host, conn.Port)
	} else {
		log.Error().Msg("No connection configured in config.yaml")
		fmt.Fprintf(os.Stderr, "No connection configured in config.yaml\n")
		os.Exit(1)
	}

	if dataDirPath != "" {
		if err = os.MkdirAll(dataDirPath, 0755); err != nil {
			log.Error().Err(err).Str("data_dir", dataDirPath).Msg("Failed to create data directory")
		} else {
			log.Info().Str("data_dir", dataDirPath).Msg("Data directory ready")
			fmt.Printf("Data directory: %s\n", dataDirPath)
		}
	}

	state := daemon.NewState(conn, dataDirPath)
	if err := state.Connect(); err != nil {
		log.Error().Err(err).Msg("Failed to connect to ClickHouse")
		fmt.Fprintf(os.Stderr, "Failed to connect to ClickHouse: %v\n", err)
		os.Exit(1)
	}
	log.Info().Msg("Connected to ClickHouse")
	fmt.Println("Connected to ClickHouse")

	// Start the HTTP handler and poll loop, but without a Unix socket.
	server := daemon.NewServer(state, "", pollInterval)
	server.StartPolling()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state.StartRRD(ctx)

	// Give the daemon a moment to complete its first poll so the UI
	// has data to display immediately.
	time.Sleep(200 * time.Millisecond)

	// Create an in-memory transport that routes HTTP requests directly
	// to the daemon's handler — no socket, no network I/O.
	memTransport := &transport.MemoryTransport{Handler: server}
	c := client.NewClientWithTransport(memTransport)

	log.Info().Msg("Starting TUI client")
	m := ui.NewWithClient(c)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		log.Error().Err(err).Msg("Error running TUI program")
		fmt.Println("Error running program:", err)
		cancel()
		server.Stop()
		state.Close()
		os.Exit(1)
	}

	log.Info().Msg("Shutting down standalone mode...")
	cancel()
	server.Stop()
	state.Close()
	log.Info().Msg("Standalone mode stopped")
}
