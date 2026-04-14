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
	"github.com/athoune/clickhouse-watcher/logger"
	"github.com/athoune/clickhouse-watcher/rrd"
	"github.com/athoune/clickhouse-watcher/version"
)

var clientLog = logger.WithComponent("client")

var default_path = "/tmp/clickhouse-watcher.sock"

func DefaultPath() string {
	return default_path
}

// Client communicates with the daemon via HTTP over Unix socket.
type Client struct {
	socketPath string
}

// NewClient creates a client that connects to the daemon at the given socket path.
func NewClient(socketPath string) *Client {
	clientLog.Debug().Str("socket", socketPath).Msg("Creating new client")
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
			clientLog.Debug().Str("socket", c.socketPath).Msg("Dialing Unix socket")
			var d net.Dialer
			return d.DialContext(ctx, "unix", c.socketPath)
		},
	}
}

// addVersionHeader adds the client version header to the request.
func addVersionHeader(req *http.Request) {
	req.Header.Set("X-Client-Version", version.Version())
}

// IsConnected checks if the daemon is available and responding.
func (c *Client) IsConnected(ctx context.Context) (bool, error) {
	clientLog.Debug().Msg("Checking daemon connection")

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/status", nil)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to create status request")
		return false, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Debug().Err(err).Msg("Daemon connection check failed (daemon may not be running)")
		return false, nil
	}
	defer resp.Body.Close()

	connected := resp.StatusCode == http.StatusOK
	clientLog.Debug().Bool("connected", connected).Int("status", resp.StatusCode).Msg("Daemon connection check completed")
	return connected, nil
}

// checkVersionError checks if the response is a version mismatch error and returns it.
func checkVersionError(resp *http.Response) error {
	if resp.StatusCode == http.StatusConflict {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return fmt.Errorf("version mismatch: please ensure client and server are the same version")
		}
		return fmt.Errorf("%s", errResp.Error)
	}
	return nil
}

// GetMetrics retrieves system metrics from the daemon.
func (c *Client) GetMetrics(ctx context.Context) (*clickhouse.SystemMetrics, error) {
	clientLog.Debug().Msg("Fetching metrics")

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/metrics", nil)
	if err != nil {
		return nil, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to fetch metrics")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("Metrics request failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result clickhouse.SystemMetrics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode metrics response")
		return nil, err
	}

	clientLog.Debug().Str("version", result.Version).Msg("Metrics fetched successfully")
	return &result, nil
}

// GetTables retrieves the list of tables from the daemon.
func (c *Client) GetTables(ctx context.Context) ([]clickhouse.TableMetric, error) {
	clientLog.Debug().Msg("Fetching tables")

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/tables", nil)
	if err != nil {
		return nil, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to fetch tables")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("Tables request failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []clickhouse.TableMetric
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode tables response")
		return nil, err
	}

	clientLog.Debug().Int("count", len(result)).Msg("Tables fetched successfully")
	return result, nil
}

// GetQueries retrieves currently running queries from the daemon.
func (c *Client) GetQueries(ctx context.Context) ([]clickhouse.QueryMetric, error) {
	clientLog.Debug().Msg("Fetching queries")

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/queries", nil)
	if err != nil {
		return nil, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to fetch queries")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("Queries request failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []clickhouse.QueryMetric
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode queries response")
		return nil, err
	}

	clientLog.Debug().Int("count", len(result)).Msg("Queries fetched successfully")
	return result, nil
}

// ExecuteQuery executes a SQL query via the daemon and returns the results.
func (c *Client) ExecuteQuery(ctx context.Context, query string) (*clickhouse.QueryResult, error) {
	clientLog.Info().Str("query", query).Msg("Executing query")

	body, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/api/query", nil)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to execute query")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("Query execution failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result clickhouse.QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode query response")
		return nil, err
	}

	clientLog.Debug().Int("rows", len(result.Rows)).Msg("Query executed successfully")
	return &result, nil
}

// TruncateTable truncates a table via the daemon.
func (c *Client) TruncateTable(ctx context.Context, database, table string) error {
	clientLog.Info().
		Str("database", database).
		Str("table", table).
		Msg("Truncating table")

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
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to truncate table")
		return err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("Truncate request failed")
		return fmt.Errorf("status: %d", resp.StatusCode)
	}

	clientLog.Info().
		Str("database", database).
		Str("table", table).
		Msg("Table truncated successfully")
	return nil
}

// ModifyTTL modifies or removes the TTL of a table via the daemon.
func (c *Client) ModifyTTL(ctx context.Context, database, table, ttl string) error {
	clientLog.Info().
		Str("database", database).
		Str("table", table).
		Str("ttl", ttl).
		Msg("Modifying TTL")

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
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to modify TTL")
		return err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("TTL modification failed")
		return fmt.Errorf("status: %d", resp.StatusCode)
	}

	clientLog.Info().
		Str("database", database).
		Str("table", table).
		Msg("TTL modified successfully")
	return nil
}

// GetHistory retrieves historical metric data from the daemon.
func (c *Client) GetHistory(metric, period string) ([]rrd.Sample, error) {
	clientLog.Debug().
		Str("metric", metric).
		Str("period", period).
		Msg("Fetching history")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/history/"+metric+"/"+period, nil)
	if err != nil {
		return nil, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to fetch history")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("History request failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []rrd.Sample
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode history response")
		return nil, err
	}

	clientLog.Debug().Int("samples", len(result)).Msg("History fetched successfully")
	return result, nil
}

// GetHistoryWithResolution retrieves historical data and aggregates it to the desired resolution.
// The RRD stores data at fixed resolutions (day=2min, week=15min, month=1h).
// This function aggregates data if a coarser resolution is requested.
func (c *Client) GetHistoryWithResolution(metric, period string, resolutionMinutes int) ([]rrd.Sample, error) {
	// Get base data at the finest resolution available for the period
	baseData, err := c.GetHistory(metric, period)
	if err != nil {
		return nil, err
	}

	if len(baseData) == 0 {
		return baseData, nil
	}

	// Determine base resolution from period
	baseResolution := 2 // day: 2 minutes
	if period == "week" {
		baseResolution = 15 // week: 15 minutes
	} else if period == "month" {
		baseResolution = 60 // month: 1 hour
	}

	// If requested resolution is finer or equal, return as-is
	if resolutionMinutes <= baseResolution {
		return baseData, nil
	}

	// Aggregate data: group samples by time windows
	windowSize := resolutionMinutes / baseResolution
	if windowSize < 2 {
		return baseData, nil
	}

	var aggregated []rrd.Sample
	for i := 0; i < len(baseData); i += windowSize {
		end := i + windowSize
		if end > len(baseData) {
			end = len(baseData)
		}

		// Calculate average value for this window
		var sum int64
		for j := i; j < end; j++ {
			sum += baseData[j].Value
		}
		avg := sum / int64(end-i)

		// Use the middle timestamp of the window
		midIdx := i + (end-i)/2
		if midIdx >= len(baseData) {
			midIdx = len(baseData) - 1
		}

		aggregated = append(aggregated, rrd.Sample{
			At:    baseData[midIdx].At,
			Value: avg,
		})
	}

	return aggregated, nil
}

// GetTruncatableTables retrieves tables with their size and truncatability info.
func (c *Client) GetTruncatableTables(ctx context.Context) ([]clickhouse.TruncatableTable, error) {
	clientLog.Debug().Msg("Fetching truncatable tables")

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/truncatables", nil)
	if err != nil {
		return nil, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to fetch truncatable tables")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("Truncatable tables request failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result []clickhouse.TruncatableTable
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode truncatable tables response")
		return nil, err
	}

	clientLog.Debug().Int("count", len(result)).Msg("Truncatable tables fetched successfully")
	return result, nil
}

// GetSystemStats retrieves system stats (CPU, memory, disk usage) from the daemon.
func (c *Client) GetSystemStats(ctx context.Context) (*clickhouse.SystemStats, error) {
	clientLog.Debug().Msg("Fetching system stats")

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/api/stats", nil)
	if err != nil {
		return nil, err
	}
	addVersionHeader(req)

	client := &http.Client{Transport: c.unixTransport()}
	resp, err := client.Do(req)
	if err != nil {
		clientLog.Error().Err(err).Msg("Failed to fetch system stats")
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkVersionError(resp); err != nil {
		clientLog.Error().Err(err).Msg("Version mismatch")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		clientLog.Error().Int("status", resp.StatusCode).Msg("System stats request failed")
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var result clickhouse.SystemStats
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		clientLog.Error().Err(err).Msg("Failed to decode system stats response")
		return nil, err
	}

	clientLog.Debug().Msg("System stats fetched successfully")
	return &result, nil
}
