// Package api provides an HTTP server that exposes the bit-rot detector's
// status and operations via a JSON REST API and serves an embedded single-page
// web UI.
//
// # Endpoints
//
//   - GET  /api/status    – last-run summary for all drives
//   - GET  /api/drives    – configured drive paths and health
//   - POST /api/sync      – trigger a sync operation (non-blocking; streams JSON event)
//   - POST /api/scrub     – trigger a scrub operation (non-blocking; streams JSON event)
//   - GET  /              – single-page web UI
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
)

// Server is the HTTP API server.  Create one with [New], then start it with
// [Server.ListenAndServe].
type Server struct {
	opts   coordinator.Options
	paths  []string
	mu     sync.RWMutex
	status *Status
	mux    *http.ServeMux
}

// Status holds the most recent aggregated scan results returned by the server.
type Status struct {
	// LastRun is the time the most recent operation completed.
	LastRun time.Time `json:"last_run"`

	// Duration is the wall-clock time of the most recent operation.
	Duration string `json:"duration"`

	// Running is true when an operation is currently in progress.
	Running bool `json:"running"`

	// Drives contains per-drive results from the most recent run.
	Drives []DriveStatus `json:"drives"`
}

// DriveStatus is the API representation of a single drive's result.
type DriveStatus struct {
	// Drive is the human-readable drive label.
	Drive string `json:"drive"`

	// Path is the absolute mount-point path.
	Path string `json:"path"`

	// Health contains disk-usage statistics.
	Health *domain.DriveHealth `json:"health,omitempty"`

	// SyncResult is the most recent sync outcome, or nil.
	SyncResult *domain.SyncResult `json:"sync_result,omitempty"`

	// ScrubResult is the most recent scrub outcome, or nil.
	ScrubResult *domain.ScrubResult `json:"scrub_result,omitempty"`

	// Err is a non-empty string when a fatal error occurred.
	Err string `json:"err,omitempty"`
}

// New creates a Server for the given target paths and coordinator options.
// The server starts with an empty status until the first operation is triggered.
func New(paths []string, opts coordinator.Options) *Server {
	s := &Server{
		opts:  opts,
		paths: paths,
		status: &Status{
			Drives: []DriveStatus{},
		},
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8080").
// It blocks until ctx is cancelled or a fatal listen error occurs.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:        addr,
		Handler:     s.mux,
		ReadTimeout: 15 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Warn("api: shutdown error", "err", err)
		}
	}()
	slog.Info("api: server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ServeHTTP implements http.Handler so the server can be used directly with
// httptest.NewRecorder in tests without binding a real TCP port.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// registerRoutes wires up all HTTP handlers.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/drives", s.handleDrives)
	s.mux.HandleFunc("POST /api/sync", s.handleSync)
	s.mux.HandleFunc("POST /api/scrub", s.handleScrub)
	s.mux.Handle("/", http.FileServerFS(staticFS))
}

// handleStatus returns the most recent aggregated run status as JSON.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	st := *s.status // value copy to avoid race with concurrent runOperation
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, &st)
}

// handleDrives returns the list of configured drives with their current health.
func (s *Server) handleDrives(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	st := *s.status // value copy to avoid race with concurrent runOperation
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"paths":  s.paths,
		"drives": st.Drives,
	})
}

// handleSync triggers a sync operation.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	opts := s.opts
	opts.RunSync = true
	opts.RunScrub = false
	s.runOperation(r.Context(), w, opts)
}

// handleScrub triggers a scrub operation.
func (s *Server) handleScrub(w http.ResponseWriter, r *http.Request) {
	opts := s.opts
	opts.RunSync = false
	opts.RunScrub = true
	s.runOperation(r.Context(), w, opts)
}

// runOperation runs a coordinator.Run and updates the server's status.
// It writes the result as a JSON response.
func (s *Server) runOperation(ctx context.Context, w http.ResponseWriter, opts coordinator.Options) {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "an operation is already in progress",
		})
		return
	}
	s.status.Running = true
	s.mu.Unlock()

	results, duration := coordinator.Run(ctx, s.paths, opts)

	drives := make([]DriveStatus, len(results))
	for i, r := range results {
		ds := DriveStatus{
			Drive:       r.Drive,
			Path:        s.paths[i],
			Health:      r.Health,
			SyncResult:  r.SyncResult,
			ScrubResult: r.ScrubResult,
		}
		if r.Err != nil {
			ds.Err = r.Err.Error()
		}
		drives[i] = ds
	}

	newStatus := &Status{
		LastRun:  time.Now(),
		Duration: duration.String(),
		Running:  false,
		Drives:   drives,
	}
	s.mu.Lock()
	s.status = newStatus
	// Take a value copy while holding the lock so the JSON encoder does not
	// race with a concurrent runOperation that may set Running=true on the
	// shared pointer.
	response := *newStatus
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, &response)
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("api: failed to encode response", "err", err)
	}
}
