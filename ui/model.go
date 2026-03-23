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

// Model is the main application state for the ClickHouse Watcher TUI.
// It holds all data needed to render the interface and handles user interactions.
type Model struct {
	view          viewState                 // Current view being displayed
	daemon        *client.Client            // HTTP client connected to the daemon via Unix socket
	err           error                     // Last error encountered
	metrics       *clickhouse.SystemMetrics // System metrics from ClickHouse
	tables        []clickhouse.TableMetric  // List of tables with sizes
	queries       []clickhouse.QueryMetric  // Currently running queries
	queryInput    string                    // SQL query text being typed
	results       [][]string                // Query results as string rows
	headers       []string                  // Query result column names
	loading       bool                      // True while connecting to daemon
	width         int                       // Terminal width
	height        int                       // Terminal height
	selectedIdx   int                       // Selected table index in tables view
	tableDetail   *clickhouse.TableDetail   // Currently viewed table details
	ttlInput      string                    // TTL expression being typed
	actionMsg     string                    // Action type for confirmation dialog
	historyData   []rrd.Sample              // Historical metric samples
	historyPeriod string                    // History time period: day, week, or month
	historyMetric string                    // History metric: total_bytes, total_rows, or uptime
}

// viewState represents the different screens in the application.
type viewState string

// View constants define all available application screens.
const (
	connectView     viewState = "connect"      // Initial connection screen
	dashboardView   viewState = "dashboard"    // System metrics overview
	queryView       viewState = "query"        // SQL query executor
	tablesView      viewState = "tables"       // List of tables
	processesView   viewState = "processes"    // Running queries
	tableDetailView viewState = "table_detail" // Single table details
	confirmView     viewState = "confirm"      // Confirmation dialog
	historyView     viewState = "history"      // Historical metrics
)

// New creates a new Model instance for the given daemon socket path.
// Starts in the connectView state and auto-connects on Init.
func New(socketPath string) *Model {
	return &Model{
		view:   connectView,
		daemon: client.NewClient(socketPath),
	}
}

// Init is called once when the application starts.
// Returns a command to auto-connect to the daemon.
func (m *Model) Init() tea.Cmd {
	return m.connect()
}

// Update handles incoming messages and updates the model state.
// Main entry point for user input and window events.
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

// handleKey dispatches keyboard events to the appropriate view handler
// based on the current view state.
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

// handleConnectKey handles keyboard input on the connection screen.
// Only allows quitting (Ctrl+C or Esc); auto-connect happens in Init.
func (m *Model) handleConnectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return m, tea.Quit
	}
	return m, nil
}

// handleNavKey handles navigation keys for main views.
// Supports Tab cycling, arrow keys, Enter selection, and 'r' to refresh.
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

// handleTableDetailKey handles input on the table detail view.
// Supports typing TTL expressions and triggering truncate or TTL apply.
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

// handleConfirmKey handles input on confirmation dialogs.
// Enter confirms, Esc cancels.
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

// handleQueryKey handles input in the SQL query executor.
// Supports typing queries, backspace, Enter to execute, Esc to exit.
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

// nextView cycles to the next main view in order.
// Order: Dashboard -> Tables -> Processes -> History -> Query -> Dashboard
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

// cycleHistoryPeriod changes the history time period.
// dir should be 1 for next period, -1 for previous.
// Cycles through: day -> week -> month -> day
func (m *Model) cycleHistoryPeriod(dir int) {
	periods := []string{"day", "week", "month"}
	for i, p := range periods {
		if p == m.historyPeriod {
			m.historyPeriod = periods[(i+dir+len(periods))%len(periods)]
			return
		}
	}
}

// loadHistory fetches historical metrics data from the daemon.
func (m *Model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, _ = ctx, cancel

		samples, err := m.daemon.GetHistory(m.historyMetric, m.historyPeriod)
		if err != nil {
			m.err = fmt.Errorf("failed to load history: %v", err)
			return nil
		}

		m.historyData = samples
		return nil
	}
}

// connect attempts to connect to the daemon and fetch initial metrics.
// If successful, transitions to dashboardView. Runs asynchronously.
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

// refresh fetches fresh data for the current view.
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

// showTableDetail populates tableDetail with the selected table's info
// and switches to the table detail view.
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

// truncateTable switches to the confirmation view for table truncation.
func (m *Model) truncateTable() tea.Cmd {
	return func() tea.Msg {
		m.view = confirmView
		m.actionMsg = "truncate"
		return nil
	}
}

// executeTruncate performs the actual table truncation via the daemon.
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

// modifyTTL applies the TTL expression to the current table via the daemon.
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

// executeQuery runs the typed SQL query via the daemon and stores results.
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

// View returns the rendered string for the current view.
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

// asciiLogo is the ClickHouse Watcher ASCII art banner displayed on the connect screen.
const asciiLogo = `
   ██████                                 ██████                     
  ██      ██                           ██      ██                    
  ██      ██   ██████   ██████        ██          ██   ██████  ██████ 
  ██      ██  ██    ██ ██    ██       ██   ██████ ██  ██    ██ ██   ██
  ██      ██  ██    ██ ██    ██       ██  ██   ██ ██  ██    ██ ██   ██
   ██████   ██  ██████   ██████         ████ ████ ██   ██████  ██████
`

// connectView renders the initial connection screen with ASCII logo and status.
func (m *Model) connectView() string {
	var b strings.Builder
	b.WriteString(styles.AsciiStyle.Render(asciiLogo))
	b.WriteString("\n")

	if m.loading {
		b.WriteString("  Connecting " + styles.FooterStyle.Render(m.daemon.SocketPath()) + "\n")
	} else if m.err != nil {
		b.WriteString(styles.ErrorStyle.Render(fmt.Sprintf("  Connection failed: %v\n", m.err)))
		b.WriteString("\n  Press ESC to quit\n")
	} else {
		b.WriteString("  Connected!\n")
	}
	return b.String()
}

// dashboardView renders the system metrics overview.
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

// tablesView renders the list of tables with selection.
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

// tableDetailView renders details for a single table with TTL management.
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

// confirmView renders the confirmation dialog for destructive actions.
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

// historyView renders the historical metrics data with period selection.
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

// processesView renders the list of currently running queries.
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

// queryView renders the SQL query executor with results display.
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

// formatBytes converts a byte count to a human-readable string (KB, MB, GB, etc.).
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

// truncate shortens a string to maxLen characters, appending ".." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}
