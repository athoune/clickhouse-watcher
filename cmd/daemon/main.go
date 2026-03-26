package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/athoune/clickhouse-watcher/config"
	"github.com/athoune/clickhouse-watcher/daemon"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/logger"
)

const (
	socketPath   = "/tmp/clickhouse-watcher.sock"
	pollInterval = 5 * time.Second
)

func getDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "clickhouse-watcher")
}

func main() {
	// Initialize logger first
	logger.Init()
	log := logger.WithComponent("daemon-main")

	log.Info().Msg("Starting clickhouse-watcher daemon")

	cfg, err := config.Load()
	if err != nil {
		log.Error().Err(err).Msg("Failed to load config")
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
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

	dataDir := getDataDir()
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Error().Err(err).Str("data_dir", dataDir).Msg("Failed to create data directory")
		} else {
			log.Info().Str("data_dir", dataDir).Msg("Data directory ready")
			fmt.Printf("Data directory: %s\n", dataDir)
		}
	}

	state := daemon.NewState(conn, dataDir)
	if err := state.Connect(); err != nil {
		log.Error().Err(err).Msg("Failed to connect to ClickHouse")
		fmt.Fprintf(os.Stderr, "Failed to connect to ClickHouse: %v\n", err)
		os.Exit(1)
	}
	log.Info().Msg("Connected to ClickHouse")
	fmt.Println("Connected to ClickHouse")

	server := daemon.NewServer(state, socketPath, pollInterval)
	if err := server.Start(); err != nil {
		log.Error().Err(err).Msg("Failed to start daemon server")
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		state.Close()
		os.Exit(1)
	}
	log.Info().Str("socket", socketPath).Msg("Daemon server started")
	fmt.Printf("Daemon listening on %s\n", socketPath)

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
	os.Remove(socketPath)
	log.Info().Msg("Daemon stopped")
	fmt.Println("Daemon stopped")
}
