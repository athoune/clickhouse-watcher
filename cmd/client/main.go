package main

import (
	"fmt"
	"os"

	"github.com/athoune/clickhouse-watcher/config"
	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/ui"
	tea "github.com/charmbracelet/bubbletea"
)

const socketPath = "/tmp/clickhouse-watcher.sock"

func main() {
	// Load configuration first
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger with configuration
	// For TUI client, default to no logging to avoid interfering with
	// the terminal interface. Logs can be enabled by setting log.path in config
	logCfg := cfg.GetLogConfig()

	// If no explicit log configuration, disable logging completely for TUI
	if logCfg.Path == "" && logCfg.Level == "" {
		// Default: discard all logs for TUI to prevent screen corruption
		logger.InitWithConfig(logger.Config{
			Level:  "disabled",
			Path:   "discard",
			Pretty: false,
		})
	} else {
		logger.InitWithConfig(logger.Config{
			Level:  logCfg.Level,
			Path:   logCfg.Path,
			Pretty: logCfg.Pretty,
		})
	}

	log := logger.WithComponent("client-main")
	log.Info().Str("socket", socketPath).Msg("Starting clickhouse-watch client")

	m := ui.New(socketPath)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		log.Error().Err(err).Msg("Error running TUI program")
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}

	log.Info().Msg("Client exited normally")
}
