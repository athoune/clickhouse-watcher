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
	cfg, err := config.Load()
	if err != nil {
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
		fmt.Printf("Connecting to %s (%s:%d)...\n", conn.Name, conn.Host, conn.Port)
	} else {
		fmt.Fprintf(os.Stderr, "No connection configured in config.yaml\n")
		os.Exit(1)
	}

	dataDir := getDataDir()
	if dataDir != "" {
		os.MkdirAll(dataDir, 0755)
		fmt.Printf("Data directory: %s\n", dataDir)
	}

	state := daemon.NewState(conn, dataDir)
	if err := state.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to ClickHouse: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connected to ClickHouse")

	server := daemon.NewServer(state, socketPath, pollInterval)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		state.Close()
		os.Exit(1)
	}
	fmt.Printf("Daemon listening on %s\n", socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state.StartRRD(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh

	fmt.Println("\nShutting down...")
	cancel()
	server.Stop()
	state.Close()
	os.Remove(socketPath)
	fmt.Println("Daemon stopped")
}
