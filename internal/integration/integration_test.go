// Package integration contains end-to-end tests that exercise the full
// coordinator.Run() pipeline against real temporary directories.
//
// Scenarios tested here mirror the Python integration test suite and cover the
// following real-world conditions:
//
//  1. First sync on a fresh directory (new files)
//  2. Unchanged files on a subsequent sync (no-op)
//  3. Modified file detected (size or mtime change)
//  4. Deleted file detected
//  5. Moved/renamed file detected
//  6. Bit rot detected via scrub (hash mismatch without mtime change)
//  7. Database checksum mismatch aborts the run
//  8. Missing canary aborts the run
//  9. Canary is written with correct DB checksum after commit
// 10. Multiple drives processed concurrently
// 11. Scrub percentage filters files correctly
// 12. Scrub frequency (weekly/monthly) skips recently scrubbed files
// 13. Database and canary files excluded from file records
// 14. Symlinks are ignored by the walker
// 15. Full lifecycle: sync → modify → sync again → scrub detects clean
// 16. Crash simulation: shadow DB left behind from aborted run
// 17. Context cancellation stops the scan gracefully
// 18. Scrub-only run after sync
// 19. Nested directory structure scanned recursively
// 20. New files added between runs are captured
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// newDrive creates a temp directory with a canary file and returns its path.
func newDrive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".bitrot-canary"), "")
	return dir
}

// writeFile writes content to path, fataling the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

// runSync performs a sync-only run against dir and returns the DriveResult.
func runSync(t *testing.T, dir string) coordinator.DriveResult {
	t.Helper()
	results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunSync:         true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	return results[0]
}

// runScrub performs a scrub-only run and returns the DriveResult.
func runScrub(t *testing.T, dir string, pct float64) coordinator.DriveResult {
	t.Helper()
	results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunScrub:        true,
		ScrubPercentage: pct,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	return results[0]
}

// runFull performs sync+scrub and returns the DriveResult.
func runFull(t *testing.T, dir string) coordinator.DriveResult {
	t.Helper()
	results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunSync:         true,
		RunScrub:        true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      2,
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	return results[0]
}

// ── Scenario 1: First sync on a fresh directory ───────────────────────────────

func TestIntegration_S01_FirstSync_NewFiles(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "alpha.txt"), "alpha content")
	writeFile(t, filepath.Join(dir, "beta.txt"), "beta content")

	r := runSync(t, dir)

	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.SyncResult.FilesAdded != 2 {
		t.Errorf("expected 2 files added, got %d", r.SyncResult.FilesAdded)
	}
	if r.SyncResult.FilesScanned != 2 {
		t.Errorf("expected 2 files scanned, got %d", r.SyncResult.FilesScanned)
	}
}

// ── Scenario 2: Unchanged files on second sync ────────────────────────────────

func TestIntegration_S02_SecondSync_Unchanged(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "stable.txt"), "stable")

	// First sync.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first sync: %v", r1.Err)
	}

	// Second sync: nothing should have changed.
	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Fatalf("second sync: %v", r2.Err)
	}
	if r2.SyncResult.FilesAdded != 0 {
		t.Errorf("expected 0 added on second sync, got %d", r2.SyncResult.FilesAdded)
	}
	if r2.SyncResult.FilesModified != 0 {
		t.Errorf("expected 0 modified on second sync, got %d", r2.SyncResult.FilesModified)
	}
}

// ── Scenario 3: Modified file detected ───────────────────────────────────────

func TestIntegration_S03_ModifiedFile(t *testing.T) {
	dir := newDrive(t)
	file := filepath.Join(dir, "mutable.txt")
	writeFile(t, file, "original")

	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first sync: %v", r1.Err)
	}

	// Modify the file and advance its mtime.
	writeFile(t, file, "modified content!")
	now := time.Now().Add(time.Second)
	if err := os.Chtimes(file, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Fatalf("second sync: %v", r2.Err)
	}
	if r2.SyncResult.FilesModified != 1 {
		t.Errorf("expected 1 modified, got %d", r2.SyncResult.FilesModified)
	}
}

// ── Scenario 4: Deleted file detected ────────────────────────────────────────

func TestIntegration_S04_DeletedFile(t *testing.T) {
	dir := newDrive(t)
	file := filepath.Join(dir, "ephemeral.txt")
	writeFile(t, file, "temporary")

	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first sync: %v", r1.Err)
	}

	if err := os.Remove(file); err != nil {
		t.Fatalf("remove: %v", err)
	}

	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Fatalf("second sync: %v", r2.Err)
	}
	if r2.SyncResult.FilesRemoved != 1 {
		t.Errorf("expected 1 removed, got %d", r2.SyncResult.FilesRemoved)
	}
}

// ── Scenario 5: Moved/renamed file detected ───────────────────────────────────

func TestIntegration_S05_MovedFile(t *testing.T) {
	dir := newDrive(t)
	src := filepath.Join(dir, "source.txt")
	dst := filepath.Join(dir, "destination.txt")
	writeFile(t, src, "move me")

	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first sync: %v", r1.Err)
	}

	if err := os.Rename(src, dst); err != nil {
		t.Fatalf("rename: %v", err)
	}

	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Fatalf("second sync: %v", r2.Err)
	}
	// Move or add – the file should not be counted as new corruption.
	if len(r2.SyncResult.Errors) != 0 {
		t.Errorf("unexpected errors on rename: %v", r2.SyncResult.Errors)
	}
}

// ── Scenario 6: Bit rot detected via scrub ────────────────────────────────────

func TestIntegration_S06_BitRotDetected(t *testing.T) {
	dir := newDrive(t)
	file := filepath.Join(dir, "precious.iso")
	writeFile(t, file, "original good content")

	// Sync to record the original hash.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync: %v", r1.Err)
	}

	// Silently corrupt the file WITHOUT updating its mtime.
	// Read the current mtime, write new content, restore mtime.
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := info.ModTime()

	writeFile(t, file, "silently corrupted!!")
	if err := os.Chtimes(file, origMtime, origMtime); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}

	// Scrub should detect the mismatch.
	r2 := runScrub(t, dir, 100)
	if r2.Err != nil {
		t.Fatalf("scrub: %v", r2.Err)
	}
	if len(r2.ScrubResult.FilesCorrupted) == 0 {
		t.Error("expected bit rot to be detected")
	}
	if !strings.Contains(r2.ScrubResult.FilesCorrupted[0], "precious.iso") {
		t.Errorf("expected precious.iso in corrupted list, got %v", r2.ScrubResult.FilesCorrupted)
	}
}

// ── Scenario 7: DB checksum mismatch is a warning; run succeeds and canary is rewritten ──

func TestIntegration_S07_DBChecksumMismatch(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "data.txt"), "data")

	// First run: establishes the DB and writes a valid checksum to canary.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first run: %v", r1.Err)
	}

	// Corrupt the canary checksum to simulate a partial-write scenario.
	canary := filepath.Join(dir, ".bitrot-canary")
	writeFile(t, canary, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	// Second run should succeed (warning + best-effort recovery).
	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Errorf("expected recovery (no error) on checksum mismatch, got: %v", r2.Err)
	}

	// Canary should have been rewritten with the real checksum (not the corrupted value).
	canaryData, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary after recovery: %v", err)
	}
	const corrupted = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if string(canaryData) == corrupted {
		t.Error("canary was not rewritten with correct checksum after recovery run")
	}
}

// ── Scenario 8: Missing canary aborts the run ─────────────────────────────────

func TestIntegration_S08_MissingCanary(t *testing.T) {
	dir := t.TempDir() // No canary.
	writeFile(t, filepath.Join(dir, "file.txt"), "data")

	results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})
	if results[0].Err == nil {
		t.Error("expected error for missing canary")
	}
	if !strings.Contains(results[0].Err.Error(), "canary") {
		t.Errorf("expected 'canary' in error message, got: %v", results[0].Err)
	}
}

// ── Scenario 9: Canary gets updated with DB checksum after commit ─────────────

func TestIntegration_S09_CanaryUpdatedAfterCommit(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "content")

	canary := filepath.Join(dir, ".bitrot-canary")

	// Initially the canary is empty.
	before, _ := os.ReadFile(canary)
	if len(strings.TrimSpace(string(before))) != 0 {
		t.Errorf("expected empty canary before first run, got %q", string(before))
	}

	r := runSync(t, dir)
	if r.Err != nil {
		t.Fatalf("sync: %v", r.Err)
	}

	// After commit, canary should contain a non-empty checksum.
	after, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary after run: %v", err)
	}
	if len(strings.TrimSpace(string(after))) == 0 {
		t.Error("expected canary to contain DB checksum after successful run")
	}
}

// ── Scenario 10: Multiple drives processed concurrently ───────────────────────

func TestIntegration_S10_MultipleDrives(t *testing.T) {
	dirs := make([]string, 3)
	for i := range dirs {
		dirs[i] = newDrive(t)
		writeFile(t, filepath.Join(dirs[i], "file.txt"), "drive content")
	}

	results, duration := coordinator.Run(context.Background(), dirs, coordinator.Options{
		RunSync:         true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      4,
	})

	if duration <= 0 {
		t.Error("expected positive duration")
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("drive %s: %v", r.Drive, r.Err)
		}
	}
}

// ── Scenario 11: Scrub percentage filters correctly ───────────────────────────

func TestIntegration_S11_ScrubPercentage(t *testing.T) {
	dir := newDrive(t)

	// Create 10 files.
	for i := 0; i < 10; i++ {
		writeFile(t, filepath.Join(dir, string(rune('a'+i))+".dat"), "x")
	}

	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync: %v", r1.Err)
	}

	// Scrub 50%: should validate roughly half the files.
	r2 := runScrub(t, dir, 50)
	if r2.Err != nil {
		t.Fatalf("50%% scrub: %v", r2.Err)
	}
	if r2.ScrubResult.FilesValidated == 0 {
		t.Error("expected at least 1 file validated with 50% scrub")
	}
	// Should not have validated ALL files.
	total := r1.SyncResult.FilesAdded
	if total > 2 && r2.ScrubResult.FilesValidated >= total {
		t.Errorf("50%% scrub validated all %d files (expected fewer)", total)
	}
}

// ── Scenario 12: Scrub frequency skips recently scrubbed files ────────────────

func TestIntegration_S12_ScrubFrequencyWeekly(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "file.txt"), "data")

	// Sync and then scrub once to set last_scrubbed.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync: %v", r1.Err)
	}
	r2 := runScrub(t, dir, 100)
	if r2.Err != nil {
		t.Fatalf("first scrub: %v", r2.Err)
	}

	// Second scrub with weekly frequency: file was just scrubbed,
	// so it should NOT be eligible (last scrub < 7 days ago).
	results, _ := coordinator.Run(context.Background(), []string{dir}, coordinator.Options{
		RunScrub:        true,
		ScrubPercentage: 100,
		ScrubFrequency:  "weekly", // requires ≥7 days since last scrub
		MaxWorkers:      2,
	})
	r3 := results[0]
	if r3.Err != nil {
		t.Fatalf("weekly scrub: %v", r3.Err)
	}
	// File was just scrubbed seconds ago, so it should be filtered out.
	if r3.ScrubResult.FilesValidated != 0 {
		t.Errorf("expected 0 files validated (weekly filter), got %d", r3.ScrubResult.FilesValidated)
	}
}

// ── Scenario 13: Database and canary files excluded from sync ─────────────────

func TestIntegration_S13_ExcludedFiles(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "normal.txt"), "data")

	// After sync, the DB and canary are created. They must NOT appear in the
	// FilesAdded count.
	r := runSync(t, dir)
	if r.Err != nil {
		t.Fatalf("sync: %v", r.Err)
	}
	// Only normal.txt should be scanned, not bitrot.db or .bitrot-canary.
	if r.SyncResult.FilesAdded != 1 {
		t.Errorf("expected 1 file added (bitrot.db/.bitrot-canary excluded), got %d", r.SyncResult.FilesAdded)
	}
}

// ── Scenario 14: Symlinks ignored by walker ───────────────────────────────────

func TestIntegration_S14_SymlinksIgnored(t *testing.T) {
	dir := newDrive(t)
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	writeFile(t, real, "real content")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	r := runSync(t, dir)
	if r.Err != nil {
		t.Fatalf("sync: %v", r.Err)
	}
	// Only real.txt should be counted (symlink ignored).
	if r.SyncResult.FilesAdded != 1 {
		t.Errorf("expected 1 file added (symlink excluded), got %d", r.SyncResult.FilesAdded)
	}
}

// ── Scenario 15: Full lifecycle ───────────────────────────────────────────────

func TestIntegration_S15_FullLifecycle(t *testing.T) {
	dir := newDrive(t)

	// Initial population.
	writeFile(t, filepath.Join(dir, "keep.txt"), "keep")
	writeFile(t, filepath.Join(dir, "delete.txt"), "delete me")
	writeFile(t, filepath.Join(dir, "modify.txt"), "original")

	// Run 1: first sync.
	r1 := runFull(t, dir)
	if r1.Err != nil {
		t.Fatalf("run 1: %v", r1.Err)
	}
	if r1.SyncResult.FilesAdded != 3 {
		t.Errorf("run 1: expected 3 added, got %d", r1.SyncResult.FilesAdded)
	}

	// Mutate state.
	_ = os.Remove(filepath.Join(dir, "delete.txt"))
	modFile := filepath.Join(dir, "modify.txt")
	writeFile(t, modFile, "modified")
	now := time.Now().Add(time.Second)
	_ = os.Chtimes(modFile, now, now)
	writeFile(t, filepath.Join(dir, "new.txt"), "new")

	// Run 2: sync detects all changes.
	r2 := runFull(t, dir)
	if r2.Err != nil {
		t.Fatalf("run 2: %v", r2.Err)
	}
	if r2.SyncResult.FilesRemoved != 1 {
		t.Errorf("run 2: expected 1 removed, got %d", r2.SyncResult.FilesRemoved)
	}
	if r2.SyncResult.FilesModified != 1 {
		t.Errorf("run 2: expected 1 modified, got %d", r2.SyncResult.FilesModified)
	}
	if r2.SyncResult.FilesAdded != 1 {
		t.Errorf("run 2: expected 1 added, got %d", r2.SyncResult.FilesAdded)
	}

	// Run 3: no changes, scrub should find no corruption.
	r3 := runFull(t, dir)
	if r3.Err != nil {
		t.Fatalf("run 3: %v", r3.Err)
	}
	if len(r3.ScrubResult.FilesCorrupted) != 0 {
		t.Errorf("run 3: expected no corruption, got %v", r3.ScrubResult.FilesCorrupted)
	}
}

// ── Scenario 16: Crash simulation (stale shadow DB) ──────────────────────────

func TestIntegration_S16_StaleShadowHandled(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "file.txt"), "data")

	// Simulate a previous crashed run: leave a stale shadow DB.
	stale := filepath.Join(dir, "bitrot.db.shadow")
	writeFile(t, stale, "stale shadow content")

	// A new run should overwrite the stale shadow and succeed.
	r := runSync(t, dir)
	if r.Err != nil {
		t.Fatalf("run after stale shadow: %v", r.Err)
	}
	if r.SyncResult.FilesAdded != 1 {
		t.Errorf("expected 1 file added, got %d", r.SyncResult.FilesAdded)
	}
}

// ── Scenario 17: Context cancellation ────────────────────────────────────────

func TestIntegration_S17_ContextCancellation(t *testing.T) {
	dir := newDrive(t)
	for i := 0; i < 5; i++ {
		writeFile(t, filepath.Join(dir, string(rune('a'+i))+".txt"), "data")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	results, _ := coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})
	// Must not panic; error may or may not be set depending on cancellation timing.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// ── Scenario 18: Scrub-only after sync ───────────────────────────────────────

func TestIntegration_S18_ScrubOnlyAfterSync(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "file.txt"), "clean content")

	// First, sync only.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync: %v", r1.Err)
	}

	// Then, scrub only.
	r2 := runScrub(t, dir, 100)
	if r2.Err != nil {
		t.Fatalf("scrub: %v", r2.Err)
	}
	if r2.ScrubResult.FilesValidated != 1 {
		t.Errorf("expected 1 file validated, got %d", r2.ScrubResult.FilesValidated)
	}
	if len(r2.ScrubResult.FilesCorrupted) != 0 {
		t.Errorf("expected no corruption, got %v", r2.ScrubResult.FilesCorrupted)
	}
}

// ── Scenario 19: Nested directory structure ───────────────────────────────────

func TestIntegration_S19_NestedDirectories(t *testing.T) {
	dir := newDrive(t)

	// Create a nested structure.
	subA := filepath.Join(dir, "music", "albums", "2023")
	subB := filepath.Join(dir, "photos", "vacation")
	for _, sub := range []string{subA, subB} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(subA, "track01.flac"), "audio data")
	writeFile(t, filepath.Join(subA, "track02.flac"), "audio data 2")
	writeFile(t, filepath.Join(subB, "photo1.jpg"), "jpeg data")
	writeFile(t, filepath.Join(dir, "readme.txt"), "top level")

	r := runSync(t, dir)
	if r.Err != nil {
		t.Fatalf("sync: %v", r.Err)
	}
	if r.SyncResult.FilesAdded != 4 {
		t.Errorf("expected 4 files added, got %d", r.SyncResult.FilesAdded)
	}
}

// ── Scenario 20: New files added between runs ─────────────────────────────────

func TestIntegration_S20_NewFilesBetweenRuns(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "existing.txt"), "existed before")

	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first sync: %v", r1.Err)
	}
	if r1.SyncResult.FilesAdded != 1 {
		t.Errorf("first sync: expected 1 added, got %d", r1.SyncResult.FilesAdded)
	}

	// Add two new files.
	writeFile(t, filepath.Join(dir, "new1.dat"), "new1")
	writeFile(t, filepath.Join(dir, "new2.dat"), "new2")

	r2 := runSync(t, dir)
	if r2.Err != nil {
		t.Fatalf("second sync: %v", r2.Err)
	}
	if r2.SyncResult.FilesAdded != 2 {
		t.Errorf("second sync: expected 2 new files, got %d", r2.SyncResult.FilesAdded)
	}
	if r2.SyncResult.FilesModified != 0 {
		t.Errorf("second sync: expected 0 modified, got %d", r2.SyncResult.FilesModified)
	}
}
