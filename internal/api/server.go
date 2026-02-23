// Package api provides an HTTP server that exposes the bit-rot detector's
// status and operations via a JSON REST API and serves an embedded single-page
// web UI.
//
// # Endpoints
//
//   - GET  /api/status            – last-run summary for all drives
//   - GET  /api/drives            – configured drive paths and health
//   - GET  /api/progress          – Server-Sent Events stream of live scan progress
//   - GET  /api/history           – historical run records from each drive's database
//   - GET  /api/export            – export run history (format=json|csv)
//   - GET  /api/compare           – compare two runs by ID (a=&b=)
//   - GET  /api/corruption        – corruption events across all drives
//   - GET  /api/settings          – current settings
//   - POST /api/settings          – update settings
//   - GET  /api/schedule          – current schedule entries
//   - POST /api/schedule          – upsert a schedule entry
//   - GET  /api/retries           – current retry queue
//   - POST /api/sync              – trigger a sync operation (non-blocking; returns 202 Accepted)
//   - POST /api/scrub             – trigger a scrub operation (non-blocking; returns 202 Accepted)
//   - POST /api/drives/{idx}/sync – per-drive sync
//   - POST /api/drives/{idx}/scrub – per-drive scrub
//   - POST /api/test-email        – send a test email via the configured mailer
//   - GET  /                      – single-page web UI
package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/retry"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/scheduler"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/settings"
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

// EmailSender is a minimal interface satisfied by *mailer.Mailer.
// It allows the API server to be tested without a real SMTP connection.
type EmailSender interface {
	SendTestEmail() error
}

// CorruptionEvent is a summary of a single run that detected bit rot.
type CorruptionEvent struct {
	DriveID        string    `json:"drive_id"`
	DriveName      string    `json:"drive_name"`
	StartedAt      time.Time `json:"started_at"`
	FilesCorrupted int       `json:"files_corrupted"`
	RunID          int64     `json:"run_id"`
}

// Server is the HTTP API server.  Create one with [New], then start it with
// [Server.ListenAndServe].
type Server struct {
	ctx       context.Context // server-lifetime context used for background operations
	opts      coordinator.Options
	paths     []string
	mu        sync.RWMutex
	status    *Status
	hub       *progressHub
	mux       *http.ServeMux
	mailer    EmailSender    // optional; set via SetMailer
	settings  *settings.Store
	scheduler *scheduler.Store
	retryQ    *retry.Queue
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

// SetMailer wires in an optional EmailSender used by POST /api/test-email.
func (s *Server) SetMailer(m EmailSender) { s.mailer = m }

// WithSettings injects a settings store.
func (s *Server) WithSettings(ss *settings.Store) { s.settings = ss }

// WithScheduler injects a scheduler store.
func (s *Server) WithScheduler(sc *scheduler.Store) { s.scheduler = sc }

// WithRetryQueue injects a retry queue.
func (s *Server) WithRetryQueue(q *retry.Queue) { s.retryQ = q }

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
	s.mux.HandleFunc("GET /api/export", s.handleExport)
	s.mux.HandleFunc("GET /api/compare", s.handleCompare)
	s.mux.HandleFunc("GET /api/corruption", s.handleCorruption)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("POST /api/settings", s.handlePostSettings)
	s.mux.HandleFunc("GET /api/schedule", s.handleGetSchedule)
	s.mux.HandleFunc("POST /api/schedule", s.handlePostSchedule)
	s.mux.HandleFunc("GET /api/retries", s.handleGetRetries)
	s.mux.HandleFunc("POST /api/sync", s.handleSync)
	s.mux.HandleFunc("POST /api/scrub", s.handleScrub)
	s.mux.HandleFunc("POST /api/drives/{idx}/sync", s.handleDriveSync)
	s.mux.HandleFunc("POST /api/drives/{idx}/scrub", s.handleDriveScrub)
	s.mux.HandleFunc("POST /api/test-email", s.handleTestEmail)
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
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
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

// handleExport streams run history as JSON or CSV.
// Query params: format=json|csv (default json), type=history|corruption (default history).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	var all []*domain.RunRecord
	for _, p := range s.paths {
		recs, err := storage.GetRunHistoryForPath(r.Context(), p, 10000)
		if err != nil {
			slog.Warn("api: export history", "path", p, "err", err)
			continue
		}
		all = append(all, recs...)
	}
	if all == nil {
		all = []*domain.RunRecord{}
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="bitrot-history.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{
			"id", "drive_id", "drive_name", "started_at", "duration_ms",
			"files_scanned", "files_added", "files_modified", "files_removed", "files_moved",
			"files_validated", "files_corrupted", "sync_errors", "scrub_errors",
		})
		for _, rec := range all {
			_ = cw.Write([]string{
				strconv.FormatInt(rec.ID, 10),
				rec.DriveID,
				rec.DriveName,
				rec.StartedAt.Format(time.RFC3339),
				strconv.FormatInt(rec.DurationMs, 10),
				strconv.Itoa(rec.FilesScanned),
				strconv.Itoa(rec.FilesAdded),
				strconv.Itoa(rec.FilesModified),
				strconv.Itoa(rec.FilesRemoved),
				strconv.Itoa(rec.FilesMoved),
				strconv.Itoa(rec.FilesValidated),
				strconv.Itoa(rec.FilesCorrupted),
				strconv.Itoa(rec.SyncErrors),
				strconv.Itoa(rec.ScrubErrors),
			})
		}
		cw.Flush()
		return
	}

	writeJSON(w, http.StatusOK, all)
}

// handleCompare computes the delta between two run records identified by the
// "a" and "b" query parameters (int64 IDs).
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	aStr := r.URL.Query().Get("a")
	bStr := r.URL.Query().Get("b")
	if aStr == "" || bStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing a or b query params"})
		return
	}
	idA, err := strconv.ParseInt(aStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid a"})
		return
	}
	idB, err := strconv.ParseInt(bStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid b"})
		return
	}

	var runA, runB *domain.RunRecord
	for _, p := range s.paths {
		recs, err := storage.GetRunsByIDs(r.Context(), p, []int64{idA, idB})
		if err != nil {
			slog.Warn("api: compare GetRunsByIDs", "path", p, "err", err)
			continue
		}
		for _, rec := range recs {
			if rec.ID == idA && runA == nil {
				runA = rec
			}
			if rec.ID == idB && runB == nil {
				runB = rec
			}
		}
		if runA != nil && runB != nil {
			break
		}
	}

	if runA == nil || runB == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "one or both runs not found"})
		return
	}

	delta := domain.RunDelta{
		FilesAdded:     runB.FilesAdded - runA.FilesAdded,
		FilesModified:  runB.FilesModified - runA.FilesModified,
		FilesRemoved:   runB.FilesRemoved - runA.FilesRemoved,
		FilesCorrupted: runB.FilesCorrupted - runA.FilesCorrupted,
		DurationMs:     runB.DurationMs - runA.DurationMs,
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run_a": runA,
		"run_b": runB,
		"delta": delta,
	})
}

// handleCorruption returns all run_history rows where files_corrupted > 0
// across all configured drives.
func (s *Server) handleCorruption(w http.ResponseWriter, r *http.Request) {
	var events []CorruptionEvent
	for _, p := range s.paths {
		recs, err := storage.GetCorruptionHistory(r.Context(), p, 100)
		if err != nil {
			slog.Warn("api: corruption history", "path", p, "err", err)
			continue
		}
		for _, rec := range recs {
			events = append(events, CorruptionEvent{
				DriveID:        rec.DriveID,
				DriveName:      rec.DriveName,
				StartedAt:      rec.StartedAt,
				FilesCorrupted: rec.FilesCorrupted,
				RunID:          rec.ID,
			})
		}
	}
	if events == nil {
		events = []CorruptionEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// handleGetSettings returns the current settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeJSON(w, http.StatusOK, settings.DefaultSettings())
		return
	}
	writeJSON(w, http.StatusOK, s.settings.Get())
}

// handlePostSettings updates the settings from the request body.
func (s *Server) handlePostSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "settings not configured"})
		return
	}
	var v settings.Settings
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.settings.Set(v); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.settings.Get())
}

// handleGetSchedule returns current schedule entries.
func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		writeJSON(w, http.StatusOK, []scheduler.ScheduleEntry{})
		return
	}
	entries := s.scheduler.GetAll()
	if entries == nil {
		entries = []scheduler.ScheduleEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// handlePostSchedule upserts a schedule entry from the request body.
func (s *Server) handlePostSchedule(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not configured"})
		return
	}
	var e scheduler.ScheduleEntry
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.scheduler.Upsert(e); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	entries := s.scheduler.GetAll()
	if entries == nil {
		entries = []scheduler.ScheduleEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleGetRetries returns the current retry queue snapshot.
func (s *Server) handleGetRetries(w http.ResponseWriter, r *http.Request) {
	if s.retryQ == nil {
		writeJSON(w, http.StatusOK, []retry.Item{})
		return
	}
	items := s.retryQ.All()
	if items == nil {
		items = []retry.Item{}
	}
	writeJSON(w, http.StatusOK, items)
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

// handleDriveSync triggers a sync for a single drive identified by the {idx}
// path parameter (0-based index into s.paths).
func (s *Server) handleDriveSync(w http.ResponseWriter, r *http.Request) {
	idx, ok := s.parseDriveIdx(w, r)
	if !ok {
		return
	}
	opts := s.opts
	opts.RunSync = true
	opts.RunScrub = false
	s.startOperationForPaths(w, []string{s.paths[idx]}, opts)
}

// handleDriveScrub triggers a scrub for a single drive identified by the {idx}
// path parameter (0-based index into s.paths).
func (s *Server) handleDriveScrub(w http.ResponseWriter, r *http.Request) {
	idx, ok := s.parseDriveIdx(w, r)
	if !ok {
		return
	}
	opts := s.opts
	opts.RunSync = false
	opts.RunScrub = true
	s.startOperationForPaths(w, []string{s.paths[idx]}, opts)
}

// parseDriveIdx extracts and validates the {idx} path value.
func (s *Server) parseDriveIdx(w http.ResponseWriter, r *http.Request) (int, bool) {
	idxStr := r.PathValue("idx")
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(s.paths) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid drive index"})
		return 0, false
	}
	return idx, true
}

// handleTestEmail sends a test email via the configured mailer.
func (s *Server) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	if s.mailer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no mailer configured"})
		return
	}
	if err := s.mailer.SendTestEmail(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// startOperation launches a coordinator.Run in the background for all
// configured paths and immediately returns 202 Accepted.
func (s *Server) startOperation(w http.ResponseWriter, opts coordinator.Options) {
	s.startOperationForPaths(w, s.paths, opts)
}

// startOperationForPaths launches a coordinator.Run in the background for the
// given paths and immediately returns 202 Accepted to the caller.
// Returns 409 Conflict if an operation is already in progress.
func (s *Server) startOperationForPaths(w http.ResponseWriter, paths []string, opts coordinator.Options) {
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
		results, duration := coordinator.Run(s.ctx, paths, opts)

		drives := make([]DriveStatus, len(results))
		for i, r := range results {
			ds := DriveStatus{
				Drive:       r.Drive,
				Path:        paths[i],
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

