package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
	"github.com/athoune/clickhouse-watcher/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	view          viewState
	daemon        *client.Client
	err           error
	metrics       *clickhouse.SystemMetrics
	tables        []clickhouse.TableMetric
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
}

type viewState string

const (
	connectView     viewState = "connect"
	dashboardView   viewState = "dashboard"
	queryView       viewState = "query"
	tablesView      viewState = "tables"
	processesView   viewState = "processes"
	tableDetailView viewState = "table_detail"
	confirmView     viewState = "confirm"
	historyView     viewState = "history"
)

func New(socketPath string) *Model {
	return &Model{
		view:   connectView,
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
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		return m, nil
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case connectView:
		return m.handleConnectKey(msg)

	case dashboardView, tablesView, processesView, historyView:
		return m.handleNavKey(msg)

	case tableDetailView:
		return m.handleTableDetailKey(msg)

	case confirmView:
		return m.handleConfirmKey(msg)

	case queryView:
		return m.handleQueryKey(msg)

	default:
		return m, nil
	}
}

func (m *Model) handleConnectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return m, m.connect()
	case tea.KeyCtrlC, tea.KeyEsc:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab:
		m.nextView()
		m.selectedIdx = 0
	case tea.KeyCtrlC, tea.KeyEsc:
		return m, tea.Quit
	case tea.KeyUp:
		if m.view == tablesView && m.selectedIdx > 0 {
			m.selectedIdx--
		}
		if m.view == historyView {
			m.selectedIdx = 0
		}
	case tea.KeyDown:
		if m.view == tablesView && m.selectedIdx < len(m.tables)-1 {
			m.selectedIdx++
		}
		if m.view == historyView {
			m.selectedIdx = 1
		}
	case tea.KeyLeft:
		if m.view == historyView {
			m.cycleHistoryPeriod(-1)
			return m, m.loadHistory()
		}
	case tea.KeyRight:
		if m.view == historyView {
			m.cycleHistoryPeriod(1)
			return m, m.loadHistory()
		}
	case tea.KeyEnter:
		if m.view == tablesView && len(m.tables) > 0 {
			return m, m.showTableDetail()
		}
	case tea.KeyRunes:
		if msg.String() == "r" {
			return m, m.refresh()
		}
		if msg.String() == "h" && m.view == dashboardView {
			m.view = historyView
			m.historyMetric = "total_bytes"
			m.historyPeriod = "day"
			return m, m.loadHistory()
		}
	}
	return m, nil
}

func (m *Model) handleTableDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.view = tablesView
		m.actionMsg = ""
	case tea.KeyRunes:
		if msg.String() == "t" {
			return m, m.truncateTable()
		}
		if msg.String() == "l" {
			return m, m.modifyTTL()
		}
	case tea.KeyBackspace:
		if len(m.ttlInput) > 0 {
			m.ttlInput = m.ttlInput[:len(m.ttlInput)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.ttlInput += msg.String()
		}
	}
	return m, nil
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if m.actionMsg == "truncate" {
			return m, m.executeTruncate()
		}
		m.view = tableDetailView
		m.actionMsg = ""
	case tea.KeyEsc:
		m.view = tableDetailView
		m.actionMsg = ""
	}
	return m, nil
}

func (m *Model) handleQueryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return m, m.executeQuery()
	case tea.KeyCtrlC, tea.KeyEsc:
		m.view = dashboardView
	case tea.KeyBackspace:
		if len(m.queryInput) > 0 {
			m.queryInput = m.queryInput[:len(m.queryInput)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.queryInput += msg.String()
		}
	}
	return m, nil
}

func (m *Model) nextView() {
	switch m.view {
	case dashboardView:
		m.view = tablesView
	case tablesView:
		m.view = processesView
	case processesView:
		m.view = historyView
	case historyView:
		m.view = queryView
	case queryView:
		m.view = dashboardView
	}
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
		m.view = dashboardView
		m.loading = false
		return nil
	}
}

func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		switch m.view {
		case dashboardView:
			metrics, err := m.daemon.GetMetrics(ctx)
			if err == nil {
				m.metrics = metrics
			}
		case tablesView:
			tables, err := m.daemon.GetTables(ctx)
			if err == nil {
				m.tables = tables
			}
		case processesView:
			queries, err := m.daemon.GetQueries(ctx)
			if err == nil {
				m.queries = queries
			}
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
		m.view = tableDetailView
		return nil
	}
}

func (m *Model) truncateTable() tea.Cmd {
	return func() tea.Msg {
		m.view = confirmView
		m.actionMsg = "truncate"
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

		m.view = tableDetailView
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

func (m *Model) executeQuery() tea.Cmd {
	return func() tea.Msg {
		if m.queryInput == "" {
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := m.daemon.ExecuteQuery(ctx, m.queryInput)
		if err != nil {
			m.err = fmt.Errorf("query failed: %v", err)
			return nil
		}

		m.headers = result.Headers
		m.results = result.Rows
		return nil
	}
}

func (m *Model) View() string {
	switch m.view {
	case connectView:
		return m.connectView()
	case dashboardView:
		return m.dashboardView()
	case tablesView:
		return m.tablesView()
	case processesView:
		return m.processesView()
	case historyView:
		return m.historyView()
	case tableDetailView:
		return m.tableDetailView()
	case confirmView:
		return m.confirmView()
	case queryView:
		return m.queryView()
	default:
		return ""
	}
}

func (m *Model) connectView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  ClickHouse Watcher\n\n"))

	if m.loading {
		b.WriteString("  Connecting to daemon...\n")
	} else if m.err != nil {
		b.WriteString(styles.ErrorStyle.Render(fmt.Sprintf("  Connection failed: %v\n", m.err)))
		b.WriteString("\n  Press ESC to quit\n")
	} else {
		b.WriteString("  Connected!\n")
	}
	return b.String()
}

func (m *Model) dashboardView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  System Metrics\n\n"))
	b.WriteString(styles.BorderStyle.Render(
		fmt.Sprintf("  Version:              %s\n", m.metrics.Version) +
			fmt.Sprintf("  Uptime:               %s\n", m.metrics.Uptime) +
			fmt.Sprintf("  Total Rows:           %d\n", m.metrics.TotalRows) +
			fmt.Sprintf("  Total Bytes:          %s\n", formatBytes(m.metrics.TotalBytes)) +
			fmt.Sprintf("  Background Pools:     %d\n", m.metrics.BackgroundPools) +
			fmt.Sprintf("  Max Parts in Part:    %d\n", m.metrics.MaxPartsInPartition),
	))
	b.WriteString("\n  [Tab] Next view  [R] Refresh  [Esc] Quit\n")
	return b.String()
}

func (m *Model) tablesView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  Tables\n\n"))

	if len(m.tables) == 0 {
		b.WriteString("  No tables found\n")
	} else {
		b.WriteString(styles.TableHeaderStyle.Render(
			fmt.Sprintf("  %-25s %-15s %-20s %-20s\n", "Name", "Database", "Min Date", "Max Date"),
		))
		for i, t := range m.tables {
			rowStyle := styles.TableCellStyle
			if i == m.selectedIdx {
				rowStyle = styles.SelectedStyle
			}
			prefix := "  "
			if i == m.selectedIdx {
				prefix = "> "
			}
			b.WriteString(rowStyle.Render(
				fmt.Sprintf("%s%-25s %-15s %-20s %-20s\n",
					prefix, truncate(t.Name, 23), truncate(t.Database, 13), t.MinDate, t.MaxDate),
			))
		}
	}

	if m.selectedIdx < len(m.tables) && m.selectedIdx >= 0 {
		b.WriteString(fmt.Sprintf("\n  Size: %s\n", m.tables[m.selectedIdx].Size))
	}
	b.WriteString("\n  [↑/↓] Select  [Enter] Details  [R] Refresh  [Esc] Quit\n")
	return b.String()
}

func (m *Model) tableDetailView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  Table Details\n\n"))

	if m.tableDetail != nil {
		b.WriteString(fmt.Sprintf("  Database:    %s\n", m.tableDetail.Database))
		b.WriteString(fmt.Sprintf("  Name:        %s\n", m.tableDetail.Name))
		b.WriteString(fmt.Sprintf("  Engine:      %s\n", m.tableDetail.Engine))
		b.WriteString(fmt.Sprintf("  Sorting Key: %s\n", m.tableDetail.SortingKey))
		b.WriteString("\n")
		b.WriteString(styles.SubtitleStyle.Render("  TTL:\n"))
		b.WriteString(styles.HighlightStyle.Render("  " + m.ttlInput))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString(styles.ErrorStyle.Render(fmt.Sprintf("\n  Error: %v\n", m.err)))
		m.err = nil
	}

	b.WriteString("\n  [t] Truncate table  [l] Apply TTL  [Esc] Back\n")
	b.WriteString("  TTL format: e.g., INTERVAL 7 DAY, INTERVAL 1 MONTH, etc.\n")
	return b.String()
}

func (m *Model) confirmView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  Confirm Action\n\n"))

	if m.actionMsg == "truncate" && m.tableDetail != nil {
		b.WriteString(styles.ErrorStyle.Render(
			fmt.Sprintf("  WARNING: TRUNCATE TABLE `%s`.`%s`?\n", m.tableDetail.Database, m.tableDetail.Name),
		))
		b.WriteString("  This will delete all data in the table!\n\n")
	}

	b.WriteString("  [Enter] Confirm  [Esc] Cancel\n")
	return b.String()
}

func (m *Model) historyView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  Metrics History\n\n"))

	b.WriteString(fmt.Sprintf("  Metric: %s\n", m.historyMetric))
	b.WriteString(fmt.Sprintf("  Period: %s\n\n", m.historyPeriod))

	if len(m.historyData) == 0 {
		b.WriteString("  No historical data available yet.\n")
		b.WriteString("  Data is collected every 2 minutes.\n")
	} else {
		b.WriteString(styles.TableHeaderStyle.Render(
			fmt.Sprintf("  %-25s %-20s\n", "Timestamp", "Value"),
		))
		for _, s := range m.historyData {
			var valueStr string
			switch m.historyMetric {
			case "total_bytes":
				valueStr = formatBytes(uint64(s.Value))
			case "total_rows":
				valueStr = fmt.Sprintf("%d rows", s.Value)
			case "uptime":
				valueStr = (time.Duration(s.Value) * time.Second).String()
			default:
				valueStr = fmt.Sprintf("%d", s.Value)
			}
			b.WriteString(styles.TableCellStyle.Render(
				fmt.Sprintf("  %-25s %-20s\n", s.At.Format("2006-01-02 15:04:05"), valueStr),
			))
		}
	}

	b.WriteString("\n  [↑/↓] Switch metric  [←/→] Change period  [Tab] Next view  [Esc] Back\n")
	return b.String()
}

func (m *Model) processesView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  Running Queries\n\n"))

	if len(m.queries) == 0 {
		b.WriteString("  No running queries\n")
	} else {
		for i, q := range m.queries {
			b.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, truncate(q.Query, 60)))
			b.WriteString(fmt.Sprintf("      Rows: %d | Bytes: %s | Memory: %s\n",
				q.RowsRead, formatBytes(q.BytesRead), formatBytes(q.MemoryUsage)))
		}
	}

	b.WriteString("\n  [Tab] Next view  [R] Refresh  [Esc] Quit\n")
	return b.String()
}

func (m *Model) queryView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("\n  Query Executor\n\n"))
	b.WriteString(styles.SubtitleStyle.Render("  > "))
	b.WriteString(styles.HighlightStyle.Render(m.queryInput))
	b.WriteString("\n\n")

	if len(m.results) > 0 {
		for _, h := range m.headers {
			b.WriteString(styles.TableHeaderStyle.Render(fmt.Sprintf("  %-15s", truncate(h, 13))) + "\n")
		}
		b.WriteString("\n")
		for _, row := range m.results {
			for _, cell := range row {
				b.WriteString(styles.TableCellStyle.Render(fmt.Sprintf("  %-15s", truncate(cell, 13))) + "\n")
			}
			b.WriteString("\n")
		}
	}

	if m.err != nil {
		b.WriteString(styles.ErrorStyle.Render(fmt.Sprintf("  Error: %v\n", m.err)))
		m.err = nil
	}

	b.WriteString("\n  [Enter] Execute  [Esc] Back  [Tab] Next view\n")
	return b.String()
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
