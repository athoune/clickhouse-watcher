package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tab indices
const (
	tabDashboard = 0
	tabTables    = 1
	tabFatTables = 2
	tabProcesses = 3
	tabHistory   = 4
)

var tabNames = []string{"Dashboard", "Tables", "Fat Tables", "Processes", "History"}

var historyMetrics = []string{"total_bytes", "total_rows", "uptime"}
var historyPeriods = []string{"day", "week", "month"}

// --- messages ----------------------------------------------------------------

type connectedMsg struct{}
type errMsg struct{ err error }
type metricsMsg struct{ metrics *clickhouse.SystemMetrics }
type tablesMsg struct{ tables []clickhouse.TableMetric }
type truncatablesMsg struct{ tables []clickhouse.TruncatableTable }
type queriesMsg struct{ queries []clickhouse.QueryMetric }
type historyMsg struct{ samples []rrd.Sample }
type tableDetailMsg struct{ detail *clickhouse.TableDetail }
type actionDoneMsg struct{ info string }

// --- styles ------------------------------------------------------------------

var (
	colorBg      = lipgloss.Color("#1E1E1E")
	colorBlue    = lipgloss.Color("#0078D4")
	colorGreen   = lipgloss.Color("#00FF88")
	colorRed     = lipgloss.Color("#FF5555")
	colorGray    = lipgloss.Color("#888888")
	colorDimGray = lipgloss.Color("#666666")
	colorCyan    = lipgloss.Color("#00D9FF")
	colorWhite   = lipgloss.Color("#CCCCCC")

	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorBlue).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				Padding(0, 1)

	tabSepStyle = lipgloss.NewStyle().
			Foreground(colorDimGray)

	sectionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorGray)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDimGray).
			Background(colorBg)

	logoStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	tableStyle = table.DefaultStyles()
)

func init() {
	tableStyle.Header = tableStyle.Header.
		Bold(true).
		Foreground(colorCyan).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(colorGray)
	tableStyle.Selected = tableStyle.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(colorBlue).
		Bold(false)
	tableStyle.Cell = tableStyle.Cell.
		Foreground(colorWhite)
}

// --- Model -------------------------------------------------------------------

type Model struct {
	tab    int
	daemon *client.Client
	width  int
	height int

	// connect screen
	spinner    spinner.Model
	loading    bool
	connectErr string

	// cached data
	metrics      *clickhouse.SystemMetrics
	tables       []clickhouse.TableMetric
	truncatables []clickhouse.TruncatableTable
	queries      []clickhouse.QueryMetric
	historyData  []rrd.Sample

	// pane components
	dashViewport viewport.Model
	tablesTable  table.Model
	fatTable     table.Model
	procViewport viewport.Model
	histViewport viewport.Model
	ttlInput     textinput.Model

	// dashboard state
	tableDetail *clickhouse.TableDetail
	actionMsg   string

	// history navigation
	histMetricIdx int
	histPeriodIdx int
}

func New(socketPath string) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorCyan)

	ti := textinput.New()
	ti.Placeholder = "e.g. created_at + INTERVAL 30 DAY"
	ti.CharLimit = 200

	m := &Model{
		tab:           tabDashboard,
		daemon:        client.NewClient(socketPath),
		spinner:       sp,
		loading:       true,
		histMetricIdx: 0,
		histPeriodIdx: 0,
		ttlInput:      ti,
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.cmdConnect(),
	)
}

// --- Update ------------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanes()
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case connectedMsg:
		m.loading = false
		m.connectErr = ""
		return m, tea.Batch(
			m.cmdFetchMetrics(),
			m.cmdFetchTables(),
			m.cmdFetchTruncatables(),
			m.cmdFetchQueries(),
		)

	case errMsg:
		m.loading = false
		m.connectErr = msg.err.Error()
		return m, nil

	case metricsMsg:
		m.metrics = msg.metrics
		m.refreshDashboard()
		return m, nil

	case tablesMsg:
		m.tables = msg.tables
		m.rebuildTablesTable()
		return m, nil

	case truncatablesMsg:
		m.truncatables = msg.tables
		m.rebuildFatTable()
		return m, nil

	case queriesMsg:
		m.queries = msg.queries
		m.refreshProcesses()
		return m, nil

	case historyMsg:
		m.historyData = msg.samples
		m.refreshHistory()
		return m, nil

	case tableDetailMsg:
		m.tableDetail = msg.detail
		m.ttlInput.SetValue("")
		m.ttlInput.Blur()
		m.refreshDashboard()
		return m, nil

	case actionDoneMsg:
		m.actionMsg = msg.info
		m.refreshDashboard()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// delegate to focused sub-component
	return m.updateFocused(msg)
}

func (m *Model) updateFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tab {
	case tabDashboard:
		if m.tableDetail != nil && m.ttlInput.Focused() {
			m.ttlInput, cmd = m.ttlInput.Update(msg)
			return m, cmd
		}
		m.dashViewport, cmd = m.dashViewport.Update(msg)
		return m, cmd
	case tabTables:
		m.tablesTable, cmd = m.tablesTable.Update(msg)
		return m, cmd
	case tabFatTables:
		m.fatTable, cmd = m.fatTable.Update(msg)
		return m, cmd
	case tabProcesses:
		m.procViewport, cmd = m.procViewport.Update(msg)
		return m, cmd
	case tabHistory:
		m.histViewport, cmd = m.histViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// TTL input absorbs all keys when focused
	if m.tab == tabDashboard && m.tableDetail != nil && m.ttlInput.Focused() {
		switch msg.Type {
		case tea.KeyEsc:
			m.ttlInput.Blur()
			return m, nil
		case tea.KeyEnter:
			return m, m.cmdModifyTTL()
		default:
			var cmd tea.Cmd
			m.ttlInput, cmd = m.ttlInput.Update(msg)
			return m, cmd
		}
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if m.tab == tabDashboard && m.tableDetail != nil {
			m.tableDetail = nil
			m.actionMsg = ""
			m.ttlInput.Blur()
			m.refreshDashboard()
			return m, nil
		}
		return m, tea.Quit
	case tea.KeyTab:
		m.tab = (m.tab + 1) % len(tabNames)
		m.ttlInput.Blur()
		return m, nil
	case tea.KeyEnter:
		switch m.tab {
		case tabTables:
			return m, m.cmdShowTableDetail(m.tablesTable.Cursor())
		case tabFatTables:
			return m, m.cmdFatTableSelect()
		}
	}

	switch msg.Type {
	case tea.KeyLeft:
		if m.tab == tabHistory {
			m.histPeriodIdx = (m.histPeriodIdx - 1 + len(historyPeriods)) % len(historyPeriods)
			return m, m.cmdFetchHistory()
		}
	case tea.KeyRight:
		if m.tab == tabHistory {
			m.histPeriodIdx = (m.histPeriodIdx + 1) % len(historyPeriods)
			return m, m.cmdFetchHistory()
		}
	case tea.KeyUp:
		if m.tab == tabHistory {
			m.histMetricIdx = (m.histMetricIdx - 1 + len(historyMetrics)) % len(historyMetrics)
			return m, m.cmdFetchHistory()
		}
	case tea.KeyDown:
		if m.tab == tabHistory {
			m.histMetricIdx = (m.histMetricIdx + 1) % len(historyMetrics)
			return m, m.cmdFetchHistory()
		}
	}

	switch msg.String() {
	case "r":
		return m, m.cmdRefresh()
	case "t":
		if m.tab == tabDashboard && m.tableDetail != nil {
			return m, m.cmdTruncate()
		}
	case "l":
		if m.tab == tabDashboard && m.tableDetail != nil {
			m.ttlInput.Focus()
			m.refreshDashboard()
			return m, textinput.Blink
		}
	}

	return m.updateFocused(msg)
}

// --- commands ----------------------------------------------------------------

func (m *Model) cmdConnect() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// keep the 500ms animation the user wants
		time.Sleep(500 * time.Millisecond)

		ok, err := m.daemon.IsConnected(ctx)
		if err != nil || !ok {
			return errMsg{fmt.Errorf("daemon not available")}
		}
		return connectedMsg{}
	}
}

func (m *Model) cmdFetchMetrics() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		metrics, err := m.daemon.GetMetrics(ctx)
		if err != nil {
			return errMsg{err}
		}
		return metricsMsg{metrics}
	}
}

func (m *Model) cmdFetchTables() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tables, err := m.daemon.GetTables(ctx)
		if err != nil {
			return errMsg{err}
		}
		return tablesMsg{tables}
	}
}

func (m *Model) cmdFetchTruncatables() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tables, err := m.daemon.GetTruncatableTables(ctx)
		if err != nil {
			return errMsg{err}
		}
		return truncatablesMsg{tables}
	}
}

func (m *Model) cmdFetchQueries() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		queries, err := m.daemon.GetQueries(ctx)
		if err != nil {
			return errMsg{err}
		}
		return queriesMsg{queries}
	}
}

func (m *Model) cmdFetchHistory() tea.Cmd {
	metric := historyMetrics[m.histMetricIdx]
	period := historyPeriods[m.histPeriodIdx]
	return func() tea.Msg {
		samples, err := m.daemon.GetHistory(metric, period)
		if err != nil {
			return errMsg{err}
		}
		return historyMsg{samples}
	}
}

func (m *Model) cmdRefresh() tea.Cmd {
	switch m.tab {
	case tabDashboard:
		return m.cmdFetchMetrics()
	case tabTables:
		return tea.Batch(m.cmdFetchTables(), m.cmdFetchTruncatables())
	case tabFatTables:
		return tea.Batch(m.cmdFetchTables(), m.cmdFetchTruncatables())
	case tabProcesses:
		return m.cmdFetchQueries()
	case tabHistory:
		return m.cmdFetchHistory()
	}
	return nil
}

func (m *Model) cmdShowTableDetail(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.tables) {
		return nil
	}
	t := m.tables[idx]
	return func() tea.Msg {
		return tableDetailMsg{&clickhouse.TableDetail{
			Database: t.Database,
			Name:     t.Name,
		}}
	}
}

func (m *Model) cmdFatTableSelect() tea.Cmd {
	idx := m.fatTable.Cursor()
	if idx < 0 || idx >= len(m.truncatables) {
		return nil
	}
	t := m.truncatables[idx]
	return func() tea.Msg {
		m.tab = tabDashboard
		return tableDetailMsg{&clickhouse.TableDetail{
			Database: t.Database,
			Name:     t.Table,
		}}
	}
}

func (m *Model) cmdTruncate() tea.Cmd {
	if m.tableDetail == nil {
		return nil
	}
	db := m.tableDetail.Database
	tbl := m.tableDetail.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.daemon.TruncateTable(ctx, db, tbl); err != nil {
			return errMsg{fmt.Errorf("truncate failed: %w", err)}
		}
		m.tableDetail = nil
		m.actionMsg = ""
		return actionDoneMsg{"truncated"}
	}
}

func (m *Model) cmdModifyTTL() tea.Cmd {
	if m.tableDetail == nil {
		return nil
	}
	db := m.tableDetail.Database
	tbl := m.tableDetail.Name
	ttl := m.ttlInput.Value()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.daemon.ModifyTTL(ctx, db, tbl, ttl); err != nil {
			return errMsg{fmt.Errorf("TTL modify failed: %w", err)}
		}
		return actionDoneMsg{fmt.Sprintf("TTL set to: %s", ttl)}
	}
}

// --- pane builders -----------------------------------------------------------

func (m *Model) resizePanes() {
	contentH := m.contentHeight()
	contentW := m.width

	m.dashViewport.Width = contentW
	m.dashViewport.Height = contentH

	m.procViewport.Width = contentW
	m.procViewport.Height = contentH

	m.histViewport.Width = contentW
	m.histViewport.Height = contentH

	m.rebuildTablesTable()
	m.rebuildFatTable()
}

func (m *Model) contentHeight() int {
	// subtract tab bar (1) + separator (1) + help bar (1)
	h := m.height - 3
	if h < 1 {
		return 1
	}
	return h
}

func (m *Model) tableWidth() int {
	if m.width < 20 {
		return 80
	}
	return m.width
}

func (m *Model) rebuildTablesTable() {
	w := m.tableWidth()
	nameW := w / 4
	dbW := w / 5
	sizeW := 12
	dateW := 12
	cols := []table.Column{
		{Title: "Table", Width: nameW},
		{Title: "Database", Width: dbW},
		{Title: "Size", Width: sizeW},
		{Title: "Min Date", Width: dateW},
		{Title: "Max Date", Width: dateW},
	}
	rows := make([]table.Row, 0, len(m.tables))
	for _, t := range m.tables {
		rows = append(rows, table.Row{t.Name, t.Database, t.Size, t.MinDate, t.MaxDate})
	}
	cur := 0
	if m.tablesTable.Cursor() < len(rows) {
		cur = m.tablesTable.Cursor()
	}
	h := m.contentHeight() - 2
	if h < 1 {
		h = 1
	}
	m.tablesTable = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
	)
	m.tablesTable.SetStyles(tableStyle)
	m.tablesTable.SetCursor(cur)
}

func (m *Model) rebuildFatTable() {
	w := m.tableWidth()
	dbW := w / 5
	nameW := w / 4
	sizeW := 12
	rowsW := 10
	truncW := 11
	cols := []table.Column{
		{Title: "Database", Width: dbW},
		{Title: "Table", Width: nameW},
		{Title: "Size", Width: sizeW},
		{Title: "Rows", Width: rowsW},
		{Title: "Truncatable", Width: truncW},
	}
	rows := make([]table.Row, 0, len(m.truncatables))
	for _, t := range m.truncatables {
		flag := "no"
		if t.Truncatable {
			flag = "yes"
		}
		rows = append(rows, table.Row{t.Database, t.Table, t.Size, fmt.Sprintf("%d", t.Rows), flag})
	}
	cur := 0
	if m.fatTable.Cursor() < len(rows) {
		cur = m.fatTable.Cursor()
	}
	h := m.contentHeight() - 2
	if h < 1 {
		h = 1
	}
	m.fatTable = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
	)
	m.fatTable.SetStyles(tableStyle)
	m.fatTable.SetCursor(cur)
}

func (m *Model) refreshDashboard() {
	m.dashViewport.SetContent(m.renderDashboardContent())
}

func (m *Model) refreshProcesses() {
	m.procViewport.SetContent(m.renderProcessesContent())
}

func (m *Model) refreshHistory() {
	m.histViewport.SetContent(m.renderHistoryContent())
}

// --- View --------------------------------------------------------------------

func (m *Model) View() string {
	if m.loading || m.connectErr != "" {
		return m.connectView()
	}

	return strings.Join([]string{
		m.renderTabBar(),
		m.renderContent(),
		m.renderHelp(),
	}, "\n")
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
	logo := logoStyle.Align(lipgloss.Center).Width(m.width).Render(asciiLogo)
	var status string
	if m.connectErr != "" {
		status = errorStyle.Render("  Connection failed: "+m.connectErr) +
			"\n\n" + mutedStyle.Render("  Press ESC to quit")
	} else {
		status = labelStyle.Render("  Connecting to ") +
			valueStyle.Render(m.daemon.SocketPath()) +
			"  " + m.spinner.View()
	}
	return logo + "\n\n" + status + "\n"
}

func (m *Model) renderTabBar() string {
	parts := make([]string, 0, len(tabNames)*2)
	for i, name := range tabNames {
		if i > 0 {
			parts = append(parts, tabSepStyle.Render(" │ "))
		}
		if i == m.tab {
			parts = append(parts, activeTabStyle.Render(name))
		} else {
			parts = append(parts, inactiveTabStyle.Render(name))
		}
	}
	return strings.Join(parts, "")
}

func (m *Model) renderContent() string {
	switch m.tab {
	case tabDashboard:
		return m.dashViewport.View()
	case tabTables:
		return m.tablesTable.View()
	case tabFatTables:
		return m.fatTable.View()
	case tabProcesses:
		return m.procViewport.View()
	case tabHistory:
		return m.histViewport.View()
	}
	return ""
}

func (m *Model) renderHelp() string {
	var help string
	switch m.tab {
	case tabDashboard:
		if m.tableDetail != nil {
			if m.ttlInput.Focused() {
				help = "[Enter] apply TTL  [Esc] cancel"
			} else {
				help = "[t] truncate  [l] edit TTL  [Esc] back  [r] refresh"
			}
		} else {
			help = "[r] refresh  [Tab] next tab"
		}
	case tabTables:
		help = "[↑/↓] select  [Enter] details  [r] refresh  [Tab] next tab"
	case tabFatTables:
		help = "[↑/↓] select  [Enter] details  [r] refresh  [Tab] next tab"
	case tabProcesses:
		help = "[↑/↓] scroll  [r] refresh  [Tab] next tab"
	case tabHistory:
		help = "[↑/↓] metric  [←/→] period  [r] refresh  [Tab] next tab"
	}
	return helpStyle.Width(m.width).Render("  " + help)
}

// --- content renderers -------------------------------------------------------

func (m *Model) renderDashboardContent() string {
	var b strings.Builder

	if m.tableDetail != nil {
		b.WriteString(sectionStyle.Render("Table Details") + "\n\n")
		row := func(label, val string) string {
			return labelStyle.Render(fmt.Sprintf("  %-16s", label)) +
				valueStyle.Render(val) + "\n"
		}
		b.WriteString(row("Database:", m.tableDetail.Database))
		b.WriteString(row("Table:", m.tableDetail.Name))
		if m.tableDetail.Engine != "" {
			b.WriteString(row("Engine:", m.tableDetail.Engine))
		}
		if m.tableDetail.SortingKey != "" {
			b.WriteString(row("Sorting Key:", m.tableDetail.SortingKey))
		}
		if m.tableDetail.TTL != "" {
			b.WriteString(row("Current TTL:", m.tableDetail.TTL))
		}
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Modify TTL") + "\n\n")
		b.WriteString("  " + m.ttlInput.View() + "\n")

		if m.actionMsg != "" {
			b.WriteString("\n  " + valueStyle.Render(m.actionMsg) + "\n")
		}
		if m.connectErr != "" {
			b.WriteString("\n  " + errorStyle.Render(m.connectErr) + "\n")
		}
		return b.String()
	}

	b.WriteString(sectionStyle.Render("System Metrics") + "\n\n")
	if m.metrics == nil {
		b.WriteString(mutedStyle.Render("  No metrics available") + "\n")
		return b.String()
	}

	type kv struct{ label, value string }
	rows := []kv{
		{"Version", m.metrics.Version},
		{"Uptime", m.metrics.Uptime.String()},
		{"Total Rows", fmt.Sprintf("%d", m.metrics.TotalRows)},
		{"Total Bytes", formatBytes(m.metrics.TotalBytes)},
		{"Background Pools", fmt.Sprintf("%d", m.metrics.BackgroundPools)},
		{"Max Parts", fmt.Sprintf("%d", m.metrics.MaxPartsInPartition)},
	}
	for _, r := range rows {
		b.WriteString(labelStyle.Render(fmt.Sprintf("  %-20s", r.label)))
		b.WriteString(valueStyle.Render(r.value) + "\n")
	}
	return b.String()
}

func (m *Model) renderProcessesContent() string {
	var b strings.Builder
	b.WriteString(sectionStyle.Render("Running Processes") + "\n\n")
	if len(m.queries) == 0 {
		b.WriteString(mutedStyle.Render("  No running queries") + "\n")
		return b.String()
	}
	for i, q := range m.queries {
		b.WriteString(valueStyle.Render(fmt.Sprintf("  [%d] ", i+1)))
		b.WriteString(labelStyle.Render(truncateStr(q.Query, 72)) + "\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("      rows: %d  bytes: %s  mem: %s\n",
			q.RowsRead, formatBytes(q.BytesRead), formatBytes(q.MemoryUsage))))
	}
	return b.String()
}

func (m *Model) renderHistoryContent() string {
	var b strings.Builder

	metric := historyMetrics[m.histMetricIdx]
	period := historyPeriods[m.histPeriodIdx]

	b.WriteString(sectionStyle.Render("Metrics History") + "\n\n")
	b.WriteString(labelStyle.Render("  Metric: ") + valueStyle.Render(metric) + "\n")
	b.WriteString(labelStyle.Render("  Period: ") + valueStyle.Render(period) + "\n\n")

	if len(m.historyData) == 0 {
		b.WriteString(mutedStyle.Render("  No historical data yet — collected every 2 minutes.") + "\n")
		return b.String()
	}

	b.WriteString(mutedStyle.Render(fmt.Sprintf("  %-26s %-20s\n", "Timestamp", "Value")))
	b.WriteString(mutedStyle.Render("  " + strings.Repeat("─", 48) + "\n"))
	for _, s := range m.historyData {
		val := formatHistoryValue(metric, s.Value)
		b.WriteString(labelStyle.Render(fmt.Sprintf("  %-26s", s.At.Format("2006-01-02 15:04:05"))))
		b.WriteString(valueStyle.Render(val) + "\n")
	}
	return b.String()
}

// --- helpers -----------------------------------------------------------------

func formatHistoryValue(metric string, v int64) string {
	switch metric {
	case "total_bytes":
		return formatBytes(uint64(v))
	case "total_rows":
		return fmt.Sprintf("%d rows", v)
	case "uptime":
		return (time.Duration(v) * time.Second).String()
	}
	return fmt.Sprintf("%d", v)
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

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
