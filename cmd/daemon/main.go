package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/athoune/clickhouse-watcher/config"
	"github.com/athoune/clickhouse-watcher/daemon"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/logger"
)

const pollInterval = 5 * time.Second

var (
	configPath = flag.String("config", "", "Path to configuration file")
	socketPath = flag.String("socket", "/var/run/clickhouse-watcher/clickhouse-watcher.sock", "Path to Unix socket")
	dataDir    = flag.String("data", "/var/lib/clickhouse-watcher", "Path to data directory")
)

func main() {
	flag.Parse()

	// Load configuration first
	var cfg *config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.LoadFrom(*configPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger with configuration
	logCfg := cfg.GetLogConfig()
	logger.InitWithConfig(logger.Config{
		Level:  logCfg.Level,
		Path:   logCfg.Path,
		Pretty: logCfg.Pretty,
	})
	log := logger.WithComponent("daemon-main")

	log.Info().Msg("Starting clickhouse-watcher daemon")

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

	dataDirPath := *dataDir
	if dataDirPath != "" {
		if err := os.MkdirAll(dataDirPath, 0755); err != nil {
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
