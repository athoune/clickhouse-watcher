package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/rrd"
	"github.com/athoune/clickhouse-watcher/version"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	humanize "github.com/dustin/go-humanize"
)

// processRefreshInterval controls how often the Processes tab polls for new data.
const processRefreshInterval = 10 * time.Second

// statsRefreshInterval controls how often system stats are refreshed.
const statsRefreshInterval = 10 * time.Second

// tab indices
const (
	tabDashboard = 0
	tabFatTables = 1
	tabProcesses = 2
	tabHistory   = 3
)

var tabNames = []string{"Dashboard", "Fat Tables", "Processes", "History"}

var historyMetrics = []string{"total_bytes", "total_rows", "disk_usage", "cpu_usage", "memory_usage", "ingestion", "users", "errors"}
var historyPeriods = []string{"day", "week", "month"}

var uiLog = logger.WithComponent("ui")

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type connectedMsg struct{}
type errMsg struct{ err error }
type metricsMsg struct{ metrics *clickhouse.SystemMetrics }
type truncatablesMsg struct{ tables []clickhouse.TruncatableTable }
type queriesMsg struct{ queries []clickhouse.QueryMetric }
type systemStatsMsg struct{ stats *clickhouse.SystemStats }
type historyMsg struct{ samples []rrd.Sample }
type tableDetailMsg struct{ detail *clickhouse.TableDetail }
type actionDoneMsg struct{ info string }
type processTickMsg struct{}

// ---------------------------------------------------------------------------
// Palette
// ---------------------------------------------------------------------------

var (
	// Base colours
	clrBg        = lipgloss.Color("#0D1117") // near-black background
	clrSurface   = lipgloss.Color("#161B22") // slightly lighter surface
	clrBorder    = lipgloss.Color("#30363D") // GitHub-dark border tone
	clrBlue      = lipgloss.Color("#58A6FF") // accent blue
	clrGreen     = lipgloss.Color("#3FB950") // success / value green
	clrYellow    = lipgloss.Color("#D29922") // warning / muted accent
	clrRed       = lipgloss.Color("#F85149") // error / destructive
	clrCyan      = lipgloss.Color("#79C0FF") // highlight cyan
	clrText      = lipgloss.Color("#C9D1D9") // primary text
	clrMuted     = lipgloss.Color("#8B949E") // secondary / muted text
	clrDim       = lipgloss.Color("#484F58") // very dim (separators)
	clrWhite     = lipgloss.Color("#FFFFFF")
	clrTabActive = lipgloss.Color("#1F6FEB") // active tab bg

	// Chrome
	chromeBarStyle = lipgloss.NewStyle().
			Background(clrSurface).
			Foreground(clrMuted)

	activeTabStyle = lipgloss.NewStyle().
			Background(clrTabActive).
			Foreground(clrWhite).
			Bold(true).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Background(clrSurface).
				Foreground(clrMuted).
				Padding(0, 2)

	tabSepStyle = lipgloss.NewStyle().
			Background(clrSurface).
			Foreground(clrDim)

	helpBarStyle = lipgloss.NewStyle().
			Background(clrSurface).
			Foreground(clrMuted)

	helpKeyStyle = lipgloss.NewStyle().
			Background(clrSurface).
			Foreground(clrCyan).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Background(clrSurface).
			Foreground(clrMuted)

	// Content
	sectionStyle = lipgloss.NewStyle().
			Foreground(clrBlue).
			Bold(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderBottom(true).
			BorderForeground(clrBorder)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrBorder).
			Padding(0, 1)

	labelStyle = lipgloss.NewStyle().
			Foreground(clrMuted)

	valueStyle = lipgloss.NewStyle().
			Foreground(clrGreen).
			Bold(true)

	dimValueStyle = lipgloss.NewStyle().
			Foreground(clrText)

	errorStyle = lipgloss.NewStyle().
			Foreground(clrRed).
			Bold(true)

	warnStyle = lipgloss.NewStyle().
			Foreground(clrYellow)

	mutedStyle = lipgloss.NewStyle().
			Foreground(clrMuted)

	logoStyle = lipgloss.NewStyle().
			Foreground(clrCyan).
			Bold(true)

	// Process card
	queryCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrBorder).
			Padding(0, 1).
			MarginBottom(1)

	queryTextStyle = lipgloss.NewStyle().
			Foreground(clrText)

	queryNumStyle = lipgloss.NewStyle().
			Foreground(clrCyan).
			Bold(true)

	// Table styles
	tblStyle = func() table.Styles {
		s := table.DefaultStyles()
		s.Header = s.Header.
			Bold(true).
			Foreground(clrCyan).
			Background(clrSurface).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(clrBorder)
		s.Selected = s.Selected.
			Foreground(clrWhite).
			Background(clrTabActive).
			Bold(true)
		s.Cell = s.Cell.
			Foreground(clrText)
		return s
	}()

	// TTL input
	ttlPromptStyle = lipgloss.NewStyle().Foreground(clrYellow)
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

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
	truncatables []clickhouse.TruncatableTable
	queries      []clickhouse.QueryMetric
	historyData  []rrd.Sample

	// pane components
	dashViewport viewport.Model
	fatTable     table.Model
	procViewport viewport.Model
	histViewport viewport.Model
	ttlInput     textinput.Model

	// dashboard detail state
	tableDetail *clickhouse.TableDetail
	actionMsg   string

	// history navigation
	histMetricIdx int
	histPeriodIdx int

	// processes auto-refresh
	procRefreshAt time.Time

	// system stats
	systemStats *clickhouse.SystemStats

	// confirmation popup state
	confirmingAction string
	confirmDatabase  string
	confirmTable     string
	confirmTableSize string
	confirmCallback  func() tea.Cmd
}

func New(socketPath string) *Model {
	uiLog.Debug().Str("socket", socketPath).Msg("Creating new UI model")

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(clrCyan)

	ti := textinput.New()
	ti.Placeholder = "e.g. created_at + INTERVAL 30 DAY"
	ti.CharLimit = 200
	ti.PromptStyle = ttlPromptStyle
	ti.TextStyle = lipgloss.NewStyle().Foreground(clrText)

	return &Model{
		tab:      tabDashboard,
		daemon:   client.NewClient(socketPath),
		spinner:  sp,
		loading:  true,
		ttlInput: ti,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.cmdConnect(),
	)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

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
		uiLog.Info().Msg("Connected to daemon")
		return m, tea.Batch(
			m.cmdFetchMetrics(),
			m.cmdFetchTruncatables(),
			m.cmdFetchQueries(),
			m.cmdProcessTick(),
			m.cmdSystemStatsTick(),
		)

	case errMsg:
		m.loading = false
		// Check if this is a version mismatch error and format it nicely
		errStr := msg.err.Error()
		if strings.Contains(errStr, "Version mismatch") {
			m.connectErr = "Version mismatch: client and server versions are different. Please ensure both are the same version."
		} else {
			m.connectErr = errStr
		}
		uiLog.Error().Err(msg.err).Msg("Connection error")
		return m, nil

	case metricsMsg:
		m.metrics = msg.metrics
		m.refreshDashboard()
		return m, nil

	case truncatablesMsg:
		m.truncatables = msg.tables
		m.rebuildFatTable()
		return m, nil

	case queriesMsg:
		m.queries = msg.queries
		m.procRefreshAt = time.Now()
		m.refreshProcesses()
		return m, nil

	case systemStatsMsg:
		m.systemStats = msg.stats
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

	case processTickMsg:
		return m, tea.Batch(
			m.cmdFetchQueries(),
			m.cmdProcessTick(),
		)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

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
	// Confirmation popup takes precedence
	if m.confirmingAction != "" {
		switch msg.String() {
		case "y", "Y":
			callback := m.confirmCallback
			// Reset state before executing
			m.confirmingAction = ""
			m.confirmCallback = nil
			return m, callback()
		case "n", "N", "esc":
			m.confirmingAction = ""
			m.confirmCallback = nil
			m.actionMsg = "Truncate cancelled"
			return m, nil
		default:
			return m, nil // Consume all other keys
		}
	}

	// TTL input consumes all keys while focused.
	if m.tab == tabDashboard && m.tableDetail != nil && m.ttlInput.Focused() {
		switch msg.Type {
		case tea.KeyEsc:
			m.ttlInput.Blur()
			m.refreshDashboard()
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
		// ESC only goes back, never quits the app
		if m.tab == tabDashboard && m.tableDetail != nil {
			m.tableDetail = nil
			m.actionMsg = ""
			m.ttlInput.Blur()
			m.refreshDashboard()
			return m, nil
		}
		// If not in a detail view, ESC does nothing (use 'q' to quit)
		return m, nil
	case tea.KeyTab:
		m.tab = (m.tab + 1) % len(tabNames)
		m.ttlInput.Blur()
		uiLog.Debug().Str("tab", tabNames[m.tab]).Msg("Switched tab")
		// Charger les données immédiatement quand on arrive sur l'onglet History
		if m.tab == tabHistory {
			return m, m.cmdFetchHistory()
		}
		return m, nil
	case tea.KeyEnter:
		switch m.tab {
		case tabFatTables:
			uiLog.Debug().Int("index", m.fatTable.Cursor()).Msg("Selected fat table")
			return m, m.cmdFatTableSelect()
		}
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
	case "q":
		return m, tea.Quit
	case "r":
		uiLog.Debug().Str("tab", tabNames[m.tab]).Msg("Manual refresh triggered")
		return m, m.cmdRefresh()
	case "t":
		if m.tab == tabDashboard && m.tableDetail != nil {
			uiLog.Info().
				Str("database", m.tableDetail.Database).
				Str("table", m.tableDetail.Name).
				Msg("Truncate requested")
			return m, m.cmdTruncate()
		}
		if m.tab == tabFatTables {
			idx := m.fatTable.Cursor()
			if idx < 0 || idx >= len(m.truncatables) {
				return m, nil
			}
			t := m.truncatables[idx]
			if !t.Truncatable {
				m.actionMsg = "Table not marked as safe to truncate"
				return m, nil
			}
			// Afficher immédiatement la modale (pas de commande async)
			m.confirmingAction = "truncate"
			m.confirmDatabase = t.Database
			m.confirmTable = t.Table
			m.confirmTableSize = t.Size
			m.confirmCallback = func() tea.Cmd {
				return m.cmdTruncateSelectedTable()
			}
			return m, nil
		}
	case "l":
		if m.tab == tabDashboard && m.tableDetail != nil {
			uiLog.Debug().
				Str("database", m.tableDetail.Database).
				Str("table", m.tableDetail.Name).
				Msg("TTL edit requested")
			m.ttlInput.Focus()
			m.refreshDashboard()
			return m, textinput.Blink
		}
	}

	return m.updateFocused(msg)
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m *Model) cmdConnect() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
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

// cmdProcessTick schedules the next automatic process-list refresh.
func (m *Model) cmdProcessTick() tea.Cmd {
	return tea.Tick(processRefreshInterval, func(time.Time) tea.Msg {
		return processTickMsg{}
	})
}

// cmdSystemStatsTick schedules the next automatic system stats refresh.
func (m *Model) cmdSystemStatsTick() tea.Cmd {
	return tea.Tick(statsRefreshInterval, func(time.Time) tea.Msg {
		return m.cmdFetchSystemStats()()
	})
}

func (m *Model) cmdFetchSystemStats() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stats, err := m.daemon.GetSystemStats(ctx)
		if err != nil {
			return errMsg{err}
		}
		return systemStatsMsg{stats}
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
	case tabFatTables:
		return m.cmdFetchTruncatables()
	case tabProcesses:
		return m.cmdFetchQueries()
	case tabHistory:
		return m.cmdFetchHistory()
	}
	return nil
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

func (m *Model) cmdTruncateSelectedTable() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := m.daemon.TruncateTable(ctx, m.confirmDatabase, m.confirmTable)
		if err != nil {
			return errMsg{fmt.Errorf("truncate failed: %w", err)}
		}

		// Reset confirmation state
		m.confirmingAction = ""
		m.confirmDatabase = ""
		m.confirmTable = ""
		m.confirmTableSize = ""
		m.confirmCallback = nil

		// Refresh truncatables list
		return actionDoneMsg{fmt.Sprintf("Table %s.%s truncated", m.confirmDatabase, m.confirmTable)}
	}
}

func (m *Model) cmdTruncate() tea.Cmd {
	if m.tableDetail == nil {
		return nil
	}
	db, tbl := m.tableDetail.Database, m.tableDetail.Name
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
	db, tbl, ttl := m.tableDetail.Database, m.tableDetail.Name, m.ttlInput.Value()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.daemon.ModifyTTL(ctx, db, tbl, ttl); err != nil {
			return errMsg{fmt.Errorf("TTL modify failed: %w", err)}
		}
		return actionDoneMsg{fmt.Sprintf("TTL set: %s", ttl)}
	}
}

// ---------------------------------------------------------------------------
// Pane layout
// ---------------------------------------------------------------------------

func (m *Model) resizePanes() {
	h := m.contentHeight()
	w := m.width

	m.dashViewport.Width = w
	m.dashViewport.Height = h

	m.procViewport.Width = w
	m.procViewport.Height = h

	m.histViewport.Width = w
	m.histViewport.Height = h

	m.rebuildFatTable()
}

// contentHeight returns the usable lines between the tab bar and the help bar.
func (m *Model) contentHeight() int {
	// 1 tab bar + 1 stats bar + 1 help bar
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

func (m *Model) rebuildFatTable() {
	w := m.tableWidth()
	dbW := w / 10
	nameW := w / 8
	sizeW := 8
	rowsW := 10
	diskW := 8
	percentW := 8
	ttlW := 12
	partitionW := 12
	truncW := 9
	h := m.contentHeight() - 2
	if h < 1 {
		h = 1
	}
	cur := m.fatTable.Cursor()
	rows := make([]table.Row, 0, len(m.truncatables))
	for _, t := range m.truncatables {
		flag := "no"
		if t.Truncatable {
			flag = "yes"
		}
		ttlDisplay := t.TTL
		if len(ttlDisplay) > 10 {
			ttlDisplay = ttlDisplay[:10] + "..."
		}
		partitionDisplay := t.PartitionKey
		if len(partitionDisplay) > 10 {
			partitionDisplay = partitionDisplay[:10] + "..."
		}
		rows = append(rows, table.Row{
			t.Database, t.Table, t.Size,
			humanize.Comma(int64(t.Rows)),
			t.DiskName,
			fmt.Sprintf("%.1f%%", t.Percent),
			ttlDisplay,
			partitionDisplay,
			flag,
		})
	}
	if cur >= len(rows) {
		cur = 0
	}
	m.fatTable = table.New(
		table.WithColumns([]table.Column{
			{Title: "Database", Width: dbW},
			{Title: "Table", Width: nameW},
			{Title: "Size", Width: sizeW},
			{Title: "Rows", Width: rowsW},
			{Title: "Disk", Width: diskW},
			{Title: "%", Width: percentW},
			{Title: "TTL", Width: ttlW},
			{Title: "Partition", Width: partitionW},
			{Title: "Trunc", Width: truncW},
		}),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
	)
	m.fatTable.SetStyles(tblStyle)
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

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

const asciiLogo = `
    __  __ __      __    __   ____  ______   __  __ __    ___  ____
   /  ]|  |  |    |  |__|  | /    ||      | /  ]|  |  | /  _]|    \
  /  / |  |  |    |  |  |  ||  o  ||      |/  / |  |  | /  [_ |  D  )
 /  /  |  |  |    |  |  |  ||     ||_|  |_/  /  |  _  ||    _]|    /
/   \_ |  |  |    |  '  '  ||  _  |  |  |/   \_ |  |  ||   [_ |    \
\     ||  |  |     \      / |  |  |  |  |\     ||  |  ||     ||  .  \
 \____||__|__|      \_/\_/  |__|__|  |__| \____||__|__||_____||__|\_|
`

func (m *Model) View() string {
	if m.loading || m.connectErr != "" {
		return m.connectView()
	}
	// Show confirmation dialog if active
	if m.confirmingAction != "" {
		return m.renderConfirmationDialog()
	}
	return m.renderTabBar() + "\n" +
		m.renderContent() + "\n" +
		m.renderStatsBar() + "\n" +
		m.renderHelpBar()
}

func (m *Model) connectView() string {
	logo := logoStyle.Align(lipgloss.Center).Width(m.width).Render(asciiLogo)
	var status string
	if m.connectErr != "" {
		// Check if this is a version mismatch error
		if strings.Contains(m.connectErr, "Version mismatch") {
			status = errorStyle.Render("  ✗ Version mismatch") + "\n\n" +
				mutedStyle.Render("  "+m.connectErr) + "\n\n" +
				mutedStyle.Render("  Please ensure clickhouse-watch and clickhouse-watcherd") + "\n" +
				mutedStyle.Render("  are the same version (e.g., rebuild both with 'make build')") + "\n\n" +
				mutedStyle.Render("  Press 'q' to quit")
		} else {
			status = errorStyle.Render("  Connection failed: "+m.connectErr) +
				"\n\n" + mutedStyle.Render("  Press 'q' to quit")
		}
	} else {
		status = mutedStyle.Render("  Connecting to ") +
			dimValueStyle.Render(m.daemon.SocketPath()) +
			"  " + m.spinner.View()
	}
	return logo + "\n\n" + status + "\n"
}

func (m *Model) renderTabBar() string {
	var tabs strings.Builder
	for i, name := range tabNames {
		if i > 0 {
			tabs.WriteString(tabSepStyle.Render("│"))
		}
		if i == m.tab {
			tabs.WriteString(activeTabStyle.Render(name))
		} else {
			tabs.WriteString(inactiveTabStyle.Render(name))
		}
	}
	// pad the remainder of the bar with the surface colour
	return chromeBarStyle.Width(m.width).Render(tabs.String())
}

// renderStatsBar renders a line with three gauges: Disk, CPU, and Memory usage.
func (m *Model) renderStatsBar() string {
	if m.systemStats == nil {
		return chromeBarStyle.Width(m.width).Render("")
	}

	// Helper to render a gauge
	renderGauge := func(label string, percent float64, color lipgloss.Color) string {
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}

		// Calculate filled width (max 20 chars for the bar)
		barWidth := 20
		filled := int(percent / 100.0 * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled

		// Choose color based on percentage
		var barColor lipgloss.Color
		switch {
		case percent >= 80:
			barColor = clrRed
		case percent >= 50:
			barColor = clrYellow
		default:
			barColor = clrGreen
		}

		// Build the bar
		filledBar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
		emptyBar := lipgloss.NewStyle().Foreground(clrDim).Render(strings.Repeat("░", empty))
		bar := filledBar + emptyBar

		// Build the full gauge: Label + bar + percentage
		labelStyled := lipgloss.NewStyle().Foreground(clrText).Bold(true).Render(fmt.Sprintf("%-8s", label))
		percentStyled := lipgloss.NewStyle().Foreground(clrText).Render(fmt.Sprintf(" %5.1f%%", percent))

		return labelStyled + " " + bar + percentStyled
	}

	// Render three gauges side by side
	diskGauge := renderGauge("Disk", m.systemStats.DiskUsagePercent, clrGreen)
	cpuGauge := renderGauge("CPU", m.systemStats.CPUUsagePercent, clrCyan)
	memGauge := renderGauge("Memory", m.systemStats.MemUsagePercent, clrBlue)

	// Combine them with spacing
	gauges := diskGauge + "  " + cpuGauge + "  " + memGauge

	// Center the gauges in the available width
	gaugesWidth := lipgloss.Width(gauges)
	padding := (m.width - gaugesWidth) / 2
	if padding < 0 {
		padding = 0
	}

	leftPad := strings.Repeat(" ", padding)

	return chromeBarStyle.Width(m.width).Render(leftPad + gauges)
}

func (m *Model) renderHelpBar() string {
	var keys []string
	switch m.tab {
	case tabDashboard:
		if m.tableDetail != nil {
			if m.ttlInput.Focused() {
				keys = []string{"Enter:apply", "Esc:cancel", "q:quit"}
			} else {
				keys = []string{"t:truncate", "l:TTL", "Esc:back", "r:refresh", "q:quit"}
			}
		} else {
			keys = []string{"r:refresh", "Tab:next", "q:quit"}
		}
	case tabFatTables:
		keys = []string{"↑↓:select", "Enter:detail", "t:truncate", "r:refresh", "Tab:next", "q:quit"}
	case tabProcesses:
		keys = []string{"↑↓:scroll", "r:refresh", "Tab:next", "q:quit"}
	case tabHistory:
		keys = []string{"↑↓:metric", "←→:period", "r:refresh", "Tab:next", "q:quit"}
	}

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(mutedStyle.Background(clrSurface).Render("  "))
		}
		parts := strings.SplitN(k, ":", 2)
		if len(parts) == 2 {
			b.WriteString(helpKeyStyle.Render(parts[0]))
			b.WriteString(helpBarStyle.Render(" " + parts[1]))
		} else {
			b.WriteString(helpBarStyle.Render(k))
		}
	}

	// right-side: next process refresh countdown
	countdown := ""
	if m.tab == tabProcesses && !m.procRefreshAt.IsZero() {
		next := processRefreshInterval - time.Since(m.procRefreshAt)
		if next < 0 {
			next = 0
		}
		countdown = statusStyle.Render(fmt.Sprintf("  refresh in %ds", int(next.Seconds())))
	}

	left := b.String()
	right := countdown
	// fill gap between left and right
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	fill := helpBarStyle.Render(strings.Repeat(" ", gap))

	return left + fill + right
}

func (m *Model) renderConfirmationDialog() string {
	dialogWidth := 60
	dialogHeight := 10

	title := "Confirm Truncate"
	message := fmt.Sprintf("Are you sure you want to truncate table '%s.%s' (%s)?\n\nThis action cannot be undone.",
		m.confirmDatabase, m.confirmTable, m.confirmTableSize)

	// Styles
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FFD700")).
		Background(clrSurface).
		Padding(2).
		Width(dialogWidth)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFD700")).
		Bold(true).
		MarginBottom(1)

	// Content
	var content strings.Builder
	content.WriteString(titleStyle.Render(title) + "\n")
	content.WriteString(message + "\n\n")
	content.WriteString(helpKeyStyle.Render("y") + helpBarStyle.Render(":yes  ") +
		helpKeyStyle.Render("n") + helpBarStyle.Render(":no  ") +
		helpKeyStyle.Render("esc") + helpBarStyle.Render(":cancel"))

	dialog := dialogStyle.Render(content.String())

	// Center on screen
	paddingTop := (m.contentHeight() - dialogHeight) / 2
	if paddingTop < 0 {
		paddingTop = 0
	}
	paddingLeft := (m.width - dialogWidth) / 2
	if paddingLeft < 0 {
		paddingLeft = 0
	}

	// Semi-transparent overlay effect
	overlayStyle := lipgloss.NewStyle().
		Background(clrSurface).
		Width(m.width).
		Height(m.height)

	// Position the dialog
	positionedDialog := lipgloss.NewStyle().
		MarginTop(paddingTop).
		MarginLeft(paddingLeft).
		Render(dialog)

	return overlayStyle.Render(positionedDialog)
}

func (m *Model) renderContent() string {
	switch m.tab {
	case tabDashboard:
		return m.dashViewport.View()
	case tabFatTables:
		return m.fatTable.View()
	case tabProcesses:
		return m.procViewport.View()
	case tabHistory:
		return m.histViewport.View()
	}
	return ""
}

// ---------------------------------------------------------------------------
// Content renderers
// ---------------------------------------------------------------------------

func (m *Model) renderDashboardContent() string {
	if m.tableDetail != nil {
		return m.renderTableDetailContent()
	}
	return m.renderMetricsContent()
}

func (m *Model) renderMetricsContent() string {
	var b strings.Builder

	title := sectionStyle.Width(m.width - 2).Render(" System Metrics")
	b.WriteString(title + "\n\n")

	if m.metrics == nil {
		b.WriteString(mutedStyle.Render("  No metrics available") + "\n")
		return b.String()
	}

	// Two-column card layout.
	type kv struct{ label, value string }
	left := []kv{
		{"Server Version", m.metrics.Version},
		{"Tool Version", version.Version()},
		{"Uptime", formatDuration(m.metrics.Uptime)},
		{"Total Rows", humanize.Comma(int64(m.metrics.TotalRows))},
	}
	right := []kv{
		{"Total Bytes", humanize.Bytes(m.metrics.TotalBytes)},
		{"Background Pools", humanize.Comma(int64(m.metrics.BackgroundPools))},
		{"Max Parts / Partition", humanize.Comma(int64(m.metrics.MaxPartsInPartition))},
	}

	colW := (m.width / 2) - 4
	if colW < 20 {
		colW = 20
	}

	renderCard := func(items []kv) string {
		var s strings.Builder
		for _, item := range items {
			s.WriteString(labelStyle.Render(fmt.Sprintf("  %-22s", item.label)))
			s.WriteString(valueStyle.Render(item.value))
			s.WriteString("\n")
		}
		return cardStyle.Width(colW).Render(s.String())
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top,
		renderCard(left),
		lipgloss.NewStyle().Width(2).Render(""),
		renderCard(right),
	)
	b.WriteString(row + "\n")
	return b.String()
}

func (m *Model) renderTableDetailContent() string {
	var b strings.Builder

	title := sectionStyle.Width(m.width - 2).Render(
		fmt.Sprintf(" %s.%s", m.tableDetail.Database, m.tableDetail.Name),
	)
	b.WriteString(title + "\n\n")

	// Find table info from truncatables cache
	var tableInfo *clickhouse.TruncatableTable
	for _, t := range m.truncatables {
		if t.Database == m.tableDetail.Database && t.Table == m.tableDetail.Name {
			tableInfo = &t
			break
		}
	}

	type kv struct{ label, value string }
	fields := []kv{
		{"Database", m.tableDetail.Database},
		{"Table", m.tableDetail.Name},
	}

	if tableInfo != nil {
		fields = append(fields, kv{"Size", tableInfo.Size})
		fields = append(fields, kv{"Rows", humanize.Comma(int64(tableInfo.Rows))})
		fields = append(fields, kv{"Disk", tableInfo.DiskName})
		fields = append(fields, kv{"Disk %", fmt.Sprintf("%.2f%%", tableInfo.Percent)})
		fields = append(fields, kv{"First Date", tableInfo.First})
		fields = append(fields, kv{"Last Date", tableInfo.Last})
		fields = append(fields, kv{"Duration", tableInfo.Duration})
		fields = append(fields, kv{"Age", tableInfo.Age})
		if tableInfo.TTL != "" {
			fields = append(fields, kv{"TTL", tableInfo.TTL})
		}
		if tableInfo.PartitionKey != "" {
			fields = append(fields, kv{"Partition Key", tableInfo.PartitionKey})
		}
		if tableInfo.Truncatable {
			fields = append(fields, kv{"Truncatable", "Yes"})
		} else {
			fields = append(fields, kv{"Truncatable", "No"})
		}
	}

	if m.tableDetail.Engine != "" {
		fields = append(fields, kv{"Engine", m.tableDetail.Engine})
	}
	if m.tableDetail.SortingKey != "" {
		fields = append(fields, kv{"Sorting Key", m.tableDetail.SortingKey})
	}
	if m.tableDetail.TTL != "" {
		fields = append(fields, kv{"Current TTL", m.tableDetail.TTL})
	}

	var info strings.Builder
	for _, f := range fields {
		info.WriteString(labelStyle.Render(fmt.Sprintf("  %-16s", f.label)))
		info.WriteString(dimValueStyle.Render(f.value) + "\n")
	}
	b.WriteString(cardStyle.Width(m.width-4).Render(info.String()) + "\n\n")

	// TTL input section
	b.WriteString(sectionStyle.Width(m.width-2).Render(" Modify TTL") + "\n\n")
	b.WriteString("  " + m.ttlInput.View() + "\n")

	if m.actionMsg != "" {
		b.WriteString("\n  " + valueStyle.Render("✓ "+m.actionMsg) + "\n")
	}
	if m.connectErr != "" {
		b.WriteString("\n  " + errorStyle.Render("✗ "+m.connectErr) + "\n")
	}
	return b.String()
}

func (m *Model) renderProcessesContent() string {
	var b strings.Builder

	// Header with last-refreshed time
	var ts string
	if !m.procRefreshAt.IsZero() {
		ts = mutedStyle.Render("  last updated " + m.procRefreshAt.Format("15:04:05"))
	}
	title := sectionStyle.Width(m.width - 2 - lipgloss.Width(ts)).Render(" Running Processes")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Bottom, title, ts) + "\n\n")

	if len(m.queries) == 0 {
		b.WriteString(mutedStyle.Render("  No running queries") + "\n")
		return b.String()
	}

	cardW := m.width - 4
	if cardW < 20 {
		cardW = 20
	}

	for i, q := range m.queries {
		var card strings.Builder

		// Query text
		card.WriteString(queryNumStyle.Render(fmt.Sprintf("[%d] ", i+1)))
		card.WriteString(queryTextStyle.Render(truncateStr(q.Query, cardW-6)) + "\n")

		// Stats row
		stats := fmt.Sprintf("rows: %-12s  read: %-10s  mem: %s",
			humanize.Comma(int64(q.RowsRead)),
			humanize.Bytes(q.BytesRead),
			humanize.Bytes(q.MemoryUsage),
		)
		card.WriteString(mutedStyle.Render("    "+stats) + "\n")

		// Memory bar (max display: 1 GB)
		if q.MemoryUsage > 0 {
			card.WriteString("    " + memBar(q.MemoryUsage, 1<<30, cardW-8) + "\n")
		}

		b.WriteString(queryCardStyle.Width(cardW).Render(card.String()) + "\n")
	}
	return b.String()
}

func (m *Model) renderHistoryContent() string {
	var b strings.Builder

	metric := historyMetrics[m.histMetricIdx]

	title := sectionStyle.Width(m.width - 2).Render(" Metrics History")
	b.WriteString(title + "\n\n")

	// Selector row
	b.WriteString(renderSelector("Metric", historyMetrics, m.histMetricIdx))
	b.WriteString(renderSelector("Period", historyPeriods, m.histPeriodIdx))
	b.WriteString("\n")

	if len(m.historyData) == 0 {
		b.WriteString(mutedStyle.Render("  No historical data yet — collected every 2 minutes.") + "\n")
		return b.String()
	}

	// Find max value for the sparkline bar.
	var maxVal int64
	for _, s := range m.historyData {
		if s.Value > maxVal {
			maxVal = s.Value
		}
	}

	barW := 24

	// Build sparkline values array (most recent first)
	values := make([]int64, len(m.historyData))
	for i, s := range m.historyData {
		values[i] = s.Value
	}

	// Display sparkline chart at the top
	spark := sparkline(values, maxVal, barW)
	b.WriteString(mutedStyle.Render("  Trend: ") + spark + "\n\n")

	b.WriteString(mutedStyle.Render(fmt.Sprintf("  %-20s  %-20s  %s\n",
		"Timestamp", "Value", strings.Repeat("▒", barW))))
	b.WriteString(mutedStyle.Render("  " + strings.Repeat("─", 20+2+20+2+barW) + "\n"))

	for _, s := range m.historyData {
		val := formatHistoryValue(metric, s.Value)
		bar := sparkBar(s.Value, maxVal, barW)
		b.WriteString(
			labelStyle.Render(fmt.Sprintf("  %-20s", s.At.Format("2006-01-02 15:04:05"))) +
				valueStyle.Render(fmt.Sprintf("  %-20s", val)) +
				bar + "\n",
		)
	}
	return b.String()
}

// renderSelector renders a labelled row of selectable options, with the active
// one highlighted.
func renderSelector(label string, options []string, active int) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(fmt.Sprintf("  %-8s", label+":")))
	for i, opt := range options {
		if i == active {
			b.WriteString(activeTabStyle.Render(" " + opt + " "))
		} else {
			b.WriteString(inactiveTabStyle.Render(" " + opt + " "))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Visual helpers
// ---------------------------------------------------------------------------

// sparkBar renders a coloured horizontal bar scaled to [0, maxVal].
func sparkBar(val, maxVal int64, width int) string {
	if maxVal <= 0 || width <= 0 {
		return ""
	}
	filled := int(float64(val) / float64(maxVal) * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	// colour: green → yellow → red based on proportion
	ratio := float64(val) / float64(maxVal)
	var style lipgloss.Style
	switch {
	case ratio >= 0.75:
		style = lipgloss.NewStyle().Foreground(clrRed)
	case ratio >= 0.40:
		style = lipgloss.NewStyle().Foreground(clrYellow)
	default:
		style = lipgloss.NewStyle().Foreground(clrGreen)
	}
	return style.Render(bar)
}

// sparkline renders a mini chart using Unicode block characters (▁▂▃▄▅▆▇█).
// It shows the trend of values over time in a compact form.
func sparkline(values []int64, maxVal int64, width int) string {
	if len(values) == 0 || maxVal <= 0 || width <= 0 {
		return strings.Repeat(" ", width)
	}

	blocks := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

	var b strings.Builder
	for i := 0; i < width && i < len(values); i++ {
		val := values[i]
		ratio := float64(val) / float64(maxVal)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		// Map ratio to 0-7 index
		idx := int(ratio * 7)
		b.WriteString(blocks[idx])
	}

	return b.String()
}

// memBar renders a small memory-usage progress bar.
func memBar(used, total uint64, width int) string {
	if total == 0 || width <= 0 {
		return ""
	}
	return sparkBar(int64(used), int64(total), width)
}

// formatDuration pretty-prints a duration with days when ≥24 h.
func formatDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd %dh", days, h)
	}
	return d.Truncate(time.Second).String()
}

func formatHistoryValue(metric string, v int64) string {
	switch metric {
	case "total_bytes":
		return humanize.Bytes(uint64(v))
	case "total_rows":
		return humanize.Comma(v) + " rows"
	case "disk_usage", "cpu_usage", "memory_usage":
		return fmt.Sprintf("%d%%", v)
	case "ingestion":
		return humanize.Bytes(uint64(v)) + "/min"
	case "users":
		return fmt.Sprintf("%d users", v)
	case "errors":
		return fmt.Sprintf("%d errors/min", v)
	}
	return humanize.Comma(v)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
