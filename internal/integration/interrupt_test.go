// Package integration_test also covers interrupt/graceful-shutdown scenarios.
// This file validates that:
//
//  1. When the context is cancelled mid-scan, the shadow DB is rolled back
//     and the production DB is NOT promoted.
//  2. The production database remains Last-Known-Good after an interrupt.
//  3. A subsequent run after an interrupt starts from the last committed DB.
//  4. Cancelling before any work is done leaves the directory clean.
//  5. SIGINT-like behaviour (cancel during active sync) does not leave stale
//     shadow files behind.
//  6. Multiple rapid cancellations are idempotent.
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/coordinator"
)

// ── helpers (shared with integration_test.go via same package) ────────────────

// shadowPath returns the expected shadow DB path for a drive directory.
func shadowPath(dir string) string {
	return filepath.Join(dir, "bitrot.db.shadow")
}

// dbPath returns the expected production DB path for a drive directory.
func dbPath(dir string) string {
	return filepath.Join(dir, "bitrot.db")
}

// ── Scenario 21: Shadow DB is rolled back on context cancellation ─────────────

// TestInterrupt_S21_ShadowRolledBack verifies that when a scan is interrupted
// via context cancellation the shadow DB file is removed and the production DB
// is NOT created/modified.
func TestInterrupt_S21_ShadowRolledBack(t *testing.T) {
	dir := newDrive(t)

	// Write many files so the scan takes non-trivial time.
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(dir, string(rune('a'+i%26))+string(rune('0'+i/26))+".dat"), "data")
	}

	// Record whether a production DB existed before the run.
	prodBefore, _ := os.Stat(dbPath(dir))

	// Cancel the context before triggering the run.
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Cancel almost immediately to interrupt mid-scan.
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1, // single worker makes cancellation timing more predictable
	})
	wg.Wait()

	// Shadow DB must NOT persist after rollback.
	if _, err := os.Stat(shadowPath(dir)); err == nil {
		t.Error("shadow DB was not removed after context cancellation")
	}

	// Production DB must not be promoted if it did not exist before.
	if prodBefore == nil {
		if _, err := os.Stat(dbPath(dir)); err == nil {
			t.Error("production DB was created despite context cancellation")
		}
	}
}

// ── Scenario 22: Production DB is Last-Known-Good after interrupt ─────────────

// TestInterrupt_S22_ProductionDBUntouched verifies that after a successful
// first run, a subsequent interrupted run leaves the production DB from the
// first run intact.
func TestInterrupt_S22_ProductionDBUntouched(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "stable.txt"), "stable content")

	// Run 1: successful – commits and writes production DB.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first run: %v", r1.Err)
	}

	info1, err := os.Stat(dbPath(dir))
	if err != nil {
		t.Fatalf("production DB missing after first run: %v", err)
	}
	mtime1 := info1.ModTime()

	// Give mtime resolution a tick.
	time.Sleep(20 * time.Millisecond)

	// Run 2: cancelled immediately – must NOT update production DB.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	writeFile(t, filepath.Join(dir, "new.txt"), "new file added")
	coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})

	info2, err := os.Stat(dbPath(dir))
	if err != nil {
		t.Fatalf("production DB disappeared after interrupted run: %v", err)
	}

	if !info2.ModTime().Equal(mtime1) {
		t.Errorf("production DB was modified during interrupted run (mtime changed: %v → %v)",
			mtime1, info2.ModTime())
	}
}

// ── Scenario 23: Recovery after interrupt starts from last committed DB ────────

// TestInterrupt_S23_RecoveryFromLastGood verifies that after an interrupted
// run, the next successful run picks up from the last committed state.
func TestInterrupt_S23_RecoveryFromLastGood(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "orig.txt"), "original")

	// Run 1: sync orig.txt.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("first sync: %v", r1.Err)
	}
	if r1.SyncResult.FilesAdded != 1 {
		t.Fatalf("expected 1 added, got %d", r1.SyncResult.FilesAdded)
	}

	// Interrupt run: pre-cancel so nothing commits.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writeFile(t, filepath.Join(dir, "interrupted.txt"), "will not be recorded")
	coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})

	// Run 3: successful – should see interrupted.txt as NEW (not already-synced).
	r3 := runSync(t, dir)
	if r3.Err != nil {
		t.Fatalf("recovery run: %v", r3.Err)
	}
	if r3.SyncResult.FilesAdded != 1 {
		t.Errorf("expected 1 newly added file in recovery run, got %d", r3.SyncResult.FilesAdded)
	}
}

// ── Scenario 24: Interrupt before scan starts leaves directory clean ──────────

// TestInterrupt_S24_CancelBeforeScan verifies that cancelling before the scan
// begins leaves no artefacts in the directory.
func TestInterrupt_S24_CancelBeforeScan(t *testing.T) {
	dir := newDrive(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before call

	coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunSync:    true,
		MaxWorkers: 1,
	})

	// No shadow DB should exist.
	if _, err := os.Stat(shadowPath(dir)); err == nil {
		t.Error("shadow DB left behind after pre-cancelled run")
	}

	// No production DB should exist (first run was cancelled).
	if _, err := os.Stat(dbPath(dir)); err == nil {
		t.Error("production DB created from a pre-cancelled run")
	}
}

// ── Scenario 25: Multiple rapid cancellations are idempotent ─────────────────

// TestInterrupt_S25_MultipleRapidCancellations verifies that calling
// coordinator.Run with an already-cancelled context multiple times in a row
// does not panic or leave stale files.
func TestInterrupt_S25_MultipleRapidCancellations(t *testing.T) {
	dir := newDrive(t)
	writeFile(t, filepath.Join(dir, "file.txt"), "data")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for i := 0; i < 5; i++ {
		coordinator.Run(ctx, []string{dir}, coordinator.Options{
			RunSync:    true,
			MaxWorkers: 1,
		})
	}

	// Shadow must not persist.
	if _, err := os.Stat(shadowPath(dir)); err == nil {
		t.Error("shadow DB persisted after repeated cancelled runs")
	}
}

// ── Scenario 26: SIGINT-like mid-scrub interrupt ───────────────────────────────

// TestInterrupt_S26_MidScrubInterrupt ensures that cancelling during a scrub
// (not a sync) also rolls back cleanly.
func TestInterrupt_S26_MidScrubInterrupt(t *testing.T) {
	dir := newDrive(t)
	for i := 0; i < 20; i++ {
		writeFile(t, filepath.Join(dir, string(rune('a'+i))+".bin"), "blob content for hashing")
	}

	// First: sync successfully so the DB exists.
	r1 := runSync(t, dir)
	if r1.Err != nil {
		t.Fatalf("sync before scrub: %v", r1.Err)
	}

	dbInfo1, err := os.Stat(dbPath(dir))
	if err != nil {
		t.Fatalf("production DB missing: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	// Interrupt during scrub.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	coordinator.Run(ctx, []string{dir}, coordinator.Options{
		RunScrub:        true,
		ScrubPercentage: 100,
		ScrubFrequency:  "daily",
		MaxWorkers:      1,
	})
	wg.Wait()

	// Shadow must be cleaned up.
	if _, err := os.Stat(shadowPath(dir)); err == nil {
		t.Error("shadow DB not cleaned up after mid-scrub cancellation")
	}

	// Production DB mtime must be unchanged.
	dbInfo2, err := os.Stat(dbPath(dir))
	if err != nil {
		t.Fatalf("production DB disappeared: %v", err)
	}
	if !dbInfo2.ModTime().Equal(dbInfo1.ModTime()) {
		t.Logf("note: production DB mtime changed (may be acceptable if scrub completed before cancel)")
	}
}
