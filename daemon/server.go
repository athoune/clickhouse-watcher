package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/athoune/clickhouse-watcher/logger"
)

var serverLog = logger.WithComponent("server")

type Server struct {
	state    *State
	socket   string
	server   *http.Server
	interval time.Duration
	stopCh   chan struct{}
}

func NewServer(state *State, socket string, interval time.Duration) *Server {
	serverLog.Debug().
		Str("socket", socket).
		Dur("interval", interval).
		Msg("Creating new server")

	return &Server{
		state:    state,
		socket:   socket,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (s *Server) Start() error {
	serverLog.Info().Str("socket", s.socket).Msg("Starting daemon server")

	if err := os.Remove(s.socket); err != nil && !os.IsNotExist(err) {
		serverLog.Error().Err(err).Str("socket", s.socket).Msg("Failed to remove existing socket")
		return fmt.Errorf("failed to remove socket: %w", err)
	}

	ln, err := net.Listen("unix", s.socket)
	if err != nil {
		serverLog.Error().Err(err).Str("socket", s.socket).Msg("Failed to listen on socket")
		return fmt.Errorf("failed to listen: %w", err)
	}

	if err := os.Chmod(s.socket, 0777); err != nil {
		serverLog.Error().Err(err).Str("socket", s.socket).Msg("Failed to chmod socket")
		return fmt.Errorf("failed to chmod socket: %w", err)
	}

	s.server = &http.Server{
		Handler: s,
	}

	go s.server.Serve(ln)
	go s.pollLoop()

	serverLog.Info().Str("socket", s.socket).Msg("Daemon listening")
	return nil
}

func (s *Server) Stop() error {
	serverLog.Info().Msg("Stopping daemon server")
	close(s.stopCh)
	return s.server.Close()
}

func (s *Server) pollLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	serverLog.Info().Dur("interval", s.interval).Msg("Starting poll loop")

	if err := s.state.Poll(); err != nil {
		serverLog.Error().Err(err).Msg("Initial poll failed")
	} else {
		serverLog.Info().Msg("Initial poll completed")
	}

	for {
		select {
		case <-ticker.C:
			if err := s.state.Poll(); err != nil {
				serverLog.Error().Err(err).Msg("Poll failed")
			} else {
				serverLog.Debug().Msg("Poll completed")
			}
		case <-s.stopCh:
			serverLog.Info().Msg("Poll loop stopped")
			return
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	path := strings.TrimPrefix(r.URL.Path, "/api")
	path = strings.TrimPrefix(path, "/")

	serverLog.Debug().
		Str("method", r.Method).
		Str("path", path).
		Str("remote", r.RemoteAddr).
		Msg("Request received")

	switch {
	case path == "status":
		s.handleStatus(w, r)
	case path == "metrics":
		s.handleMetrics(w, r)
	case path == "tables":
		s.handleTables(w, r)
	case path == "truncatables":
		s.handleTruncatables(w, r)
	case path == "queries":
		s.handleQueries(w, r)
	case strings.HasPrefix(path, "history/"):
		s.handleHistory(w, r, strings.TrimPrefix(path, "history/"))
	case path == "query":
		s.handleQuery(w, r)
	case path == "truncate":
		s.handleTruncate(w, r)
	case path == "ttl":
		s.handleTTL(w, r)
	default:
		serverLog.Warn().
			Str("path", path).
			Msg("Unknown endpoint requested")
		http.NotFound(w, r)
	}

	duration := time.Since(start)
	serverLog.Debug().
		Str("method", r.Method).
		Str("path", path).
		Dur("duration", duration).
		Msg("Request completed")
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	connected := s.state.IsConnected()

	serverLog.Debug().Bool("connected", connected).Msg("Status check")

	w.Header().Set("Content-Type", "application/json")
	if connected {
		json.NewEncoder(w).Encode(map[string]bool{"connected": true})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]bool{"connected": false})
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := s.state.GetMetrics()
	if metrics == nil {
		serverLog.Debug().Msg("Metrics not available")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	serverLog.Debug().
		Str("version", metrics.Version).
		Msg("Serving metrics")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func (s *Server) handleTables(w http.ResponseWriter, r *http.Request) {
	tables := s.state.GetTables()

	serverLog.Debug().Int("count", len(tables)).Msg("Serving tables")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tables)
}

func (s *Server) handleTruncatables(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tables, err := s.state.GetTruncatableTables(ctx)
	if err != nil {
		serverLog.Error().Err(err).Msg("Failed to get truncatable tables")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	serverLog.Debug().Int("count", len(tables)).Msg("Serving truncatable tables")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tables)
}

func (s *Server) handleQueries(w http.ResponseWriter, r *http.Request) {
	queries := s.state.GetQueries()

	serverLog.Debug().Int("count", len(queries)).Msg("Serving queries")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queries)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		serverLog.Warn().Str("path", path).Msg("Invalid history request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	metric := parts[0]
	period := parts[1]

	samples, err := s.state.QueryHistory(metric, period)
	if err != nil {
		serverLog.Error().
			Err(err).
			Str("metric", metric).
			Str("period", period).
			Msg("Failed to query history")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	serverLog.Debug().
		Str("metric", metric).
		Str("period", period).
		Int("samples", len(samples)).
		Msg("Serving history")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(samples)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		serverLog.Warn().Str("method", r.Method).Msg("Method not allowed for query")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		serverLog.Error().Err(err).Msg("Failed to read request body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		serverLog.Error().Err(err).Msg("Failed to unmarshal request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	serverLog.Info().Str("query", req.Query).Msg("Executing query via API")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.state.ExecuteQuery(ctx, req.Query)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleTruncate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		serverLog.Warn().Str("method", r.Method).Msg("Method not allowed for truncate")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		serverLog.Error().Err(err).Msg("Failed to read request body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Database string `json:"database"`
		Table    string `json:"table"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		serverLog.Error().Err(err).Msg("Failed to unmarshal request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	serverLog.Info().
		Str("database", req.Database).
		Str("table", req.Table).
		Msg("Truncating table via API")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := s.state.TruncateTable(ctx, req.Database, req.Table); err != nil {
		serverLog.Error().
			Err(err).
			Str("database", req.Database).
			Str("table", req.Table).
			Msg("Failed to truncate table")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	s.state.Poll()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleTTL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		serverLog.Warn().Str("method", r.Method).Msg("Method not allowed for TTL")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		serverLog.Error().Err(err).Msg("Failed to read request body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Database string `json:"database"`
		Table    string `json:"table"`
		TTL      string `json:"ttl"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		serverLog.Error().Err(err).Msg("Failed to unmarshal request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	serverLog.Info().
		Str("database", req.Database).
		Str("table", req.Table).
		Str("ttl", req.TTL).
		Msg("Modifying TTL via API")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := s.state.ModifyTTL(ctx, req.Database, req.Table, req.TTL); err != nil {
		serverLog.Error().
			Err(err).
			Str("database", req.Database).
			Str("table", req.Table).
			Msg("Failed to modify TTL")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
