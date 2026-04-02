package clickhouse

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Connection struct {
	Name     string
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

type SystemMetrics struct {
	Version             string
	Uptime              time.Duration
	TotalRows           uint64
	TotalBytes          uint64
	BackgroundPools     int
	MaxPartsInPartition int
}

type QueryMetric struct {
	Query       string
	RowsRead    uint64
	BytesRead   uint64
	MemoryUsage uint64
}

type TableMetric struct {
	Database  string
	Name      string
	Size      string
	SizeBytes uint64
	MinDate   string
	MaxDate   string
}

type TableDetail struct {
	Database   string
	Name       string
	Engine     string
	SortingKey string
	TTL        string
}

type TruncatableTable struct {
	Database    string
	Table       string
	Rows        uint64
	Size        string
	Truncatable bool
}

type DiskMetric struct {
	Database string
	Table    string
	DiskName string
	Percent  float64
	Bytes    uint64
}

type QueryResult struct {
	Headers []string
	Rows    [][]string
}

type SystemStats struct {
	DiskUsagePercent float64
	CPUUsagePercent  float64
	MemUsagePercent  float64
}

type Client struct {
	conn driver.Conn
}

func NewClient(cfg Connection) (*Client, error) {
	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:  5 * time.Second,
		MaxOpenConns: 5,
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) GetSystemMetrics(ctx context.Context) (*SystemMetrics, error) {
	var version string
	var uptime uint32

	query := "SELECT version(), uptime()"
	row := c.conn.QueryRow(ctx, query)
	if err := row.Scan(&version, &uptime); err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	var totalRows uint64
	var totalBytes uint64
	query = `
SELECT
    sum(total_rows) AS rows,
    sum(total_bytes) AS bytes
FROM system.tables
		`
	row = c.conn.QueryRow(ctx, query)
	_ = row.Scan(&totalRows, &totalBytes)

	var maxParts int
	query = "SELECT max(partition_id) FROM system.parts WHERE active = 1"
	_ = c.conn.QueryRow(ctx, query).Scan(&maxParts)

	return &SystemMetrics{
		Version:             version,
		Uptime:              time.Duration(uptime) * time.Second,
		TotalRows:           totalRows,
		TotalBytes:          totalBytes,
		BackgroundPools:     16,
		MaxPartsInPartition: maxParts,
	}, nil
}

func (c *Client) GetTableMetrics(ctx context.Context, limit int) ([]TableMetric, error) {
	query := fmt.Sprintf(`
		SELECT
			"table",
			database,
			formatReadableSize(sum(bytes)) AS size,
			sum(bytes) AS sort_by_size,
			min(min_date) AS min_date,
			max(max_date) AS max_date
		FROM system.parts
		WHERE active
		GROUP BY
			"table",
			database
		ORDER BY sort_by_size DESC
		LIMIT %d
	`, limit)

	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var metrics []TableMetric
	for rows.Next() {
		var m TableMetric
		if err := rows.Scan(&m.Name, &m.Database, &m.Size, &m.SizeBytes, &m.MinDate, &m.MaxDate); err != nil {
			continue
		}
		metrics = append(metrics, m)
	}

	return metrics, nil
}

func (c *Client) GetTableDetails(ctx context.Context, database, table string) (*TableDetail, error) {
	query := `
		SELECT
			database,
			name,
			engine,
			sorting_key
		FROM system.tables
		WHERE database = ? AND name = ?
		LIMIT 1
	`

	var detail TableDetail
	row := c.conn.QueryRow(ctx, query, database, table)
	if err := row.Scan(&detail.Database, &detail.Name, &detail.Engine, &detail.SortingKey); err != nil {
		return nil, fmt.Errorf("failed to get table details: %w", err)
	}

	ttlQuery := `
		SELECT expression
		FROM system.ttl_recalculations
		WHERE database = ? AND table_name = ?
		LIMIT 1
	`
	row = c.conn.QueryRow(ctx, ttlQuery, database, table)
	_ = row.Scan(&detail.TTL)

	return &detail, nil
}

func (c *Client) TruncateTable(ctx context.Context, database, table string) error {
	query := fmt.Sprintf("TRUNCATE TABLE `%s`.`%s`", database, table)
	return c.conn.Exec(ctx, query)
}

func (c *Client) ModifyTTL(ctx context.Context, database, table, newTTL string) error {
	var query string
	if newTTL == "" {
		query = fmt.Sprintf("ALTER TABLE `%s`.`%s` REMOVE TTL", database, table)
	} else {
		query = fmt.Sprintf("ALTER TABLE `%s`.`%s` MODIFY TTL %s", database, table, newTTL)
	}
	return c.conn.Exec(ctx, query)
}

func (c *Client) GetRunningQueries(ctx context.Context) ([]QueryMetric, error) {
	query := `
		SELECT
			query,
			read_rows,
			read_bytes,
			memory_usage
		FROM system.processes
		WHERE query NOT LIKE '%system.processes%'
		LIMIT 10
	`

	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query processes: %w", err)
	}
	defer rows.Close()

	var metrics []QueryMetric
	for rows.Next() {
		var m QueryMetric
		if err := rows.Scan(&m.Query, &m.RowsRead, &m.BytesRead, &m.MemoryUsage); err != nil {
			continue
		}
		metrics = append(metrics, m)
	}

	return metrics, nil
}

func (c *Client) GetTruncatableTables(ctx context.Context) ([]TruncatableTable, error) {
	query := `
SELECT
    database,
    "table",
    size,
    rows,
    like(comment, '%It is safe to truncate or drop this table at any time.') AS truncatable
FROM system.tables AS t
INNER JOIN
(
    SELECT
        "table",
        database,
        formatReadableSize(sum(bytes)) AS size,
        sum(bytes) AS bytes_raw,
        sum(rows) AS rows
    FROM system.parts
    WHERE active
    GROUP BY
        database,
        "table"
    ORDER BY bytes_raw DESC
    LIMIT 50
) AS p ON (t."table" = p."table") AND (t.database = p.database)
ORDER BY p.bytes_raw DESC
	`
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query processes: %w", err)
	}
	defer rows.Close()

	var tables []TruncatableTable
	for rows.Next() {
		var line TruncatableTable
		err = rows.Scan(&line.Database, &line.Table, &line.Size, &line.Rows, &line.Truncatable)
		if err != nil {
			return nil, fmt.Errorf("failed to scan query response: %w", err)
		}
		tables = append(tables, line)
	}
	return tables, nil
}

func (c *Client) ExecuteQuery(ctx context.Context, query string) (*QueryResult, error) {
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	headers := rows.Columns()
	types := rows.ColumnTypes()
	var results [][]string

	for rows.Next() {
		values := make([]interface{}, len(types))
		for idx, t := range types {
			scanType := t.ScanType()
			values[idx] = reflect.New(scanType).Interface()
		}
		if err := rows.Scan(values...); err != nil {
			continue
		}
		row := make([]string, len(headers))
		for i, v := range values {
			rv := reflect.ValueOf(v)
			if rv.Kind() == reflect.Ptr {
				rv = rv.Elem()
			}
			switch rv.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				row[i] = fmt.Sprintf("%d", rv.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				row[i] = fmt.Sprintf("%d", rv.Uint())
			case reflect.Float32, reflect.Float64:
				row[i] = fmt.Sprintf("%f", rv.Float())
			case reflect.String:
				row[i] = rv.String()
			case reflect.Struct:
				if _, ok := v.(*time.Time); ok {
					row[i] = rv.Interface().(time.Time).Format(time.RFC3339)
				} else {
					row[i] = fmt.Sprintf("%v", rv.Interface())
				}
			default:
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		results = append(results, row)
	}

	return &QueryResult{
		Headers: headers,
		Rows:    results,
	}, nil
}

func (c *Client) GetDiskMetrics(ctx context.Context) ([]DiskMetric, error) {
	query := `
SELECT
    database,
    "table",
    disk_name,
    round((100 * sum(bytes_on_disk)) / median(disks.total_space), 5) AS percent,
    sum(bytes_on_disk) AS bytes
FROM system.parts AS parts
INNER JOIN system.disks AS disks ON parts.disk_name = disks.name
GROUP BY
    database,
    "table",
    disk_name
ORDER BY
    disk_name ASC,
    database ASC,
    "table" ASC,
    bytes DESC
	`
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query disk metrics: %w", err)
	}
	defer rows.Close()

	var metrics []DiskMetric
	for rows.Next() {
		var m DiskMetric
		err = rows.Scan(&m.Database, &m.Table, &m.DiskName, &m.Percent, &m.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to scan disk metric: %w", err)
		}
		metrics = append(metrics, m)
	}
	return metrics, nil
}

func (c *Client) GetSystemStats(ctx context.Context) (*SystemStats, error) {
	query := `
SELECT
	(
	SELECT
 		round(100 * maxIf(value, metric = 'DiskUsed_default') / maxIf(value, metric = 'DiskTotal_default'), 2) AS ratio
	FROM system.asynchronous_metrics
	WHERE metric IN ('DiskUsed_default', 'DiskTotal_default')
	) AS disk_usage,
	(
	SELECT round(sum(CAST(value AS Float64)) * 100, 2)
	FROM system.asynchronous_metrics
	WHERE metric IN (
	 	'OSUserTimeNormalized',
	 	'OSSystemTimeNormalized',
		'OSIrqTimeNormalized',
		'OSSoftIrqTimeNormalized')
	) AS cpu_usage,
	(
	SELECT round((CAST(value AS Float64) / (SELECT CAST(value AS Float64)
	FROM system.asynchronous_metrics
	WHERE metric = 'MemoryResidentMax') * 100), 2
	)
FROM system.asynchronous_metrics
WHERE metric = 'MemoryResident') AS mem_usage
	`
	row := c.conn.QueryRow(ctx, query)
	var stats SystemStats
	err := row.Scan(&stats.DiskUsagePercent, &stats.CPUUsagePercent, &stats.MemUsagePercent)
	if err != nil {
		return nil, fmt.Errorf("failed to scan system stats: %w", err)
	}
	return &stats, nil
}

func (c *Client) Exec(ctx context.Context, query string) error {
	return c.conn.Exec(ctx, query)
}
