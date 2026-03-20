package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
)

type State struct {
	mu        sync.RWMutex
	conn      clickhouse.Connection
	client    *clickhouse.Client
	metrics   *clickhouse.SystemMetrics
	tables    []clickhouse.TableMetric
	queries   []clickhouse.QueryMetric
	connected bool
	lastError string

	rrdTotalBytes *rrd.RRD
	rrdTotalRows  *rrd.RRD
	rrdUptime     *rrd.RRD
	dataDir       string
}

func NewState(conn clickhouse.Connection, dataDir string) *State {
	return &State{
		conn:    conn,
		dataDir: dataDir,
	}
}

func (s *State) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := clickhouse.NewClient(s.conn)
	if err != nil {
		s.lastError = fmt.Sprintf("connection failed: %v", err)
		s.connected = false
		return err
	}

	s.client = client
	s.connected = true
	s.lastError = ""

	if s.dataDir != "" {
		s.initRRD()
	}

	return nil
}

func (s *State) initRRD() {
	os.MkdirAll(s.dataDir, 0755)

	s.rrdTotalBytes, _ = rrd.New(filepath.Join(s.dataDir, "total_bytes.rrd"))
	s.rrdTotalRows, _ = rrd.New(filepath.Join(s.dataDir, "total_rows.rrd"))
	s.rrdUptime, _ = rrd.New(filepath.Join(s.dataDir, "uptime.rrd"))
}

func (s *State) StartRRD(ctx context.Context) {
	if s.rrdTotalBytes == nil {
		return
	}

	collector := func() (int64, error) {
		s.mu.RLock()
		m := s.metrics
		s.mu.RUnlock()
		if m == nil {
			return 0, fmt.Errorf("no metrics")
		}
		return int64(m.TotalBytes), nil
	}
	s.rrdTotalBytes.StartScheduler(ctx, collector)

	collectorRows := func() (int64, error) {
		s.mu.RLock()
		m := s.metrics
		s.mu.RUnlock()
		if m == nil {
			return 0, fmt.Errorf("no metrics")
		}
		return int64(m.TotalRows), nil
	}
	s.rrdTotalRows.StartScheduler(ctx, collectorRows)

	collectorUptime := func() (int64, error) {
		s.mu.RLock()
		m := s.metrics
		s.mu.RUnlock()
		if m == nil {
			return 0, fmt.Errorf("no metrics")
		}
		return int64(m.Uptime.Seconds()), nil
	}
	s.rrdUptime.StartScheduler(ctx, collectorUptime)
}

func (s *State) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
	s.connected = false
	return nil
}

func (s *State) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

func (s *State) GetMetrics() *clickhouse.SystemMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}

func (s *State) GetTables() []clickhouse.TableMetric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tables
}

func (s *State) GetQueries() []clickhouse.QueryMetric {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queries
}

func (s *State) GetLastError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastError
}

func (s *State) Poll() error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metrics, err := client.GetSystemMetrics(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastError = fmt.Sprintf("metrics: %v", err)
		s.mu.Unlock()
		return err
	}

	tables, err := client.GetTableMetrics(ctx, 50)
	if err != nil {
		s.mu.Lock()
		s.lastError = fmt.Sprintf("tables: %v", err)
		s.mu.Unlock()
		return err
	}

	queries, err := client.GetRunningQueries(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastError = fmt.Sprintf("queries: %v", err)
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	s.metrics = metrics
	s.tables = tables
	s.queries = queries
	s.lastError = ""
	s.mu.Unlock()

	return nil
}

func (s *State) TruncateTable(ctx context.Context, database, table string) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	return client.TruncateTable(ctx, database, table)
}

func (s *State) ModifyTTL(ctx context.Context, database, table, newTTL string) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	return client.ModifyTTL(ctx, database, table, newTTL)
}

func (s *State) ExecuteQuery(ctx context.Context, query string) (*clickhouse.QueryResult, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	return client.ExecuteQuery(ctx, query)
}

func (s *State) QueryHistory(metric string, period string) []rrd.Sample {
	switch metric {
	case "total_bytes":
		switch period {
		case "day":
			return s.rrdTotalBytes.QueryDay()
		case "week":
			return s.rrdTotalBytes.QueryWeek()
		case "month":
			return s.rrdTotalBytes.QueryMonth()
		}
	case "total_rows":
		switch period {
		case "day":
			return s.rrdTotalRows.QueryDay()
		case "week":
			return s.rrdTotalRows.QueryWeek()
		case "month":
			return s.rrdTotalRows.QueryMonth()
		}
	case "uptime":
		switch period {
		case "day":
			return s.rrdUptime.QueryDay()
		case "week":
			return s.rrdUptime.QueryWeek()
		case "month":
			return s.rrdUptime.QueryMonth()
		}
	}
	return nil
}
