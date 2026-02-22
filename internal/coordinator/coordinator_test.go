package coordinator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
)

// setupDrive creates a temporary directory with a .bitrot-canary and test files.
func setupDrive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".bitrot-canary"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file2.bin"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ── Run() ────────────────────────────────────────────────────────────────────

func TestRun_EmptyTargetPaths(t *testing.T) {
	results, duration := coordinator.Run(context.Background(), nil, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})
	if len(results) != 0 {
		t.Errorf("expected no results for empty paths, got %d", len(results))
	}
	if duration != 0 {
		t.Errorf("expected zero duration for empty paths, got %v", duration)
	}
}

func TestRun_SyncOnly(t *testing.T) {
	dir := setupDrive(t)

	results, duration := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunSync:         true,
		RunScrub:        false,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})

	if duration <= 0 {
		t.Error("expected positive duration")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.SyncResult == nil {
		t.Fatal("expected non-nil SyncResult")
	}
	if r.SyncResult.FilesAdded < 2 {
		t.Errorf("expected at least 2 files added, got %d", r.SyncResult.FilesAdded)
	}
	if r.ScrubResult != nil {
		t.Error("expected nil ScrubResult when RunScrub=false")
	}
}

func TestRun_SyncThenScrub(t *testing.T) {
	dir := setupDrive(t)

	opts := coordinator.Options{
		RunSync:         true,
		RunScrub:        false,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	}

	// First pass: sync
	results, _ := coordinator.Run(context.Background(), []string{dir}, opts)
	if results[0].Err != nil {
		t.Fatalf("sync pass: %v", results[0].Err)
	}

	// Second pass: scrub only
	opts.RunSync = false
	opts.RunScrub = true
	results, _ = coordinator.Run(context.Background(), []string{dir}, opts)
	if results[0].Err != nil {
		t.Fatalf("scrub pass: %v", results[0].Err)
	}
	if results[0].ScrubResult == nil {
		t.Fatal("expected ScrubResult")
	}
	if len(results[0].ScrubResult.FilesCorrupted) != 0 {
		t.Errorf("expected no corruption, got %v", results[0].ScrubResult.FilesCorrupted)
	}
}

func TestRun_MissingCanary(t *testing.T) {
	dir := t.TempDir() // No canary file.
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644)

	results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Error("expected error when canary is missing")
	}
}

func TestRun_MultipleDrives(t *testing.T) {
	dir1 := setupDrive(t)
	dir2 := setupDrive(t)

	results, _ := coordinator.Run(context.Background(), []string{dir1, dir2}, coordinator.Options{
		RunSync:         true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("drive %s: unexpected error: %v", r.Drive, r.Err)
		}
	}
}

func TestRun_ScrubFrequencies(t *testing.T) {
	for _, freq := range []string{"daily", "weekly", "monthly"} {
		t.Run(freq, func(t *testing.T) {
			dir := setupDrive(t)

			// Sync first.
			coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
				RunSync: true, ScrubPercentage: 100, ScrubFrequency: freq, MaxWorkers: 2,
			})

			// Then scrub.
			results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
				RunScrub: true, ScrubPercentage: 100, ScrubFrequency: freq, MaxWorkers: 2,
			})
			if results[0].Err != nil {
				t.Errorf("freq=%s: %v", freq, results[0].Err)
			}
		})
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	dir := setupDrive(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before starting.

	results, _ := coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})
	// With a cancelled context the scan may fail or succeed (race); we just
	// ensure it does not panic and returns a result.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// ── Helper converters ─────────────────────────────────────────────────────────

func TestToMailerSyncEntries(t *testing.T) {
	results := []coordinator.DriveResult{
		{Drive: "a", SyncResult: &domain.SyncResult{FilesAdded: 1}},
		{Drive: "b", SyncResult: nil},
	}
	entries := coordinator.ToMailerSyncEntries(results)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (nil filtered), got %d", len(entries))
	}
	if entries[0].Drive != "a" {
		t.Errorf("expected drive a, got %q", entries[0].Drive)
	}
}

func TestToMailerScrubEntries(t *testing.T) {
	results := []coordinator.DriveResult{
		{Drive: "a", ScrubResult: nil},
		{Drive: "b", ScrubResult: &domain.ScrubResult{FilesValidated: 10}},
	}
	entries := coordinator.ToMailerScrubEntries(results)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Drive != "b" {
		t.Errorf("expected drive b, got %q", entries[0].Drive)
	}
}

// Verify that a ScrubEntry round-trips through the coordinator helper.
func TestToMailerScrubEntries_Type(t *testing.T) {
	results := []coordinator.DriveResult{
		{Drive: "x", ScrubResult: &domain.ScrubResult{FilesCorrupted: []string{"/bad"}}},
	}
	entries := coordinator.ToMailerScrubEntries(results)
	var _ []mailer.ScrubEntry = entries // compile-time type assertion
	if len(entries[0].Result.FilesCorrupted) != 1 {
		t.Error("expected 1 corrupted file")
	}
}

func TestToHealthSlice(t *testing.T) {
	h := &domain.DriveHealth{DriveName: "disk0"}
	results := []coordinator.DriveResult{
		{Drive: "disk0", Health: h},
		{Drive: "disk1", Health: nil},
	}
	out := coordinator.ToHealthSlice(results)
	if len(out) != 1 {
		t.Fatalf("expected 1 health entry (nil filtered), got %d", len(out))
	}
}

func TestCollectErrors(t *testing.T) {
	results := []coordinator.DriveResult{
		{Drive: "ok", Err: nil},
		{Drive: "bad", Err: context.Canceled},
	}
	errs := coordinator.CollectErrors(results)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !containsString(errs[0], "bad") {
		t.Errorf("expected drive name in error string, got %q", errs[0])
	}
}

func TestCollectErrors_NoErrors(t *testing.T) {
	results := []coordinator.DriveResult{
		{Drive: "a"}, {Drive: "b"},
	}
	if errs := coordinator.CollectErrors(results); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStringHelper(s, sub))
}

func containsStringHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
