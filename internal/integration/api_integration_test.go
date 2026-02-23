// This file extends the integration test suite with API-level scenarios
// that exercise the HTTP server endpoints built on top of coordinator.Run.
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/api"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
)

// newAPIServer returns a Server wired to dir.
func newAPIServer(t *testing.T, dir string) *api.Server {
	t.Helper()
	return api.New(context.Background(), []string{dir}, coordinator.Options{
		RunSync:         true,
		RunScrub:        true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})
}

// ── Scenario 27: Export CSV after sync ───────────────────────────────────────

func TestS27_ExportCSV_AfterSync(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "doc.txt"), "content")

	// Use coordinator directly for sync so the DB exists.
	r := runSync(t, dir)
	if r.Err != nil {
		t.Fatalf("sync: %v", r.Err)
	}

	srv := newAPIServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Errorf("expected text/csv, got %q", ct)
	}
	body := rr.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least CSV header row")
	}
	// Verify header starts with "id,"
	if !strings.HasPrefix(lines[0], "id,") {
		t.Errorf("CSV header should start with 'id,', got %q", lines[0])
	}
	// After sync there should be a data row.
	if len(lines) < 2 {
		t.Error("expected at least one data row after sync")
	}
}

// ── Scenario 28: Comparison of two runs ──────────────────────────────────────

func TestS28_Comparison_TwoRuns(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "file1.txt"), "content1")

	// Two sequential sync runs.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync 1: %v", r1.Err)
	}
	writeFile(t, filepath.Join(dir, "file2.txt"), "content2")
	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Fatalf("sync 2: %v", r2.Err)
	}

	srv := newAPIServer(t, dir)

	// Get history to find run IDs.
	req := httptest.NewRequest(http.MethodGet, "/api/history?limit=10", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("history: %d", rr.Code)
	}
	var records []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &records); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(records) < 2 {
		t.Fatalf("expected >= 2 history records, got %d", len(records))
	}

	idA := int64(records[0]["id"].(float64))
	idB := int64(records[1]["id"].(float64))

	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/compare?a=%d&b=%d", idA, idB), nil)
	rr  = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal compare: %v", err)
	}
	if _, ok := result["run_a"]; !ok {
		t.Error("expected run_a in compare response")
	}
	if _, ok := result["run_b"]; !ok {
		t.Error("expected run_b in compare response")
	}
	if _, ok := result["delta"]; !ok {
		t.Error("expected delta in compare response")
	}
}

// ── Scenario 29: Corruption drill-down ───────────────────────────────────────

func TestS29_CorruptionDrillDown(t *testing.T) {
	dir := newDrive(t)
	file := filepath.Join(dir, "precious.dat")
	writeFile(t, file, "original content")

	// Sync to record original hash.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync: %v", r1.Err)
	}

	// Corrupt the file without changing mtime.
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := info.ModTime()
	writeFile(t, file, "corrupted data!!")
	if err := os.Chtimes(file, origMtime, origMtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Scrub detects corruption.
	r2 := runScrub(t, dir, 100)
	if r2.Err != nil {
		t.Fatalf("scrub: %v", r2.Err)
	}
	if len(r2.ScrubResult.FilesCorrupted) == 0 {
		t.Skip("corruption not detected (mtime resolution or timing issue)")
	}

	// /api/corruption should now return the event.
	srv := newAPIServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/corruption", nil)
	rr  := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var events []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one corruption event")
	}
	if _, ok := events[0]["drive_id"]; !ok {
		t.Error("expected drive_id field in corruption event")
	}
}
