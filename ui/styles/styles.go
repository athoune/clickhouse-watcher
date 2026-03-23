package styles

import "github.com/charmbracelet/lipgloss"

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA"))

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	HighlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D9FF"))

	ValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF88"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555"))

	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444"))

	SelectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1A1A2E"))

	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00D9FF"))

	TableCellStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CCCCCC"))

	StatusConnected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF88"))

	StatusDisconnected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF5555"))

	AsciiStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D9FF")).
			Bold(true)

	FooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))
)
