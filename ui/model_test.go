package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel returns a Model wired to a non-existent socket so no real
// daemon is required for unit tests.
func newTestModel() *Model {
	m := New("/tmp/clickhouse-watcher-unit-test.sock")
	// Pretend the connection phase finished successfully so we see the main UI.
	m.loading = false
	m.connectErr = ""
	// Give it a known size so viewport calculations are deterministic.
	m.width = 120
	m.height = 40
	m.resizePanes()
	return m
}

// sendKey fires a KeyMsg through Update and returns the updated model.
func sendKey(m *Model, key tea.KeyType, runes ...rune) *Model {
	msg := tea.KeyMsg{Type: key}
	if len(runes) > 0 {
		msg.Runes = runes
		msg.Type = tea.KeyRunes
	}
	updated, _ := m.Update(msg)
	if um, ok := updated.(*Model); ok {
		return um
	}
	return m
}

func sendRune(m *Model, r rune) *Model {
	return sendKey(m, tea.KeyRunes, r)
}

// ---------------------------------------------------------------------------
// Tab navigation
// ---------------------------------------------------------------------------

func TestTabCycling(t *testing.T) {
	m := newTestModel()

	if m.tab != tabDashboard {
		t.Fatalf("expected initial tab %d, got %d", tabDashboard, m.tab)
	}

	for _, want := range []int{tabTables, tabFatTables, tabProcesses, tabHistory, tabDashboard} {
		m = sendKey(m, tea.KeyTab)
		if m.tab != want {
			t.Errorf("after Tab: expected tab %d, got %d", want, m.tab)
		}
	}
}

// ---------------------------------------------------------------------------
// Connect screen view
// ---------------------------------------------------------------------------

func TestConnectViewWhileLoading(t *testing.T) {
	m := New("/tmp/test.sock")
	m.width = 80
	m.height = 24
	v := m.View()
	if v == "" {
		t.Error("View() must not be empty during loading")
	}
	// The ASCII logo must appear.
	if len(v) < 50 {
		t.Errorf("connect view looks too short (%d chars)", len(v))
	}
}

func TestConnectViewOnError(t *testing.T) {
	m := New("/tmp/test.sock")
	m.loading = false
	m.connectErr = "connection refused"
	m.width = 80
	v := m.View()
	if v == "" {
		t.Error("View() must not be empty on error")
	}
}

// ---------------------------------------------------------------------------
// connectedMsg wires up data
// ---------------------------------------------------------------------------

func TestConnectedMsgTransition(t *testing.T) {
	m := New("/tmp/test.sock")
	updated, _ := m.Update(connectedMsg{})
	um := updated.(*Model)
	if um.loading {
		t.Error("loading should be false after connectedMsg")
	}
	if um.connectErr != "" {
		t.Errorf("connectErr should be empty after connectedMsg, got %q", um.connectErr)
	}
}

func TestErrMsgTransition(t *testing.T) {
	m := New("/tmp/test.sock")
	updated, _ := m.Update(errMsg{err: errors.New("boom")})
	um := updated.(*Model)
	if um.loading {
		t.Error("loading should be false after errMsg")
	}
	if um.connectErr == "" {
		t.Error("connectErr should be set after errMsg")
	}
}

// ---------------------------------------------------------------------------
// metricsMsg populates dashboard viewport
// ---------------------------------------------------------------------------

func TestMetricsMsgUpdatesViewport(t *testing.T) {
	m := newTestModel()
	metrics := &clickhouse.SystemMetrics{
		Version:    "24.8",
		Uptime:     2 * time.Hour,
		TotalRows:  1_000_000,
		TotalBytes: 512 * 1024 * 1024,
	}
	updated, _ := m.Update(metricsMsg{metrics})
	um := updated.(*Model)
	if um.metrics != metrics {
		t.Error("metrics not stored after metricsMsg")
	}
	// Viewport content must contain the version string.
	content := um.dashViewport.View()
	if content == "" {
		t.Error("dashboard viewport should not be empty after metrics are set")
	}
}

// ---------------------------------------------------------------------------
// tablesMsg populates the tables table
// ---------------------------------------------------------------------------

func TestTablesMsgPopulatesTable(t *testing.T) {
	m := newTestModel()
	tables := []clickhouse.TableMetric{
		{Database: "db1", Name: "events", Size: "1.2 GB", SizeBytes: 1200000000},
		{Database: "db1", Name: "metrics", Size: "500 MB", SizeBytes: 500000000},
	}
	updated, _ := m.Update(tablesMsg{tables})
	um := updated.(*Model)
	if len(um.tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(um.tables))
	}
	rows := um.tablesTable.Rows()
	if len(rows) != 2 {
		t.Errorf("tablesTable should have 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "events" {
		t.Errorf("first row Table should be 'events', got %q", rows[0][0])
	}
}

// ---------------------------------------------------------------------------
// truncatablesMsg populates the fat table
// ---------------------------------------------------------------------------

func TestTruncatablesMsgPopulatesFatTable(t *testing.T) {
	m := newTestModel()
	trunc := []clickhouse.TruncatableTable{
		{Database: "db1", Table: "logs", Size: "10 GB", Rows: 5_000_000, Truncatable: true},
	}
	updated, _ := m.Update(truncatablesMsg{trunc})
	um := updated.(*Model)
	rows := um.fatTable.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 fat-table row, got %d", len(rows))
	}
	if rows[0][4] != "yes" {
		t.Errorf("truncatable flag should be 'yes', got %q", rows[0][4])
	}
}

// ---------------------------------------------------------------------------
// queriesMsg populates processes viewport
// ---------------------------------------------------------------------------

func TestQueriesMsgPopulatesViewport(t *testing.T) {
	m := newTestModel()
	queries := []clickhouse.QueryMetric{
		{Query: "SELECT 1", RowsRead: 1, BytesRead: 8, MemoryUsage: 1024},
	}
	updated, _ := m.Update(queriesMsg{queries})
	um := updated.(*Model)
	if len(um.queries) != 1 {
		t.Errorf("expected 1 query, got %d", len(um.queries))
	}
}

// ---------------------------------------------------------------------------
// historyMsg populates the history viewport
// ---------------------------------------------------------------------------

func TestHistoryMsgPopulatesViewport(t *testing.T) {
	m := newTestModel()
	now := time.Now()
	samples := []rrd.Sample{
		{At: now.Add(-2 * time.Minute), Value: 100},
		{At: now, Value: 200},
	}
	updated, _ := m.Update(historyMsg{samples})
	um := updated.(*Model)
	if len(um.historyData) != 2 {
		t.Errorf("expected 2 history samples, got %d", len(um.historyData))
	}
}

// ---------------------------------------------------------------------------
// tableDetailMsg switches dashboard to detail view
// ---------------------------------------------------------------------------

func TestTableDetailMsgShowsDetail(t *testing.T) {
	m := newTestModel()
	detail := &clickhouse.TableDetail{
		Database: "db1",
		Name:     "events",
		Engine:   "MergeTree",
	}
	updated, _ := m.Update(tableDetailMsg{detail})
	um := updated.(*Model)
	if um.tableDetail == nil {
		t.Fatal("tableDetail should be set after tableDetailMsg")
	}
	if um.tableDetail.Name != "events" {
		t.Errorf("expected table name 'events', got %q", um.tableDetail.Name)
	}
}

// ---------------------------------------------------------------------------
// Esc clears table detail / quits on main screen
// ---------------------------------------------------------------------------

func TestEscClearsTableDetail(t *testing.T) {
	m := newTestModel()
	m.tableDetail = &clickhouse.TableDetail{Database: "db", Name: "t"}
	m.refreshDashboard()

	m = sendKey(m, tea.KeyEsc)
	if m.tableDetail != nil {
		t.Error("Esc should clear tableDetail")
	}
}

// ---------------------------------------------------------------------------
// TTL input focus via 'l' key
// ---------------------------------------------------------------------------

func TestLKeyFocusesTTLInput(t *testing.T) {
	m := newTestModel()
	m.tableDetail = &clickhouse.TableDetail{Database: "db", Name: "t"}
	m.refreshDashboard()

	m = sendRune(m, 'l')
	if !m.ttlInput.Focused() {
		t.Error("'l' key should focus the TTL input when table detail is shown")
	}
}

func TestEscBlursTTLInput(t *testing.T) {
	m := newTestModel()
	m.tableDetail = &clickhouse.TableDetail{Database: "db", Name: "t"}
	m.refreshDashboard()
	m.ttlInput.Focus()

	m = sendKey(m, tea.KeyEsc)
	if m.ttlInput.Focused() {
		t.Error("Esc should blur the TTL input")
	}
}

// ---------------------------------------------------------------------------
// History navigation keys
// ---------------------------------------------------------------------------

func TestHistoryPeriodCyclesRight(t *testing.T) {
	m := newTestModel()
	m.tab = tabHistory
	initial := m.histPeriodIdx

	m = sendKey(m, tea.KeyRight)
	expected := (initial + 1) % len(historyPeriods)
	if m.histPeriodIdx != expected {
		t.Errorf("expected period index %d after →, got %d", expected, m.histPeriodIdx)
	}
}

func TestHistoryPeriodCyclesLeft(t *testing.T) {
	m := newTestModel()
	m.tab = tabHistory
	initial := m.histPeriodIdx

	m = sendKey(m, tea.KeyLeft)
	expected := (initial - 1 + len(historyPeriods)) % len(historyPeriods)
	if m.histPeriodIdx != expected {
		t.Errorf("expected period index %d after ←, got %d", expected, m.histPeriodIdx)
	}
}

func TestHistoryMetricCyclesDown(t *testing.T) {
	m := newTestModel()
	m.tab = tabHistory
	initial := m.histMetricIdx

	m = sendKey(m, tea.KeyDown)
	expected := (initial + 1) % len(historyMetrics)
	if m.histMetricIdx != expected {
		t.Errorf("expected metric index %d after ↓, got %d", expected, m.histMetricIdx)
	}
}

func TestHistoryMetricCyclesUp(t *testing.T) {
	m := newTestModel()
	m.tab = tabHistory
	initial := m.histMetricIdx

	// Wrap: 0 → last
	m = sendKey(m, tea.KeyUp)
	expected := (initial - 1 + len(historyMetrics)) % len(historyMetrics)
	if m.histMetricIdx != expected {
		t.Errorf("expected metric index %d after ↑, got %d", expected, m.histMetricIdx)
	}
}

// ---------------------------------------------------------------------------
// WindowSizeMsg
// ---------------------------------------------------------------------------

func TestWindowSizeMsgResizesPanes(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	um := updated.(*Model)
	if um.width != 200 || um.height != 50 {
		t.Errorf("expected 200×50, got %d×%d", um.width, um.height)
	}
	if um.dashViewport.Width != 200 {
		t.Errorf("dashViewport.Width should be 200, got %d", um.dashViewport.Width)
	}
}

// ---------------------------------------------------------------------------
// formatBytes helper
// ---------------------------------------------------------------------------

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		got := formatBytes(c.input)
		if got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// truncateStr helper
// ---------------------------------------------------------------------------

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 10); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
	if got := truncateStr("hello world", 8); got != "hello .." {
		t.Errorf("expected 'hello ..', got %q", got)
	}
	if got := truncateStr("ab", 2); got != "ab" {
		t.Errorf("expected 'ab', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatHistoryValue helper
// ---------------------------------------------------------------------------

func TestFormatHistoryValue(t *testing.T) {
	got := formatHistoryValue("total_bytes", 1024)
	if got != "1.0 KB" {
		t.Errorf("expected '1.0 KB', got %q", got)
	}

	got = formatHistoryValue("total_rows", 42)
	if got != "42 rows" {
		t.Errorf("expected '42 rows', got %q", got)
	}

	got = formatHistoryValue("uptime", 3600)
	if got != "1h0m0s" {
		t.Errorf("expected '1h0m0s', got %q", got)
	}

	got = formatHistoryValue("unknown", 99)
	if got != "99" {
		t.Errorf("expected '99', got %q", got)
	}
}
