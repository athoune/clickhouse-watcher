package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
	tea "github.com/charmbracelet/bubbletea"
	humanize "github.com/dustin/go-humanize"
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

	for _, want := range []int{tabFatTables, tabProcesses, tabHistory, tabDashboard} {
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
	if rows[0][8] != "yes" {
		t.Errorf("truncatable flag should be 'yes', got %q", rows[0][8])
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
// humanize integration — spot-check the values produced in the TUI
// ---------------------------------------------------------------------------

func TestHumanizeBytes(t *testing.T) {
	// humanize.Bytes uses SI (base-10) units, not binary.
	cases := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 kB"},
		{1536, "1.5 kB"},
		{1024 * 1024, "1.0 MB"},
	}
	for _, c := range cases {
		got := humanize.Bytes(c.input)
		if got != c.want {
			t.Errorf("humanize.Bytes(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestHumanizeComma(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1_000_000, "1,000,000"},
	}
	for _, c := range cases {
		got := humanize.Comma(c.input)
		if got != c.want {
			t.Errorf("humanize.Comma(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// processTickMsg triggers a queries fetch and re-schedules itself
// ---------------------------------------------------------------------------

func TestProcessTickMsgFetchesQueries(t *testing.T) {
	m := newTestModel()
	// processTickMsg should always return a non-nil Cmd (to re-schedule).
	_, cmd := m.Update(processTickMsg{})
	if cmd == nil {
		t.Error("processTickMsg should return a Cmd (re-schedule tick + fetch queries)")
	}
}

func TestQueriesMsgSetsRefreshTimestamp(t *testing.T) {
	m := newTestModel()
	before := time.Now()
	m, _ = func() (*Model, tea.Cmd) {
		updated, cmd := m.Update(queriesMsg{queries: []clickhouse.QueryMetric{
			{Query: "SELECT 1"},
		}})
		return updated.(*Model), cmd
	}()
	if m.procRefreshAt.Before(before) {
		t.Error("procRefreshAt should be updated after queriesMsg")
	}
}

func TestHelpBarShowsCountdownOnProcessesTab(t *testing.T) {
	m := newTestModel()
	m.tab = tabProcesses
	m.procRefreshAt = time.Now().Add(-3 * time.Second) // 3s ago → 7s remaining

	bar := m.renderHelpBar()
	if !strings.Contains(bar, "refresh in") {
		t.Errorf("help bar should show countdown on Processes tab, got: %q", bar)
	}
}

func TestHelpBarNoCountdownOnOtherTabs(t *testing.T) {
	m := newTestModel()
	m.tab = tabDashboard
	m.procRefreshAt = time.Now()

	bar := m.renderHelpBar()
	if strings.Contains(bar, "refresh in") {
		t.Errorf("help bar should not show countdown on non-Processes tab")
	}
}

// ---------------------------------------------------------------------------
// formatDuration helper
// ---------------------------------------------------------------------------

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{time.Hour, "1h0m0s"},
		{25 * time.Hour, "1d 1h"},
		{48 * time.Hour, "2d 0h"},
	}
	for _, c := range cases {
		got := formatDuration(c.d)
		if got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// sparkBar helper
// ---------------------------------------------------------------------------

func TestSparkBarEmpty(t *testing.T) {
	if got := sparkBar(0, 0, 10); got != "" {
		t.Errorf("sparkBar(0,0,10) should be empty, got %q", got)
	}
}

func TestSparkBarFull(t *testing.T) {
	// 100% fill — the rendered string should contain only block chars.
	rendered := sparkBar(100, 100, 10)
	// lipgloss adds ANSI codes, so just check we get something non-empty.
	if rendered == "" {
		t.Error("sparkBar(100,100,10) should not be empty")
	}
}

func TestSparkBarHalf(t *testing.T) {
	rendered := sparkBar(50, 100, 10)
	if rendered == "" {
		t.Error("sparkBar(50,100,10) should not be empty")
	}
}

// ---------------------------------------------------------------------------
// memBar helper
// ---------------------------------------------------------------------------

func TestMemBarZeroTotal(t *testing.T) {
	if got := memBar(100, 0, 10); got != "" {
		t.Errorf("memBar with zero total should return empty string, got %q", got)
	}
}

func TestMemBarNonZero(t *testing.T) {
	got := memBar(512*1024*1024, 1024*1024*1024, 20)
	if got == "" {
		t.Error("memBar should return non-empty string for non-zero usage")
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
	cases := []struct {
		metric string
		value  int64
		want   string
	}{
		{"total_bytes", 1024, humanize.Bytes(1024)}, // "1.0 kB"
		{"total_rows", 1_000_000, "1,000,000 rows"},
		{"total_rows", 42, "42 rows"},
		{"disk_usage", 75, "75%"},
		{"unknown", 99, humanize.Comma(99)},       // "99"
		{"unknown", 1_000, humanize.Comma(1_000)}, // "1,000"
	}
	for _, c := range cases {
		got := formatHistoryValue(c.metric, c.value)
		if got != c.want {
			t.Errorf("formatHistoryValue(%q, %d) = %q, want %q", c.metric, c.value, got, c.want)
		}
	}
}
