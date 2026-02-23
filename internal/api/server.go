// Package api provides an HTTP server that exposes the bit-rot detector's
// status and operations via a JSON REST API and serves an embedded single-page
// web UI.
//
// # Endpoints
//
//   - GET  /api/status    – last-run summary for all drives
//   - GET  /api/drives    – configured drive paths and health
//   - GET  /api/progress  – Server-Sent Events stream of live scan progress
//   - GET  /api/history   – historical run records from each drive's database
//   - POST /api/sync      – trigger a sync operation (non-blocking; returns 202 Accepted)
//   - POST /api/scrub     – trigger a scrub operation (non-blocking; returns 202 Accepted)
//   - GET  /              – single-page web UI
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/storage"
)

// progressHub broadcasts ProgressEvents to all registered SSE subscribers.
// All methods are safe for concurrent use.
type progressHub struct {
	mu   sync.Mutex
	subs map[chan domain.ProgressEvent]struct{}
}

func newProgressHub() *progressHub {
	return &progressHub{subs: make(map[chan domain.ProgressEvent]struct{})}
}

// subscribe registers a new subscriber and returns its channel plus an
// unsubscribe function.  The caller must invoke unsub when done.
func (h *progressHub) subscribe() (<-chan domain.ProgressEvent, func()) {
	ch := make(chan domain.ProgressEvent, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// publish sends ev to every registered subscriber, dropping silently when a
// subscriber's buffer is full.
func (h *progressHub) publish(ev domain.ProgressEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Server is the HTTP API server.  Create one with [New], then start it with
// [Server.ListenAndServe].
type Server struct {
	ctx    context.Context // server-lifetime context used for background operations
	opts   coordinator.Options
	paths  []string
	mu     sync.RWMutex
	status *Status
	hub    *progressHub
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
// ctx is the server's lifetime context; background operations (sync/scrub)
// are cancelled when ctx is cancelled.
// The server starts with an empty status until the first operation is triggered.
func New(ctx context.Context, paths []string, opts coordinator.Options) *Server {
	s := &Server{
		ctx:  ctx,
		opts: opts,
		paths: paths,
		status: &Status{
			Drives: []DriveStatus{},
		},
		hub: newProgressHub(),
	}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8080").
// It blocks until ctx is cancelled or a fatal listen error occurs.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      120 * time.Second, // long scrub operations
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB (1,048,576 bytes)
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
	s.mux.HandleFunc("GET /api/progress", s.handleProgress)
	s.mux.HandleFunc("GET /api/history", s.handleHistory)
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

// handleProgress is a Server-Sent Events endpoint that streams live
// ProgressEvents to the client during sync/scrub operations.
// Each event is encoded as: data: <json>\n\n
// The connection is kept open until the client disconnects or ctx is cancelled.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	// Disable the per-request write deadline so the SSE stream can remain
	// open for the full duration of a long scan.
	// http.NewResponseController never returns nil; the only error is when
	// the underlying ResponseWriter does not support deadline control
	// (e.g. some middleware wrappers), which we log and ignore.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Warn("api: could not clear write deadline for SSE", "err", err)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported by this transport", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable Nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := s.hub.subscribe()
	defer unsub()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// handleHistory returns historical run records from each configured drive's
// database.  The optional "limit" query parameter controls the maximum number
// of records returned per drive (default 50).
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}

	var all []*domain.RunRecord
	for _, p := range s.paths {
		recs, err := storage.GetRunHistoryForPath(r.Context(), p, limit)
		if err != nil {
			slog.Warn("api: get history", "path", p, "err", err)
			continue
		}
		all = append(all, recs...)
	}
	if all == nil {
		all = []*domain.RunRecord{} // return [] not null
	}
	writeJSON(w, http.StatusOK, all)
}

// handleSync triggers a sync operation (non-blocking; returns 202 Accepted).
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	opts := s.opts
	opts.RunSync = true
	opts.RunScrub = false
	s.startOperation(w, opts)
}

// handleScrub triggers a scrub operation (non-blocking; returns 202 Accepted).
func (s *Server) handleScrub(w http.ResponseWriter, r *http.Request) {
	opts := s.opts
	opts.RunSync = false
	opts.RunScrub = true
	s.startOperation(w, opts)
}

// startOperation launches a coordinator.Run in the background and immediately
// returns 202 Accepted to the caller.  The caller polls GET /api/status for
// results.  Returns 409 Conflict if an operation is already in progress.
func (s *Server) startOperation(w http.ResponseWriter, opts coordinator.Options) {
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

	// Acknowledge immediately so the client is not blocked waiting for I/O.
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})

	// Create a buffered progress channel; a forwarder goroutine broadcasts
	// events from it to all registered SSE subscribers.
	progCh := make(chan domain.ProgressEvent, 256)
	opts.ProgressCh = progCh

	go func() {
		for ev := range progCh {
			s.hub.publish(ev)
		}
	}()

	// Run the operation in the background using the server's lifetime context
	// so it is cancelled on graceful shutdown rather than on HTTP disconnect.
	go func() {
		defer close(progCh)
		results, duration := coordinator.Run(s.ctx, s.paths, opts)

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

		s.mu.Lock()
		s.status = &Status{
			LastRun:  time.Now(),
			Duration: duration.String(),
			Running:  false,
			Drives:   drives,
		}
		s.mu.Unlock()
	}()
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("api: failed to encode response", "err", err)
	}
}
