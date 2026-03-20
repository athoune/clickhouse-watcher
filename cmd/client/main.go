package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/athoune/clickhouse-watcher/ui"
)

const socketPath = "/tmp/clickhouse-watcher.sock"

func main() {
	m := ui.New(socketPath)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
