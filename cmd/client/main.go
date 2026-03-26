package main

import (
	"fmt"
	"os"

	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/ui"
	tea "github.com/charmbracelet/bubbletea"
)

const socketPath = "/tmp/clickhouse-watcher.sock"

func main() {
	// Initialize logger
	logger.Init()
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
