package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type Server struct {
	state    *State
	socket   string
	server   *http.Server
	interval time.Duration
	stopCh   chan struct{}
}

func NewServer(state *State, socket string, interval time.Duration) *Server {
	return &Server{
		state:    state,
		socket:   socket,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (s *Server) Start() error {
	if err := os.Remove(s.socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket: %w", err)
	}

	ln, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	if err := os.Chmod(s.socket, 0777); err != nil {
		return fmt.Errorf("failed to chmod socket: %w", err)
	}

	s.server = &http.Server{
		Handler: s,
	}

	go s.server.Serve(ln)
	go s.pollLoop()

	log.Printf("Daemon listening on %s", s.socket)
	return nil
}

func (s *Server) Stop() error {
	close(s.stopCh)
	return s.server.Close()
}

func (s *Server) pollLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	if err := s.state.Poll(); err != nil {
		fmt.Printf("Initial poll failed: %v\n", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := s.state.Poll(); err != nil {
				fmt.Printf("Poll failed: %v\n", err)
			}
		case <-s.stopCh:
			return
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("[client] %s %s", r.Method, r.URL.Path)

	path := strings.TrimPrefix(r.URL.Path, "/api")
	path = strings.TrimPrefix(path, "/")

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
		http.NotFound(w, r)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	connected := s.state.IsConnected()
	log.Printf("[status] connected=%v", connected)

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
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func (s *Server) handleTables(w http.ResponseWriter, r *http.Request) {
	tables := s.state.GetTables()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tables)
}

func (s *Server) handleTruncatables(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tables, err := s.state.GetTruncatableTables(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tables)
}

func (s *Server) handleQueries(w http.ResponseWriter, r *http.Request) {
	queries := s.state.GetQueries()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queries)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	metric := parts[0]
	period := parts[1]

	samples, err := s.state.QueryHistory(metric, period)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(samples)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Database string `json:"database"`
		Table    string `json:"table"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := s.state.TruncateTable(ctx, req.Database, req.Table); err != nil {
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := s.state.ModifyTTL(ctx, req.Database, req.Table, req.TTL); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
