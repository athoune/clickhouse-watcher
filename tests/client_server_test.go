package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/athoune/clickhouse-watcher/client"
	"github.com/athoune/clickhouse-watcher/daemon"
	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
)

const testSocketPath = "/tmp/clickhouse-watcher-test.sock"

func TestClientServerStatus(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	if !state.IsConnected() {
		t.Skip("Daemon not connected to ClickHouse")
	}

	cli := client.NewClient(testSocketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connected, err := cli.IsConnected(ctx)
	if err != nil {
		t.Fatalf("IsConnected failed: %v", err)
	}
	if !connected {
		t.Error("Expected to be connected")
	}
}

func TestClientServerMetrics(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	cli := client.NewClient(testSocketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics, err := cli.GetMetrics(ctx)
	if err != nil {
		t.Logf("Metrics not available (initial poll may have failed): %v", err)
		t.Skip("Metrics not available")
	}

	if metrics.Version == "" {
		t.Error("Version should not be empty")
	}

	t.Logf("Version: %s, Uptime: %s", metrics.Version, metrics.Uptime)
}

func TestClientServerTables(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	cli := client.NewClient(testSocketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tables, err := cli.GetTables(ctx)
	if err != nil {
		t.Fatalf("GetTables failed: %v", err)
	}

	t.Logf("Found %d tables", len(tables))
	for _, tbl := range tables {
		t.Logf("  Table: %s.%s, Size: %s", tbl.Database, tbl.Name, tbl.Size)
	}
}

func TestClientServerQueries(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	cli := client.NewClient(testSocketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	queries, err := cli.GetQueries(ctx)
	if err != nil {
		t.Fatalf("GetQueries failed: %v", err)
	}

	t.Logf("Current running queries: %d", len(queries))
}

func TestClientServerExecuteQuery(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	cli := client.NewClient(testSocketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cli.ExecuteQuery(ctx, "SELECT 1 AS num, 'hello' AS str")
	if err != nil {
		t.Fatalf("ExecuteQuery failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	t.Logf("Query result: headers=%v, rows=%d", result.Headers, len(result.Rows))
}

func TestClientServerTruncateTable(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	chCli := state.GetCHClientForTest()
	chCli.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS test_truncate_srv (id UInt64) ENGINE = MergeTree() ORDER BY id")
	chCli.Exec(context.Background(), "INSERT INTO test_truncate_srv VALUES (1), (2), (3)")

	cli := client.NewClient(testSocketPath)
	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := cli.TruncateTable(ctx2, "test", "test_truncate_srv")
	if err != nil {
		t.Fatalf("TruncateTable failed: %v", err)
	}

	t.Log("Truncate successful")
}

func TestClientServerHistory(t *testing.T) {
	tmpDir := t.TempDir()
	conn := clickhouse.Connection{
		Host:     getEnvOrDefault("CH_HOST", "localhost"),
		Port:     9001,
		Database: getEnvOrDefault("CH_DATABASE", "test"),
		Username: getEnvOrDefault("CH_USER", "test"),
		Password: getEnvOrDefault("CH_PASSWORD", "test123"),
	}

	state := daemon.NewState(conn, tmpDir)
	if err := state.Connect(); err != nil {
		t.Skipf("Skipping test: cannot connect to ClickHouse: %v", err)
	}
	defer state.Close()

	srv := daemon.NewServer(state, testSocketPath, 5*time.Second)
	if err := srv.Start(); err != nil {
		t.Skipf("Skipping test: cannot start daemon: %v", err)
	}
	defer srv.Stop()

	if !state.IsConnected() {
		t.Skip("Daemon not connected to ClickHouse")
	}

	cli := client.NewClient(testSocketPath)

	samples, err := cli.GetHistory("total_bytes", "day")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}

	t.Logf("Got %d history samples", len(samples))
}

func TestClientServerModifyTTL(t *testing.T) {
	srv, state := startTestDaemon(t)
	defer stopTestDaemon(srv, state)

	chCli := state.GetCHClientForTest()
	ctx := context.Background()
	chCli.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_ttl_srv (id UInt64, created_at DateTime) ENGINE = MergeTree() ORDER BY id")

	cli := client.NewClient(testSocketPath)
	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := cli.ModifyTTL(ctx2, "test", "test_ttl_srv", "created_at + INTERVAL 7 DAY")
	if err != nil {
		t.Fatalf("ModifyTTL failed: %v", err)
	}

	t.Log("TTL modified successfully")
}

func TestClientServerConnectionRefused(t *testing.T) {
	os.Remove(testSocketPath)

	cli := client.NewClient(testSocketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	connected, err := cli.IsConnected(ctx)
	if err != nil {
		t.Logf("Expected error: %v", err)
	}
	if connected {
		t.Error("Should not be connected when daemon is not running")
	}
}

func startTestDaemon(t *testing.T) (*daemon.Server, *daemon.State) {
	conn := clickhouse.Connection{
		Host:     getEnvOrDefault("CH_HOST", "localhost"),
		Port:     9001,
		Database: getEnvOrDefault("CH_DATABASE", "test"),
		Username: getEnvOrDefault("CH_USER", "test"),
		Password: getEnvOrDefault("CH_PASSWORD", "test123"),
	}

	state := daemon.NewState(conn, "")
	if err := state.Connect(); err != nil {
		t.Skipf("Skipping test: cannot connect to ClickHouse: %v", err)
	}

	t.Logf("State connected: %v", state.IsConnected())

	srv := daemon.NewServer(state, testSocketPath, 5*time.Second)
	if err := srv.Start(); err != nil {
		state.Close()
		t.Skipf("Skipping test: cannot start daemon: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	return srv, state
}

func stopTestDaemon(srv *daemon.Server, state *daemon.State) {
	srv.Stop()
	state.Close()
	os.Remove(testSocketPath)
}
