package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg struct{}

type Model struct {
	tab           int
	daemon        *client.Client
	err           error
	metrics       *clickhouse.SystemMetrics
	tables        []clickhouse.TableMetric
	truncatables  []clickhouse.TruncatableTable
	queries       []clickhouse.QueryMetric
	queryInput    string
	results       [][]string
	headers       []string
	loading       bool
	width         int
	height        int
	selectedIdx   int
	tableDetail   *clickhouse.TableDetail
	ttlInput      string
	actionMsg     string
	historyData   []rrd.Sample
	historyPeriod string
	historyMetric string
	fatTable      table.Model
}

const (
	tabDashboard = 0
	tabTables    = 1
	tabFatTables = 2
	tabProcesses = 3
	tabHistory   = 4
)

var tabNames = []string{"Dashboard", "Tables", "Fat Tables", "Processes", "History"}

var (
	helpBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Background(lipgloss.Color("#1E1E1E")).
			Width(120)

	tabBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1E1E1E")).
			Width(120)

	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#0078D4")).
			Padding(0, 1).
			Margin(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#666666")).
				Padding(0, 1).
				Margin(0, 1)

	sectionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B"))

	contentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CCCCCC"))
)

func New(socketPath string) *Model {
	return &Model{
		tab:    tabDashboard,
		daemon: client.NewClient(socketPath),
	}
}

func (m *Model) Init() tea.Cmd {
	return m.connect()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.fatTable.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		if m.tab == tabFatTables {
			teaModel, cmd := m.fatTable.Update(msg)
			m.fatTable = teaModel
			if cmd != nil {
				return m, cmd
			}
			if msg.Type == tea.KeyEnter {
				return m, m.handleFatTableSelect()
			}
			return m, nil
		}
		_, cmd := m.handleKey(msg)
		return m, cmd

	case tickMsg:
		return m, nil

	default:
		return m, nil
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
		return m, tea.Quit
	}

	switch msg.Type {
	case tea.KeyTab:
		m.nextTab()

	case tea.KeyUp:
		if m.tab == tabTables && m.selectedIdx > 0 {
			m.selectedIdx--
		}
		if m.tab == tabHistory {
			m.selectedIdx = 0
		}

	case tea.KeyDown:
		if m.tab == tabTables && m.selectedIdx < len(m.tables)-1 {
			m.selectedIdx++
		}
		if m.tab == tabHistory {
			m.selectedIdx = 1
		}

	case tea.KeyLeft:
		if m.tab == tabHistory {
			m.cycleHistoryPeriod(-1)
			return m, m.loadHistory()
		}

	case tea.KeyRight:
		if m.tab == tabHistory {
			m.cycleHistoryPeriod(1)
			return m, m.loadHistory()
		}

	case tea.KeyEnter:
		if m.tab == tabTables {
			return m, m.showTableDetail()
		}

	case tea.KeyBackspace:
		if m.tab == tabDashboard && m.tableDetail != nil && len(m.ttlInput) > 0 {
			m.ttlInput = m.ttlInput[:len(m.ttlInput)-1]
		}

	case tea.KeyRunes:
		switch msg.String() {
		case "r":
			return m, m.refresh()
		case "t":
			if m.tab == tabDashboard && m.tableDetail != nil {
				return m, m.truncateTable()
			}
		case "l":
			if m.tab == tabDashboard && m.tableDetail != nil {
				return m, m.modifyTTL()
			}
		case "z":
			if m.tab == tabDashboard {
				m.tableDetail = nil
				m.ttlInput = ""
				m.actionMsg = ""
			}
		}
	}

	return m, nil
}

func (m *Model) nextTab() {
	m.tab = (m.tab + 1) % len(tabNames)
	m.selectedIdx = 0
}

func (m *Model) cycleHistoryPeriod(dir int) {
	periods := []string{"day", "week", "month"}
	for i, p := range periods {
		if p == m.historyPeriod {
			m.historyPeriod = periods[(i+dir+len(periods))%len(periods)]
			return
		}
	}
}

func (m *Model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ctx

		samples, err := m.daemon.GetHistory(m.historyMetric, m.historyPeriod)
		if err != nil {
			m.err = fmt.Errorf("failed to load history: %v", err)
			return nil
		}

		m.historyData = samples
		return nil
	}
}

func (m *Model) connect() tea.Cmd {
	return func() tea.Msg {
		m.loading = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		connected, err := m.daemon.IsConnected(ctx)
		if err != nil || !connected {
			m.err = fmt.Errorf("daemon not available")
			m.loading = false
			return nil
		}

		metrics, err := m.daemon.GetMetrics(ctx)
		if err != nil {
			m.err = fmt.Errorf("failed to get metrics: %v", err)
			m.loading = false
			return nil
		}
		m.metrics = metrics

		tables, err := m.daemon.GetTables(ctx)
		if err == nil {
			m.tables = tables
		}

		truncatables, err := m.daemon.GetTruncatableTables(ctx)
		if err == nil {
			m.truncatables = truncatables
			m.initFatTable()
		}

		queries, err := m.daemon.GetQueries(ctx)
		if err == nil {
			m.queries = queries
		}

		m.loading = false
		time.Sleep(500 * time.Millisecond)
		return tickMsg{}
	}
}

func (m *Model) initFatTable() {
	columns := []table.Column{
		{Title: "Database", Width: 20},
		{Title: "Table", Width: 25},
		{Title: "Size", Width: 15},
		{Title: "Rows", Width: 12},
		{Title: "Truncatable", Width: 12},
	}

	var rows []table.Row
	for _, t := range m.truncatables {
		truncatable := "No"
		if t.Truncatable {
			truncatable = "Yes"
		}
		rows = append(rows, table.Row{
			t.Database,
			t.Table,
			formatBytes(t.Size),
			fmt.Sprintf("%d", t.Rows),
			truncatable,
		})
	}

	m.fatTable = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(20),
	)
}

func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ctx

		switch m.tab {
		case tabDashboard:
			metrics, err := m.daemon.GetMetrics(ctx)
			if err == nil {
				m.metrics = metrics
			}
		case tabTables, tabFatTables:
			tables, err := m.daemon.GetTables(ctx)
			if err == nil {
				m.tables = tables
			}
			truncatables, err := m.daemon.GetTruncatableTables(ctx)
			if err == nil {
				m.truncatables = truncatables
				m.initFatTable()
			}
		case tabProcesses:
			queries, err := m.daemon.GetQueries(ctx)
			if err == nil {
				m.queries = queries
			}
		case tabHistory:
			return m.loadHistory()
		}

		return nil
	}
}

func (m *Model) showTableDetail() tea.Cmd {
	return func() tea.Msg {
		if len(m.tables) == 0 || m.selectedIdx >= len(m.tables) {
			return nil
		}

		t := m.tables[m.selectedIdx]
		m.tableDetail = &clickhouse.TableDetail{
			Database: t.Database,
			Name:     t.Name,
		}
		m.ttlInput = ""
		return nil
	}
}

func (m *Model) handleFatTableSelect() tea.Cmd {
	return func() tea.Msg {
		row := m.fatTable.Cursor()
		if row < len(m.truncatables) {
			t := m.truncatables[row]
			m.tableDetail = &clickhouse.TableDetail{
				Database: t.Database,
				Name:     t.Table,
			}
			m.ttlInput = ""
			m.tab = tabDashboard
		}
		return nil
	}
}

func (m *Model) truncateTable() tea.Cmd {
	return func() tea.Msg {
		m.actionMsg = "confirm"
		return nil
	}
}

func (m *Model) executeTruncate() tea.Cmd {
	return func() tea.Msg {
		if m.tableDetail == nil {
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := m.daemon.TruncateTable(ctx, m.tableDetail.Database, m.tableDetail.Name)
		if err != nil {
			m.err = fmt.Errorf("truncate failed: %v", err)
		}

		m.tableDetail = nil
		m.ttlInput = ""
		m.actionMsg = ""
		return m.refresh()
	}
}

func (m *Model) modifyTTL() tea.Cmd {
	return func() tea.Msg {
		if m.tableDetail == nil {
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := m.daemon.ModifyTTL(ctx, m.tableDetail.Database, m.tableDetail.Name, m.ttlInput)
		if err != nil {
			m.err = fmt.Errorf("TTL modify failed: %v", err)
		}

		return nil
	}
}

func (m *Model) View() string {
	var s string

	if m.loading || m.err != nil {
		return m.connectView()
	}

	s += m.renderTabBar()
	s += "\n"
	s += m.renderContent()
	s += m.renderHelp()

	return s
}

func (m *Model) renderTabBar() string {
	var s string
	for i, name := range tabNames {
		if i == m.tab {
			s += activeTabStyle.Render(name)
		} else {
			s += inactiveTabStyle.Render(name)
		}
	}
	return tabBarStyle.Render(s)
}

func (m *Model) renderHelp() string {
	switch m.tab {
	case tabDashboard:
		if m.tableDetail != nil {
			return helpBarStyle.Render(" [t] Truncate  [l] Apply TTL  [z] Back  [r] Refresh")
		}
		return helpBarStyle.Render(" [r] Refresh  [Tab] Next")
	case tabTables:
		return helpBarStyle.Render(" [↑/↓] Select  [Enter] Details  [r] Refresh")
	case tabFatTables:
		return helpBarStyle.Render(" [↑/↓] Select  [Enter] Table Details  [r] Refresh")
	case tabProcesses:
		return helpBarStyle.Render(" [r] Refresh  [Tab] Next")
	case tabHistory:
		return helpBarStyle.Render(" [↑/↓] Metric  [←/→] Period  [r] Refresh")
	default:
		return ""
	}
}

func (m *Model) renderContent() string {
	switch m.tab {
	case tabDashboard:
		return m.dashboardView()
	case tabTables:
		return m.tablesView()
	case tabFatTables:
		return m.fatTablesView()
	case tabProcesses:
		return m.processesView()
	case tabHistory:
		return m.historyView()
	default:
		return ""
	}
}

const asciiLogo = `
    __  __ __      __    __   ____  ______   __  __ __    ___  ____
   /  ]|  |  |    |  |__|  | /    ||      | /  ]|  |  | /  _]|    \
  /  / |  |  |    |  |  |  ||  o  ||      |/  / |  |  | /  [_ |  D  )
 /  /  |  |  |    |  |  |  ||     ||_|  |_/  /  |  _  ||    _]|    /
/   \_ |  |  |    |  '  '  ||  _  |  |  |/   \_ |  |  ||   [_ |    \
\     ||  |  |     \      / |  |  |  |  |\     ||  |  ||     ||  .  \
 \____||__|__|      \_/\_/  |__|__|  |__| \____||__|__||_____||__|\_|
`

func (m *Model) connectView() string {
	var s string

	bg := lipgloss.Color("#1A1A2E")
	fg := lipgloss.Color("#00D9FF")

	s += lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center).
		Render("")

	s += lipgloss.NewStyle().
		Foreground(fg).
		Bold(true).
		Align(lipgloss.Center).
		Width(m.width).
		Render(asciiLogo)
	s += "\n\n"

	if m.loading {
		s += contentStyle.Render("  Connecting to ")
		s += valueStyle.Render(m.daemon.SocketPath())
		s += "...\n"
	} else if m.err != nil {
		s += errorStyle.Render("  Connection failed: " + m.err.Error() + "\n")
		s += "\n"
		s += contentStyle.Render("  Press ESC to quit\n")
	}

	return s
}

func (m *Model) dashboardView() string {
	var s string

	if m.tableDetail != nil {
		s += sectionStyle.Render("\n  Table Details\n\n")
		s += contentStyle.Render(fmt.Sprintf("  %-15s %s\n", "Database:", m.tableDetail.Database))
		s += contentStyle.Render(fmt.Sprintf("  %-15s %s\n", "Name:", m.tableDetail.Name))
		s += contentStyle.Render(fmt.Sprintf("  %-15s %s\n", "Engine:", m.tableDetail.Engine))
		s += contentStyle.Render(fmt.Sprintf("  %-15s %s\n", "Sorting Key:", m.tableDetail.SortingKey))
		s += "\n"
		s += sectionStyle.Render("  TTL\n")
		s += "\n"
		s += valueStyle.Render("  > " + m.ttlInput + "\n")

		if m.err != nil {
			s += errorStyle.Render("\n  Error: " + m.err.Error() + "\n")
		}
		return s
	}

	s += sectionStyle.Render("\n  System Metrics\n\n")

	if m.metrics == nil {
		s += contentStyle.Render("  No metrics available\n")
		return s
	}

	metrics := []struct {
		label string
		value string
	}{
		{"Version", m.metrics.Version},
		{"Uptime", m.metrics.Uptime.String()},
		{"Total Rows", fmt.Sprintf("%d", m.metrics.TotalRows)},
		{"Total Bytes", formatBytes(m.metrics.TotalBytes)},
		{"Background Pools", fmt.Sprintf("%d", m.metrics.BackgroundPools)},
		{"Max Parts", fmt.Sprintf("%d", m.metrics.MaxPartsInPartition)},
	}

	for _, met := range metrics {
		s += contentStyle.Render(fmt.Sprintf("  %-20s", met.label))
		s += valueStyle.Render(fmt.Sprintf("%s\n", met.value))
	}

	return s
}

func (m *Model) tablesView() string {
	var s string
	s += sectionStyle.Render("\n  Tables\n\n")

	if len(m.tables) == 0 {
		s += contentStyle.Render("  No tables found\n")
		return s
	}

	s += contentStyle.Render(fmt.Sprintf("  %-25s %-15s %-15s %-12s %-12s\n", "Name", "Database", "Size", "Min Date", "Max Date"))
	s += contentStyle.Render("  " + repeat("-", 85) + "\n")

	for i, t := range m.tables {
		prefix := "  "
		style := contentStyle
		if i == m.selectedIdx {
			prefix = "> "
			style = valueStyle
		}
		s += style.Render(fmt.Sprintf("%s%-25s %-15s %-15s %-12s %-12s\n",
			prefix, truncate(t.Name, 23), truncate(t.Database, 13), t.Size, t.MinDate, t.MaxDate))
	}

	return s
}

func (m *Model) fatTablesView() string {
	return m.fatTable.View()
}

func (m *Model) processesView() string {
	var s string
	s += sectionStyle.Render("\n  Running Processes\n\n")

	if len(m.queries) == 0 {
		s += contentStyle.Render("  No running queries\n")
		return s
	}

	for i, q := range m.queries {
		s += valueStyle.Render(fmt.Sprintf("  [%d] %s\n", i+1, truncate(q.Query, 70)))
		s += contentStyle.Render(fmt.Sprintf("      Rows: %d | Bytes: %s | Memory: %s\n",
			q.RowsRead, formatBytes(q.BytesRead), formatBytes(q.MemoryUsage)))
	}

	return s
}

func (m *Model) historyView() string {
	var s string
	s += sectionStyle.Render("\n  Metrics History\n\n")

	s += contentStyle.Render("  Metric: ")
	s += valueStyle.Render(m.historyMetric + "\n")
	s += contentStyle.Render("  Period: ")
	s += valueStyle.Render(m.historyPeriod + "\n\n")

	if len(m.historyData) == 0 {
		s += contentStyle.Render("  No historical data available.\n")
		s += contentStyle.Render("  Data is collected every 2 minutes.\n")
		return s
	}

	s += contentStyle.Render(fmt.Sprintf("  %-25s %-20s\n", "Timestamp", "Value"))
	s += contentStyle.Render("  " + repeat("-", 50) + "\n")

	for _, sample := range m.historyData {
		var valueStr string
		switch m.historyMetric {
		case "total_bytes":
			valueStr = formatBytes(uint64(sample.Value))
		case "total_rows":
			valueStr = fmt.Sprintf("%d rows", sample.Value)
		case "uptime":
			valueStr = (time.Duration(sample.Value) * time.Second).String()
		default:
			valueStr = fmt.Sprintf("%d", sample.Value)
		}
		s += contentStyle.Render(fmt.Sprintf("  %-25s %-20s\n",
			sample.At.Format("2006-01-02 15:04:05"), valueStr))
	}

	return s
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}

func repeat(s string, count int) string {
	var result string
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
