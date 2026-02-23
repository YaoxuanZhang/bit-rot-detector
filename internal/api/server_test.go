package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/api"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/retry"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/scheduler"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/settings"
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

func TestPostDriveScrub_ValidIdx(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/drives/0/scrub", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	pollStatus(t, srv)
}

func TestPostDriveScrub_InvalidIdx(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/drives/99/scrub", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetSettings_NoStore_ReturnsDefaults(t *testing.T) {
	// When no settings store is injected, GET /api/settings returns defaults.
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["disk_thresholds"] == nil {
		t.Error("expected disk_thresholds key in default settings")
	}
}

func TestPostSettings_NoStore_Returns503(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{}`))
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestPostSettings_BadJSON_Returns400(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	srv.WithSettings(settings.New(""))
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`not json`))
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetSchedule_NoStore_ReturnsEmpty(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/schedule", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var entries []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty schedule, got %d", len(entries))
	}
}

func TestPostSchedule_NoStore_Returns503(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	body := `{"id":"sync","label":"Daily","enabled":true,"cron_expr":"@daily"}`
	req := httptest.NewRequest(http.MethodPost, "/api/schedule", strings.NewReader(body))
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestPostSchedule_BadJSON_Returns400(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	srv.WithScheduler(scheduler.New(""))
	req := httptest.NewRequest(http.MethodPost, "/api/schedule", strings.NewReader(`not json`))
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetRetries_NoQueue_ReturnsEmpty(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/retries", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var items []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty retries, got %d", len(items))
	}
}

func TestGetRetries_WithItems(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	q := retry.New(10)
	q.Add("/data", "sync", "permission denied")
	srv.WithRetryQueue(q)

	req := httptest.NewRequest(http.MethodGet, "/api/retries", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["path"] != "/data" {
		t.Errorf("expected path=/data, got %v", items[0]["path"])
	}
}

func TestGetCompare_InvalidA(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/compare?a=notanint&b=2", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetCompare_InvalidB(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/compare?a=1&b=notanint", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetCompare_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	srv := newTestServer(t, dir)

	// Trigger a sync to create the DB.
	syncReq := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	syncRR  := httptest.NewRecorder()
	srv.ServeHTTP(syncRR, syncReq)
	pollStatus(t, srv)

	// IDs 9999 and 9998 almost certainly do not exist.
	req := httptest.NewRequest(http.MethodGet, "/api/compare?a=9999&b=9998", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetCompare_TwoRuns(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, dir)

	// Run sync twice to produce two run history records.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
		rr  := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("sync %d: expected 202, got %d", i, rr.Code)
		}
		pollStatus(t, srv)
	}

	// Fetch history to get the two run IDs.
	histReq := httptest.NewRequest(http.MethodGet, "/api/history?limit=2", nil)
	histRR  := httptest.NewRecorder()
	srv.ServeHTTP(histRR, histReq)
	if histRR.Code != http.StatusOK {
		t.Fatalf("history: expected 200, got %d", histRR.Code)
	}
	var records []map[string]interface{}
	if err := json.Unmarshal(histRR.Body.Bytes(), &records); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(records) < 2 {
		t.Skipf("need at least 2 history records; got %d — skipping comparison test", len(records))
	}

	idA := records[len(records)-1]["id"]
	idB := records[len(records)-2]["id"]

	// Compare the two runs.
	url := fmt.Sprintf("/api/compare?a=%v&b=%v", idA, idB)
	cmpReq := httptest.NewRequest(http.MethodGet, url, nil)
	cmpRR  := httptest.NewRecorder()
	srv.ServeHTTP(cmpRR, cmpReq)
	if cmpRR.Code != http.StatusOK {
		t.Fatalf("compare: expected 200, got %d: %s", cmpRR.Code, cmpRR.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(cmpRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal compare: %v", err)
	}
	if resp["run_a"] == nil || resp["run_b"] == nil || resp["delta"] == nil {
		t.Errorf("expected run_a, run_b, delta keys in response; got: %v", resp)
	}
}

func TestGetExport_CSV_EmptyHistory(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("expected text/csv Content-Type, got %q", ct)
	}
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

// ── stub mailer ────────────────────────────────────────────────────────────────

type stubMailer struct{ err error }

func (s *stubMailer) SendTestEmail() error { return s.err }

// ── TestPostTestEmail ──────────────────────────────────────────────────────────

func TestPostTestEmail_NoMailer(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/test-email", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestPostTestEmail_Success(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	srv.SetMailer(&stubMailer{err: nil})
	req := httptest.NewRequest(http.MethodPost, "/api/test-email", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "sent" {
		t.Errorf("expected status=sent, got %v", resp)
	}
}

func TestPostTestEmail_Failure(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	srv.SetMailer(&stubMailer{err: errors.New("smtp down")})
	req := httptest.NewRequest(http.MethodPost, "/api/test-email", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ── TestGetExport ──────────────────────────────────────────────────────────────

func TestGetExport_JSON(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	srv := newTestServer(t, dir)

	// Trigger a sync and wait.
	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("sync: %d", rr.Code)
	}
	pollStatus(t, srv)

	req = httptest.NewRequest(http.MethodGet, "/api/export?format=json", nil)
	rr  = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content-type, got %q", ct)
	}
	var records []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &records); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestGetExport_CSV(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Errorf("expected text/csv, got %q", ct)
	}
}

// ── TestGetCompare ─────────────────────────────────────────────────────────────

func TestGetCompare_MissingParams(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/compare", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ── TestGetCorruption ──────────────────────────────────────────────────────────

func TestGetCorruption_Empty(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/corruption", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var events []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty events, got %d", len(events))
	}
}

// ── TestSettings ───────────────────────────────────────────────────────────────

func TestGetSettings_Default(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	ss  := settings.New("")
	srv.WithSettings(ss)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var got settings.Settings
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.DiskThresholds.WarnPercent != 75 {
		t.Errorf("expected warn_pct=75, got %d", got.DiskThresholds.WarnPercent)
	}
}

func TestPostSettings_Update(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	ss  := settings.New("")
	srv.WithSettings(ss)

	body := `{"disk_thresholds":{"warn_pct":80,"error_pct":95},"notification_rules":{"on_success":true,"on_warning":true,"on_corruption":true,"on_error":true}}`
	req  := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	rr   := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Re-read
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr  = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	var got settings.Settings
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.DiskThresholds.WarnPercent != 80 {
		t.Errorf("expected warn_pct=80, got %d", got.DiskThresholds.WarnPercent)
	}
}

// ── TestSchedule ───────────────────────────────────────────────────────────────

func TestGetSchedule_Default(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	sc  := scheduler.New("")
	srv.WithScheduler(sc)

	req := httptest.NewRequest(http.MethodGet, "/api/schedule", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var entries []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Default store is empty.
	if entries == nil {
		t.Error("expected non-nil array")
	}
}

func TestPostSchedule_Upsert(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	sc  := scheduler.New("")
	srv.WithScheduler(sc)

	body := `{"id":"sync","label":"Daily Sync","enabled":true,"cron_expr":"@daily"}`
	req  := httptest.NewRequest(http.MethodPost, "/api/schedule", strings.NewReader(body))
	rr   := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Re-read
	req = httptest.NewRequest(http.MethodGet, "/api/schedule", nil)
	rr  = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	var entries []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatalf("re-read unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if entries[0]["id"] != "sync" {
		t.Errorf("expected id=sync, got %v", entries[0]["id"])
	}
}

// ── TestRetries ────────────────────────────────────────────────────────────────

func TestGetRetries_Empty(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	q   := retry.New(100)
	srv.WithRetryQueue(q)

	req := httptest.NewRequest(http.MethodGet, "/api/retries", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var items []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty, got %d", len(items))
	}
}

// ── TestDriveSync ─────────────────────────────────────────────────────────────

func TestPostDriveSync_ValidIdx(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/drives/0/sync", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	pollStatus(t, srv)
}

func TestPostDriveSync_InvalidIdx(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/drives/99/sync", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
