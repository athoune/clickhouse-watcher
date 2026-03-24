package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/athoune/clickhouse-watcher/internal/clickhouse"
	"github.com/athoune/clickhouse-watcher/rrd"
)

// Client communicates with the daemon via HTTP over Unix socket.
type Client struct {
	socketPath string
}

// NewClient creates a client that connects to the daemon at the given socket path.
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
	}
}

// SocketPath returns the Unix socket path used for connections.
func (c *Client) SocketPath() string {
	return c.socketPath
}

// unixTransport returns an HTTP transport that dials the Unix socket.
func (c *Client) unixTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", c.socketPath)
		},
	}
}

// IsConnected checks if the daemon is available and responding.
func (c *Client) IsConnected(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/status", nil)
	if err != nil {
		return false, err
	}

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// GetMetrics retrieves system metrics from the daemon.
func (c *Client) GetMetrics(ctx context.Context) (*clickhouse.SystemMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/metrics", nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result clickhouse.SystemMetrics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetTables retrieves the list of tables from the daemon.
func (c *Client) GetTables(ctx context.Context) ([]clickhouse.TableMetric, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/tables", nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []clickhouse.TableMetric
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetQueries retrieves currently running queries from the daemon.
func (c *Client) GetQueries(ctx context.Context) ([]clickhouse.QueryMetric, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/queries", nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []clickhouse.QueryMetric
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// ExecuteQuery executes a SQL query via the daemon and returns the results.
func (c *Client) ExecuteQuery(ctx context.Context, query string) (*clickhouse.QueryResult, error) {
	body, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/api/query", nil)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result clickhouse.QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// TruncateTable truncates a table via the daemon.
func (c *Client) TruncateTable(ctx context.Context, database, table string) error {
	body, _ := json.Marshal(map[string]string{
		"database": database,
		"table":    table,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/api/truncate", nil)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", resp.StatusCode)
	}

	return nil
}

// ModifyTTL modifies or removes the TTL of a table via the daemon.
func (c *Client) ModifyTTL(ctx context.Context, database, table, ttl string) error {
	body, _ := json.Marshal(map[string]string{
		"database": database,
		"table":    table,
		"ttl":      ttl,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/api/ttl", nil)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", resp.StatusCode)
	}

	return nil
}

// GetHistory retrieves historical metric data from the daemon.
func (c *Client) GetHistory(metric, period string) ([]rrd.Sample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/history/"+metric+"/"+period, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []rrd.Sample
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}
