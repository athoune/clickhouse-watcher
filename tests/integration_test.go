package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
)

func TestConnection(t *testing.T) {
	conn := clickhouse.Connection{
		Host:     getEnvOrDefault("CH_HOST", "localhost"),
		Port:     9001,
		Database: getEnvOrDefault("CH_DATABASE", "test"),
		Username: getEnvOrDefault("CH_USER", "test"),
		Password: getEnvOrDefault("CH_PASSWORD", "test123"),
	}

	client, err := clickhouse.NewClient(conn)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to clickHouse://%s:%s@%s:%v : %v", conn.Username, conn.Password, conn.Host, conn.Port, err)
	}
	defer client.Close()

	if err := client.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestGetSystemMetrics(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics, err := client.GetSystemMetrics(ctx)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	if metrics.Version == "" {
		t.Error("Version should not be empty")
	}

	if metrics.Uptime == 0 {
		t.Error("Uptime should be greater than 0")
	}

	t.Logf("Version: %s, Uptime: %s", metrics.Version, metrics.Uptime)
}

func TestGetTableMetrics(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics, err := client.GetTableMetrics(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get table metrics: %v", err)
	}

	t.Logf("Found %d tables", len(metrics))
	for _, m := range metrics {
		t.Logf("  Table: %s.%s, Size: %s", m.Database, m.Name, m.Size)
	}
}

func TestGetTableDetails(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_table (id UInt64, name String, created_at DateTime) ENGINE = MergeTree() ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer client.Exec(ctx, "DROP TABLE IF EXISTS test_table")

	detail, err := client.GetTableDetails(ctx, "test", "test_table")
	if err != nil {
		t.Fatalf("Failed to get table details: %v", err)
	}

	if detail.Name != "test_table" {
		t.Errorf("Expected table name 'test_table', got '%s'", detail.Name)
	}

	if detail.Engine != "MergeTree" {
		t.Errorf("Expected engine 'MergeTree', got '%s'", detail.Engine)
	}

	t.Logf("Table detail: %+v", detail)
}

func TestTruncateTable(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_truncate (id UInt64, name String) ENGINE = MergeTree() ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer client.Exec(ctx, "DROP TABLE IF EXISTS test_truncate")

	err = client.Exec(ctx, "INSERT INTO test_truncate VALUES (1, 'test1'), (2, 'test2')")
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	err = client.TruncateTable(ctx, "test", "test_truncate")
	if err != nil {
		t.Fatalf("Failed to truncate table: %v", err)
	}

	detail, err := client.GetTableDetails(ctx, "test", "test_truncate")
	if err != nil {
		t.Fatalf("Failed to get table details after truncate: %v", err)
	}

	if detail.TTL != "" {
		t.Logf("Table TTL after truncate: %s", detail.TTL)
	}
}

func TestModifyTTL(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_ttl (id UInt64, created_at DateTime) ENGINE = MergeTree() ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer client.Exec(ctx, "DROP TABLE IF EXISTS test_ttl")

	err = client.ModifyTTL(ctx, "test", "test_ttl", "created_at + INTERVAL 7 DAY")
	if err != nil {
		t.Fatalf("Failed to modify TTL: %v", err)
	}

	detail, err := client.GetTableDetails(ctx, "test", "test_ttl")
	if err != nil {
		t.Fatalf("Failed to get table details: %v", err)
	}

	t.Logf("TTL set: %s", detail.TTL)

	err = client.ModifyTTL(ctx, "test", "test_ttl", "")
	if err != nil {
		t.Fatalf("Failed to remove TTL: %v", err)
	}
}

func TestExecuteQuery(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ExecuteQuery(ctx, "SELECT 1 AS num, 'hello' AS str, toDateTime('2024-01-01 12:00:00') AS dt")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if len(result.Rows) == 0 {
		t.Fatal("Expected at least one row")
	}

	t.Logf("Query executed successfully, got %d rows", len(result.Rows))
}

func TestGetRunningQueries(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	queries, err := client.GetRunningQueries(ctx)
	if err != nil {
		t.Fatalf("Failed to get running queries: %v", err)
	}

	t.Logf("Current running queries: %d", len(queries))
}

func TestGetTruncatableTables(t *testing.T) {
	client := getTestClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_truncatable (id UInt64, name String) ENGINE = MergeTree() ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer client.Exec(ctx, "DROP TABLE IF EXISTS test_truncatable")

	err = client.Exec(ctx, "INSERT INTO test_truncatable VALUES (1, 'test1'), (2, 'test2'), (3, 'test3')")
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	tables, err := client.GetTruncatableTables(ctx)
	if err != nil {
		t.Fatalf("Failed to get truncatable tables: %v", err)
	}

	found := false
	for _, tbl := range tables {
		if tbl.Database == "test" && tbl.Table == "test_truncatable" {
			found = true
			t.Logf("Found table: %s.%s, Size: %s, Rows: %d, Truncatable: %v",
				tbl.Database, tbl.Table, tbl.Size, tbl.Rows, tbl.Truncatable)
			if tbl.Rows == 0 {
				t.Error("Expected rows to be greater than 0")
			}
			break
		}
	}

	if !found {
		t.Errorf("Expected to find test_truncatable in the list")
	}

	t.Logf("Found %d truncatable tables", len(tables))
}

func getTestClient(t *testing.T) *clickhouse.Client {
	conn := clickhouse.Connection{
		Host:     getEnvOrDefault("CH_HOST", "localhost"),
		Port:     9001,
		Database: getEnvOrDefault("CH_DATABASE", "test"),
		Username: getEnvOrDefault("CH_USER", "test"),
		Password: getEnvOrDefault("CH_PASSWORD", "test123"),
	}

	client, err := clickhouse.NewClient(conn)
	if err != nil {
		t.Skipf("Skipping test: cannot connect to ClickHouse: %v", err)
	}

	return client
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
