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
	return api.New([]string{dir}, coordinator.Options{
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

func TestPostSync_RequiresCanary(t *testing.T) {
	dir := t.TempDir()
	// No canary → coordinator should return an error.
	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 envelope, got %d", rr.Code)
	}
	var st api.Status
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
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

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var st api.Status
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
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

func TestPostScrub_Success(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)

	srv := newTestServer(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/api/scrub", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestConflictWhenAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	writeCanaryFile(t, dir)

	srv := newTestServer(t, dir)

	// Issue two concurrent requests.
	req1 := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rr1 := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(rr1, req1)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rr2 := httptest.NewRecorder()
	srv.ServeHTTP(rr2, req2)

	<-done

	// Accept 200 (first finished) or 409 (genuinely concurrent).
	if rr2.Code != http.StatusOK && rr2.Code != http.StatusConflict {
		t.Errorf("expected 200 or 409, got %d", rr2.Code)
	}
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

	// Trigger a sync.
	syncReq := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	syncRR := httptest.NewRecorder()
	srv.ServeHTTP(syncRR, syncReq)

	// Then query status.
	statusReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	statusRR := httptest.NewRecorder()
	srv.ServeHTTP(statusRR, statusReq)

	var st api.Status
	if err := json.Unmarshal(statusRR.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.LastRun.IsZero() {
		t.Error("expected LastRun to be set after sync")
	}
	if len(st.Drives) == 0 {
		t.Error("expected drives in status after sync")
	}
}

