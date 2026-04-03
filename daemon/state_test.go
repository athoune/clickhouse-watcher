package daemon

import (
	"testing"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
)

func TestNewState(t *testing.T) {
	conn := clickhouse.Connection{Host: "localhost", Port: 9000}
	s := NewState(conn, "")
	if s == nil {
		t.Fatal("NewState returned nil")
	}
	if s.IsConnected() {
		t.Error("new state should not be connected")
	}
}

func TestStateGettersReturnNilWhenEmpty(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")

	if s.GetMetrics() != nil {
		t.Error("GetMetrics should return nil before any poll")
	}
	if s.GetTables() != nil {
		t.Error("GetTables should return nil before any poll")
	}
	if s.GetQueries() != nil {
		t.Error("GetQueries should return nil before any poll")
	}
	if s.GetLastError() != "" {
		t.Error("GetLastError should be empty initially")
	}
}

func TestStatePollWithoutClient(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	err := s.Poll()
	if err == nil {
		t.Error("Poll should fail when not connected")
	}
}

func TestStateTruncateWithoutClient(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	err := s.TruncateTable(nil, "db", "tbl") //nolint:staticcheck
	if err == nil {
		t.Error("TruncateTable should fail when not connected")
	}
}

func TestStateModifyTTLWithoutClient(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	err := s.ModifyTTL(nil, "db", "tbl", "") //nolint:staticcheck
	if err == nil {
		t.Error("ModifyTTL should fail when not connected")
	}
}

func TestStateExecuteQueryWithoutClient(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	_, err := s.ExecuteQuery(nil, "SELECT 1") //nolint:staticcheck
	if err == nil {
		t.Error("ExecuteQuery should fail when not connected")
	}
}

func TestStateGetTruncatablesWithoutClient(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	_, err := s.GetTruncatableTables(nil) //nolint:staticcheck
	if err == nil {
		t.Error("GetTruncatableTables should fail when not connected")
	}
}

// ---------------------------------------------------------------------------
// QueryHistory routing — no ClickHouse needed; we inject RRDs directly.
// ---------------------------------------------------------------------------

func stateWithRRDs(t *testing.T) *State {
	t.Helper()
	s := NewState(clickhouse.Connection{}, "")

	var err error
	s.rrdTotalBytes, err = rrd.New("")
	if err != nil {
		t.Fatalf("rrd.New failed: %v", err)
	}
	s.rrdTotalRows, err = rrd.New("")
	if err != nil {
		t.Fatalf("rrd.New failed: %v", err)
	}

	// Push a sample into each RRD so QueryDay returns non-empty slices.
	_ = s.rrdTotalBytes.Record(4096)
	_ = s.rrdTotalRows.Record(100)
	return s
}

func TestQueryHistoryTotalBytesDay(t *testing.T) {
	s := stateWithRRDs(t)
	samples, err := s.QueryHistory("total_bytes", "day")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) == 0 {
		t.Error("expected at least one sample")
	}
}

func TestQueryHistoryTotalRowsWeek(t *testing.T) {
	s := stateWithRRDs(t)
	samples, err := s.QueryHistory("total_rows", "week")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = samples
}

func TestQueryHistoryInvalidMetric(t *testing.T) {
	s := stateWithRRDs(t)
	_, err := s.QueryHistory("nonexistent", "day")
	if err == nil {
		t.Error("expected error for unknown metric")
	}
}

func TestQueryHistoryInvalidPeriod(t *testing.T) {
	s := stateWithRRDs(t)
	_, err := s.QueryHistory("total_bytes", "decade")
	if err == nil {
		t.Error("expected error for unknown period")
	}
}

func TestQueryHistoryNoRRD(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	_, err := s.QueryHistory("total_bytes", "day")
	if err == nil {
		t.Error("expected error when RRD is not initialised")
	}
}

// ---------------------------------------------------------------------------
// StartRRD exits silently when rrdTotalBytes is nil.
// ---------------------------------------------------------------------------

func TestStartRRDWithNilRRD(t *testing.T) {
	s := NewState(clickhouse.Connection{}, "")
	// Should not panic.
	s.StartRRD(nil) //nolint:staticcheck
}
