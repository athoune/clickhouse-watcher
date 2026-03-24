package client

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
)

// testServer spins up a real Unix-socket HTTP server backed by handler h,
// returns the socket path and a cleanup function.
func testServer(t *testing.T, h http.Handler) (string, func()) {
	t.Helper()
	sock := t.TempDir() + "/test.sock"

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln) //nolint:errcheck
	return sock, func() {
		srv.Close()
		os.Remove(sock)
	}
}

// respond is a shorthand to write a JSON body.
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// IsConnected
// ---------------------------------------------------------------------------

func TestIsConnectedTrue(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, map[string]bool{"connected": true})
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ok, err := c.IsConnected(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected connected=true")
	}
}

func TestIsConnectedFalseOn503(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		respond(w, map[string]bool{"connected": false})
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ok, err := c.IsConnected(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected connected=false on 503")
	}
}

func TestIsConnectedNoServer(t *testing.T) {
	c := NewClient("/tmp/nonexistent-socket-xyz.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ok, err := c.IsConnected(ctx)
	// The implementation swallows the dial error and returns (false, nil).
	if err != nil {
		t.Logf("got error (acceptable): %v", err)
	}
	if ok {
		t.Error("should not be connected when socket does not exist")
	}
}

// ---------------------------------------------------------------------------
// GetMetrics
// ---------------------------------------------------------------------------

func TestGetMetrics(t *testing.T) {
	want := clickhouse.SystemMetrics{
		Version: "24.8.1",
		Uptime:  2 * time.Hour,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, want)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version != want.Version {
		t.Errorf("Version: got %q, want %q", got.Version, want.Version)
	}
}

func TestGetMetrics503(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.GetMetrics(ctx)
	if err == nil {
		t.Error("expected error on 503")
	}
}

// ---------------------------------------------------------------------------
// GetTables
// ---------------------------------------------------------------------------

func TestGetTables(t *testing.T) {
	want := []clickhouse.TableMetric{
		{Database: "db1", Name: "events", Size: "1.0 GB"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tables", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, want)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.GetTables(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "events" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// GetQueries
// ---------------------------------------------------------------------------

func TestGetQueries(t *testing.T) {
	want := []clickhouse.QueryMetric{
		{Query: "SELECT 1", RowsRead: 1},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/queries", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, want)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.GetQueries(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Query != "SELECT 1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// GetTruncatableTables
// ---------------------------------------------------------------------------

func TestGetTruncatableTables(t *testing.T) {
	want := []clickhouse.TruncatableTable{
		{Database: "db1", Table: "logs", Truncatable: true},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/truncatables", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, want)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.GetTruncatableTables(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || !got[0].Truncatable {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// ExecuteQuery
// ---------------------------------------------------------------------------

func TestExecuteQuery(t *testing.T) {
	want := clickhouse.QueryResult{
		Headers: []string{"num"},
		Rows:    [][]string{{"1"}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		respond(w, want)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.ExecuteQuery(ctx, "SELECT 1 AS num")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0][0] != "1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// TruncateTable
// ---------------------------------------------------------------------------

func TestTruncateTable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/truncate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		respond(w, map[string]string{"status": "ok"})
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.TruncateTable(ctx, "db1", "events"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ModifyTTL
// ---------------------------------------------------------------------------

func TestModifyTTL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ttl", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		respond(w, map[string]string{"status": "ok"})
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.ModifyTTL(ctx, "db1", "events", "created_at + INTERVAL 7 DAY"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetHistory
// ---------------------------------------------------------------------------

func TestGetHistory(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	want := []rrd.Sample{
		{At: now, Value: 1024},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/history/total_bytes/day", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, want)
	})
	sock, cleanup := testServer(t, mux)
	defer cleanup()

	c := NewClient(sock)

	got, err := c.GetHistory("total_bytes", "day")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Value != 1024 {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// SocketPath
// ---------------------------------------------------------------------------

func TestSocketPath(t *testing.T) {
	c := NewClient("/var/run/test.sock")
	if c.SocketPath() != "/var/run/test.sock" {
		t.Errorf("unexpected socket path: %q", c.SocketPath())
	}
}
