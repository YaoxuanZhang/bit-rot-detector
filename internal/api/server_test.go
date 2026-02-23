package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/api"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
)

// newTestServer returns a *Server wired to a real temp directory.
func newTestServer(t *testing.T, dir string) *api.Server {
	t.Helper()
	return api.New(context.Background(), []string{dir}, coordinator.Options{
		RunSync:         true,
		RunScrub:        true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})
}

// writeCanaryFile creates an empty .bitrot-canary so coordinator.Run can proceed.
func writeCanaryFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".bitrot-canary"), nil, 0o644); err != nil {
		t.Fatalf("writeCanaryFile: %v", err)
	}
}

// pollStatus polls GET /api/status until Running is false or timeout.
func pollStatus(t *testing.T, srv *api.Server) api.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		var st api.Status
		if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
			t.Fatalf("pollStatus: unmarshal: %v", err)
		}
		if !st.Running {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pollStatus: operation did not complete within timeout")
	return api.Status{}
}

func TestGetStatus_InitiallyEmpty(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var st api.Status
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Running {
		t.Error("expected Running=false on fresh server")
	}
}

func TestGetDrives(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/drives", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["paths"]; !ok {
		t.Error("expected 'paths' key in drives response")
	}
}

func TestPostSync_Returns202(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)

	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Errorf("expected status=accepted, got %v", resp)
	}
}

func TestPostSync_RequiresCanary(t *testing.T) {
	dir := t.TempDir()
	// No canary → coordinator should return an error after async completion.
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	// Poll until complete, then check for error.
	st := pollStatus(t, srv)
	if len(st.Drives) == 0 {
		t.Fatal("expected at least one drive result")
	}
	if st.Drives[0].Err == "" {
		t.Error("expected error string for missing canary")
	}
}

func TestPostSync_Success(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)

	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	st := pollStatus(t, srv)
	if st.Running {
		t.Error("expected Running=false after sync completes")
	}
	if len(st.Drives) == 0 {
		t.Error("expected drive results after sync")
	}
	if st.Drives[0].Err != "" {
		t.Errorf("unexpected drive error: %s", st.Drives[0].Err)
	}
}

func TestPostScrub_Returns202(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)

	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/scrub", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	// Wait for the background scrub goroutine to finish so that t.TempDir()
	// cleanup does not race with the still-running coordinator.
	pollStatus(t, srv)
}

func TestConflictWhenAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)

	srv := newTestServer(t, dir)

	// Issue first request — returns 202 immediately.
	req1 := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rr1 := httptest.NewRecorder()
	srv.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first request: expected 202, got %d", rr1.Code)
	}

	// Issue second request while first is still running.
	req2 := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rr2 := httptest.NewRecorder()
	srv.ServeHTTP(rr2, req2)

	// Expect either 409 (caught the race) or 202 (first already finished).
	if rr2.Code != http.StatusConflict && rr2.Code != http.StatusAccepted {
		t.Errorf("second request: expected 409 or 202, got %d", rr2.Code)
	}

	// Wait for all background goroutines to finish before TempDir cleanup.
	pollStatus(t, srv)
}

func TestStaticUI_Served(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Bit Rot Detector") {
		t.Error("expected HTML page title in response")
	}
}

func TestServer_ListenAndServe_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ListenAndServe: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("ListenAndServe did not stop after context cancellation")
	}
}

func TestStatusAfterSync_Populated(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, dir)

	// Trigger a sync (async).
	syncReq := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	syncRR := httptest.NewRecorder()
	srv.ServeHTTP(syncRR, syncReq)

	if syncRR.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", syncRR.Code)
	}

	// Poll until done, then verify status is populated.
	st := pollStatus(t, srv)
	if st.LastRun.IsZero() {
		t.Error("expected LastRun to be set after sync")
	}
	if len(st.Drives) == 0 {
		t.Error("expected drives in status after sync")
	}
}

func TestGetProgress_SSE_Headers(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/progress", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(rr, req)
	}()

	// Give handler time to write headers.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not exit after context cancel")
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
}

func TestGetProgress_SSE_ReceivesEvents(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, dir)

	// Subscribe to SSE before triggering operation.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sseReq := httptest.NewRequest(http.MethodGet, "/api/progress", nil).WithContext(ctx)
	sseRR := httptest.NewRecorder()

	sseDone := make(chan struct{})
	go func() {
		defer close(sseDone)
		srv.ServeHTTP(sseRR, sseReq)
	}()

	// Trigger a sync.
	syncReq := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	syncRR := httptest.NewRecorder()
	srv.ServeHTTP(syncRR, syncReq)
	if syncRR.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", syncRR.Code)
	}

	// Wait for operation to complete and cancel SSE.
	pollStatus(t, srv)
	cancel()
	<-sseDone

	// Body should contain at least one SSE data: line.
	body := sseRR.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected at least one SSE data line, got body: %q", body[:min(200, len(body))])
	}
}

func TestGetHistory_EmptyInitially(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var records []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &records); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// No DB exists yet → empty array.
	if len(records) != 0 {
		t.Errorf("expected empty history before any run, got %d records", len(records))
	}
}

func TestGetHistory_PopulatedAfterSync(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, dir)

	// Trigger sync and wait for completion.
	syncReq := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	syncRR := httptest.NewRecorder()
	srv.ServeHTTP(syncRR, syncReq)
	if syncRR.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", syncRR.Code)
	}
	pollStatus(t, srv)

	// Now history should contain one record.
	histReq := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	histRR := httptest.NewRecorder()
	srv.ServeHTTP(histRR, histReq)

	if histRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", histRR.Code, histRR.Body.String())
	}
	var records []map[string]interface{}
	if err := json.Unmarshal(histRR.Body.Bytes(), &records); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(records) == 0 {
		t.Error("expected at least one run history record after sync")
	}
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
