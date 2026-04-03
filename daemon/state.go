package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/rrd"
)

var stateLog = logger.WithComponent("state")

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
	rrdDiskUsage  *rrd.RRD
	rrdCPUUsage   *rrd.RRD
	rrdMemUsage   *rrd.RRD
	rrdIngestion  *rrd.RRD
	rrdUsers      *rrd.RRD
	rrdErrors     *rrd.RRD
	dataDir       string
}

func NewState(conn clickhouse.Connection, dataDir string) *State {
	stateLog.Debug().
		Str("host", conn.Host).
		Int("port", conn.Port).
		Str("database", conn.Database).
		Str("data_dir", dataDir).
		Msg("Creating new state")

	return &State{
		conn:    conn,
		dataDir: dataDir,
	}
}

func (s *State) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stateLog.Info().
		Str("host", s.conn.Host).
		Int("port", s.conn.Port).
		Msg("Connecting to ClickHouse")

	client, err := clickhouse.NewClient(s.conn)
	if err != nil {
		stateLog.Error().
			Err(err).
			Str("host", s.conn.Host).
			Int("port", s.conn.Port).
			Msg("Failed to connect to ClickHouse")
		s.lastError = fmt.Sprintf("connection failed: %v", err)
		s.connected = false
		return err
	}

	s.client = client
	s.connected = true
	s.lastError = ""

	stateLog.Info().
		Str("host", s.conn.Host).
		Int("port", s.conn.Port).
		Msg("Connected to ClickHouse successfully")

	if s.dataDir != "" {
		s.initRRD()
	}

	return nil
}

func (s *State) initRRD() {
	stateLog.Info().Str("data_dir", s.dataDir).Msg("Initializing RRD storage")

	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		stateLog.Error().Err(err).Str("data_dir", s.dataDir).Msg("Failed to create data directory")
		return
	}

	var err error
	s.rrdTotalBytes, err = rrd.New(filepath.Join(s.dataDir, "total_bytes.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create total_bytes RRD")
	}

	s.rrdTotalRows, err = rrd.New(filepath.Join(s.dataDir, "total_rows.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create total_rows RRD")
	}

	s.rrdDiskUsage, err = rrd.New(filepath.Join(s.dataDir, "disk_usage.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create disk_usage RRD")
	}

	s.rrdCPUUsage, err = rrd.New(filepath.Join(s.dataDir, "cpu_usage.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create cpu_usage RRD")
	}

	s.rrdMemUsage, err = rrd.New(filepath.Join(s.dataDir, "mem_usage.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create mem_usage RRD")
	}

	s.rrdIngestion, err = rrd.New(filepath.Join(s.dataDir, "ingestion.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create ingestion RRD")
	}

	s.rrdUsers, err = rrd.New(filepath.Join(s.dataDir, "users.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create users RRD")
	}

	s.rrdErrors, err = rrd.New(filepath.Join(s.dataDir, "errors.rrd"))
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to create errors RRD")
	}

	stateLog.Debug().
		Str("data_dir", s.dataDir).
		Msg("RRD storage initialized")
}

func (s *State) StartRRD(ctx context.Context) {
	if s.rrdTotalBytes == nil {
		stateLog.Warn().Msg("RRD not initialized, skipping scheduler start")
		return
	}

	stateLog.Info().Msg("Starting RRD schedulers")

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

	// Disk usage collector - using SystemStats
	collectorDisk := func() (int64, error) {
		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()
		if client == nil {
			return 0, fmt.Errorf("not connected")
		}
		stats, err := client.GetSystemStats(ctx)
		if err != nil {
			return 0, err
		}
		return int64(stats.DiskUsagePercent), nil
	}
	s.rrdDiskUsage.StartScheduler(ctx, collectorDisk)

	// CPU usage collector
	collectorCPU := func() (int64, error) {
		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()
		if client == nil {
			return 0, fmt.Errorf("not connected")
		}
		stats, err := client.GetSystemStats(ctx)
		if err != nil {
			return 0, err
		}
		return int64(stats.CPUUsagePercent), nil
	}
	s.rrdCPUUsage.StartScheduler(ctx, collectorCPU)

	// Memory usage collector
	collectorMem := func() (int64, error) {
		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()
		if client == nil {
			return 0, fmt.Errorf("not connected")
		}
		stats, err := client.GetSystemStats(ctx)
		if err != nil {
			return 0, err
		}
		return int64(stats.MemUsagePercent), nil
	}
	s.rrdMemUsage.StartScheduler(ctx, collectorMem)

	// Ingestion speed collector
	collectorIngestion := func() (int64, error) {
		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()
		if client == nil {
			return 0, fmt.Errorf("not connected")
		}
		bytes, err := client.GetIngestionSpeed(ctx)
		if err != nil {
			return 0, err
		}
		return int64(bytes), nil
	}
	s.rrdIngestion.StartScheduler(ctx, collectorIngestion)

	// Connected users collector
	collectorUsers := func() (int64, error) {
		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()
		if client == nil {
			return 0, fmt.Errorf("not connected")
		}
		count, err := client.GetConnectedUsers(ctx)
		if err != nil {
			return 0, err
		}
		return int64(count), nil
	}
	s.rrdUsers.StartScheduler(ctx, collectorUsers)

	// Error count collector
	collectorErrors := func() (int64, error) {
		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()
		if client == nil {
			return 0, fmt.Errorf("not connected")
		}
		count, err := client.GetErrorCount(ctx)
		if err != nil {
			return 0, err
		}
		return int64(count), nil
	}
	s.rrdErrors.StartScheduler(ctx, collectorErrors)

	stateLog.Info().Msg("RRD schedulers started")
}

func (s *State) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stateLog.Info().Msg("Closing state")

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			stateLog.Error().Err(err).Msg("Error closing ClickHouse client")
		}
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

	stateLog.Debug().Msg("Polling ClickHouse metrics")

	metrics, err := client.GetSystemMetrics(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastError = fmt.Sprintf("metrics: %v", err)
		s.mu.Unlock()
		stateLog.Error().Err(err).Msg("Failed to poll metrics")
		return err
	}

	tables, err := client.GetTableMetrics(ctx, 50)
	if err != nil {
		s.mu.Lock()
		s.lastError = fmt.Sprintf("tables: %v", err)
		s.mu.Unlock()
		stateLog.Error().Err(err).Msg("Failed to poll tables")
		return err
	}

	queries, err := client.GetRunningQueries(ctx)
	if err != nil {
		s.mu.Lock()
		s.lastError = fmt.Sprintf("queries: %v", err)
		s.mu.Unlock()
		stateLog.Error().Err(err).Msg("Failed to poll queries")
		return err
	}

	s.mu.Lock()
	s.metrics = metrics
	s.tables = tables
	s.queries = queries
	s.lastError = ""
	s.mu.Unlock()

	stateLog.Debug().
		Int("tables", len(tables)).
		Int("queries", len(queries)).
		Msg("Poll completed successfully")

	return nil
}

func (s *State) TruncateTable(ctx context.Context, database, table string) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	stateLog.Info().
		Str("database", database).
		Str("table", table).
		Msg("Truncating table")

	err := client.TruncateTable(ctx, database, table)
	if err != nil {
		stateLog.Error().
			Err(err).
			Str("database", database).
			Str("table", table).
			Msg("Failed to truncate table")
	}
	return err
}

func (s *State) ModifyTTL(ctx context.Context, database, table, newTTL string) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	stateLog.Info().
		Str("database", database).
		Str("table", table).
		Str("ttl", newTTL).
		Msg("Modifying table TTL")

	err := client.ModifyTTL(ctx, database, table, newTTL)
	if err != nil {
		stateLog.Error().
			Err(err).
			Str("database", database).
			Str("table", table).
			Str("ttl", newTTL).
			Msg("Failed to modify TTL")
	}
	return err
}

func (s *State) ExecuteQuery(ctx context.Context, query string) (*clickhouse.QueryResult, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	stateLog.Debug().
		Str("query", query).
		Msg("Executing query")

	result, err := client.ExecuteQuery(ctx, query)
	if err != nil {
		stateLog.Error().
			Err(err).
			Str("query", query).
			Msg("Query execution failed")
		return nil, err
	}

	stateLog.Debug().
		Str("query", query).
		Int("rows", len(result.Rows)).
		Msg("Query executed successfully")

	return result, nil
}

func (s *State) QueryHistory(metric string, period string) ([]rrd.Sample, error) {
	stateLog.Debug().
		Str("metric", metric).
		Str("period", period).
		Msg("Querying history")

	if s.rrdTotalBytes == nil {
		stateLog.Warn().Msg("History not available (no data directory)")
		return nil, fmt.Errorf("history not available (no data directory)")
	}

	switch metric {
	case "total_bytes":
		switch period {
		case "day":
			return s.rrdTotalBytes.QueryDay(), nil
		case "week":
			return s.rrdTotalBytes.QueryWeek(), nil
		case "month":
			return s.rrdTotalBytes.QueryMonth(), nil
		}
	case "total_rows":
		switch period {
		case "day":
			return s.rrdTotalRows.QueryDay(), nil
		case "week":
			return s.rrdTotalRows.QueryWeek(), nil
		case "month":
			return s.rrdTotalRows.QueryMonth(), nil
		}
	case "disk_usage":
		switch period {
		case "day":
			return s.rrdDiskUsage.QueryDay(), nil
		case "week":
			return s.rrdDiskUsage.QueryWeek(), nil
		case "month":
			return s.rrdDiskUsage.QueryMonth(), nil
		}
	case "cpu_usage":
		switch period {
		case "day":
			return s.rrdCPUUsage.QueryDay(), nil
		case "week":
			return s.rrdCPUUsage.QueryWeek(), nil
		case "month":
			return s.rrdCPUUsage.QueryMonth(), nil
		}
	case "memory_usage":
		switch period {
		case "day":
			return s.rrdMemUsage.QueryDay(), nil
		case "week":
			return s.rrdMemUsage.QueryWeek(), nil
		case "month":
			return s.rrdMemUsage.QueryMonth(), nil
		}
	case "ingestion":
		switch period {
		case "day":
			return s.rrdIngestion.QueryDay(), nil
		case "week":
			return s.rrdIngestion.QueryWeek(), nil
		case "month":
			return s.rrdIngestion.QueryMonth(), nil
		}
	case "users":
		switch period {
		case "day":
			return s.rrdUsers.QueryDay(), nil
		case "week":
			return s.rrdUsers.QueryWeek(), nil
		case "month":
			return s.rrdUsers.QueryMonth(), nil
		}
	case "errors":
		switch period {
		case "day":
			return s.rrdErrors.QueryDay(), nil
		case "week":
			return s.rrdErrors.QueryWeek(), nil
		case "month":
			return s.rrdErrors.QueryMonth(), nil
		}
	}

	stateLog.Error().
		Str("metric", metric).
		Str("period", period).
		Msg("Unknown metric or period")
	return nil, fmt.Errorf("unknown metric or period")
}

func (s *State) GetCHClientForTest() *clickhouse.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *State) GetTruncatableTables(ctx context.Context) ([]clickhouse.TruncatableTable, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	stateLog.Debug().Msg("Fetching truncatable tables")

	tables, err := client.GetTruncatableTables(ctx)
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to fetch truncatable tables")
	}

	return tables, err
}

func (s *State) GetDiskMetrics(ctx context.Context) ([]clickhouse.DiskMetric, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	stateLog.Debug().Msg("Fetching disk metrics")

	metrics, err := client.GetDiskMetrics(ctx)
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to fetch disk metrics")
	}

	return metrics, err
}

func (s *State) GetSystemStats(ctx context.Context) (*clickhouse.SystemStats, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	stateLog.Debug().Msg("Fetching system stats")

	stats, err := client.GetSystemStats(ctx)
	if err != nil {
		stateLog.Error().Err(err).Msg("Failed to fetch system stats")
	}

	return stats, err
}
