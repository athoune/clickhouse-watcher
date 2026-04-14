package main

import (
	"fmt"
	"os"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/config"
	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Load configuration first
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger with configuration
	// Default behavior for TUI client:
	// - Only ERROR level logs go to stderr (for important errors)
	// - INFO logs are optional and go to a file if log.path is configured
	logCfg := cfg.GetLogConfig()

	if logCfg.Path != "" {
		// If log file is configured, write INFO+ logs to file, nothing to stderr
		level := logCfg.Level
		if level == "" {
			level = "info" // Default to INFO if path is set but level isn't
		}
		logger.InitWithConfig(logger.Config{
			Level:  level,
			Path:   logCfg.Path,
			Pretty: logCfg.Pretty,
		})
	} else {
		// No log file configured: only ERROR+ goes to stderr
		logger.InitWithConfig(logger.Config{
			Level:  "error",
			Path:   "stderr",
			Pretty: false,
		})
	}

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
