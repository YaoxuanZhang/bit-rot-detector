package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/storage"
)

// openRepo opens a Repository in a temporary directory.
func openRepo(t *testing.T) (*storage.Repository, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	return repo, dir
}

func TestRepository_UpsertAndGetAllFiles(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()

	ctx := context.Background()

	rec := &domain.FileRecord{
		AbsPath:  "/data/file.txt",
		Hash:     "abc123",
		AddedAt:  time.Now(),
		LastSeen: time.Now(),
		FileSize: 42,
		Mtime:    time.Now(),
	}

	if err := repo.UpsertFile(ctx, rec); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	files, err := repo.GetAllFiles(ctx)
	if err != nil {
		t.Fatalf("GetAllFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	got := files["/data/file.txt"]
	if got.Hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", got.Hash)
	}
}

func TestRepository_DeleteFile(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()

	ctx := context.Background()

	rec := &domain.FileRecord{
		AbsPath:  "/data/file.txt",
		Hash:     "deadbeef",
		FileSize: 10,
		Mtime:    time.Now(),
	}
	_ = repo.UpsertFile(ctx, rec)
	_ = repo.DeleteFile(ctx, "/data/file.txt")

	files, _ := repo.GetAllFiles(ctx)
	if _, ok := files["/data/file.txt"]; ok {
		t.Error("file should have been deleted")
	}
}

func TestRepository_ShadowSwap_Commit(t *testing.T) {
	dir := t.TempDir()

	// First run: open, write a record, commit.
	repo, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open (first run): %v", err)
	}
	ctx := context.Background()
	_ = repo.UpsertFile(ctx, &domain.FileRecord{
		AbsPath:  "/data/kept.txt",
		Hash:     "aabbcc",
		FileSize: 5,
		Mtime:    time.Now(),
	})
	if err := repo.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Production DB must exist after commit.
	prodPath := filepath.Join(dir, "bitrot.db")
	if _, err := os.Stat(prodPath); os.IsNotExist(err) {
		t.Fatal("production DB missing after commit")
	}

	// Shadow file must be gone after commit.
	shadowPath := prodPath + ".shadow"
	if _, err := os.Stat(shadowPath); !os.IsNotExist(err) {
		t.Error("shadow DB should be removed after commit")
	}

	// Second run: data written in first run should be visible.
	repo2, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open (second run): %v", err)
	}
	defer func() { _ = repo2.Rollback() }()

	files, err := repo2.GetAllFiles(ctx)
	if err != nil {
		t.Fatalf("GetAllFiles (second run): %v", err)
	}
	if _, ok := files["/data/kept.txt"]; !ok {
		t.Error("record written in first run should persist after commit")
	}
}

func TestRepository_ShadowSwap_Rollback(t *testing.T) {
	dir := t.TempDir()

	// First run: write and commit.
	repo, _ := storage.Open(dir)
	ctx := context.Background()
	_ = repo.UpsertFile(ctx, &domain.FileRecord{
		AbsPath:  "/data/original.txt",
		Hash:     "orig",
		FileSize: 1,
		Mtime:    time.Now(),
	})
	_ = repo.Commit()

	// Second run: write a new record, then rollback.
	repo2, _ := storage.Open(dir)
	_ = repo2.UpsertFile(ctx, &domain.FileRecord{
		AbsPath:  "/data/aborted.txt",
		Hash:     "abort",
		FileSize: 1,
		Mtime:    time.Now(),
	})
	_ = repo2.Rollback()

	// Third run: the aborted write must NOT be present.
	repo3, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open (third run): %v", err)
	}
	defer func() { _ = repo3.Rollback() }()

	files, _ := repo3.GetAllFiles(ctx)
	if _, ok := files["/data/aborted.txt"]; ok {
		t.Error("aborted write should not be visible after rollback")
	}
	if _, ok := files["/data/original.txt"]; !ok {
		t.Error("original committed record should still be present")
	}
}

func TestRepository_GetFilesForScrub(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_ = repo.UpsertFile(ctx, &domain.FileRecord{
			AbsPath:  filepath.Join("/data", filepath.Join("sub", "file.txt")),
			Hash:     "h",
			FileSize: int64(i),
			Mtime:    time.Now(),
		})
	}

	// 50% of 10 = 5
	files, err := repo.GetFilesForScrub(ctx, 50, nil)
	if err != nil {
		t.Fatalf("GetFilesForScrub: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected at least one file for scrub")
	}
}

func TestRepository_UpdateScrubStatus(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()
	ctx := context.Background()

	rec := &domain.FileRecord{
		AbsPath:  "/data/scrub.txt",
		Hash:     "h",
		FileSize: 1,
		Mtime:    time.Now(),
	}
	_ = repo.UpsertFile(ctx, rec)
	if err := repo.UpdateScrubStatus(ctx, "/data/scrub.txt"); err != nil {
		t.Fatalf("UpdateScrubStatus: %v", err)
	}

	files, _ := repo.GetAllFiles(ctx)
	got := files["/data/scrub.txt"]
	if got == nil {
		t.Fatal("file not found after UpdateScrubStatus")
	}
	if got.ScrubCount != 1 {
		t.Errorf("expected scrub_count=1, got %d", got.ScrubCount)
	}
	if got.LastScrubbed == nil {
		t.Error("expected LastScrubbed to be set")
	}
}

func TestRepository_ComputeChecksum(t *testing.T) {
	repo, dir := openRepo(t)
	defer func() { _ = repo.Rollback() }()

	ctx := context.Background()
	_ = repo.UpsertFile(ctx, &domain.FileRecord{
		AbsPath:  "/data/a.txt",
		Hash:     "abc",
		FileSize: 1,
		Mtime:    time.Now(),
	})

	// Commit so the production DB exists and has real content.
	if err := repo.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Open a new repo pointing at the same dir so the shadow file exists.
	repo2, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open repo2: %v", err)
	}
	defer func() { _ = repo2.Rollback() }()

	sum1, err := repo2.ComputeChecksum()
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}
	if len(sum1) == 0 {
		t.Error("expected non-empty checksum")
	}

	// Checksum must be stable.
	sum2, _ := repo2.ComputeChecksum()
	if sum1 != sum2 {
		t.Error("checksum is not stable")
	}
}

func TestRepository_Close(t *testing.T) {
	repo, _ := openRepo(t)
	// Close should not error on an open connection.
	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Second Close should also not error (conn already nil).
	if err := repo.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestRepository_GetFilesForScrub_WithMinAgeDays(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()
	ctx := context.Background()

	// Add a file that has never been scrubbed.
	_ = repo.UpsertFile(ctx, &domain.FileRecord{
		AbsPath:  "/data/never-scrubbed.txt",
		Hash:     "h",
		FileSize: 1,
		Mtime:    time.Now(),
	})

	// minAgeDays=7: file never scrubbed qualifies.
	days := 7
	files, err := repo.GetFilesForScrub(ctx, 100, &days)
	if err != nil {
		t.Fatalf("GetFilesForScrub: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected file to qualify for scrub (never scrubbed)")
	}
}

func TestRepository_UpsertFile_WithLastScrubbed(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()
	ctx := context.Background()

	now := time.Now()
	rec := &domain.FileRecord{
		AbsPath:      "/data/scrubbed.txt",
		Hash:         "h",
		FileSize:     1,
		Mtime:        now,
		LastScrubbed: &now,
		ScrubCount:   3,
	}
	if err := repo.UpsertFile(ctx, rec); err != nil {
		t.Fatalf("UpsertFile with LastScrubbed: %v", err)
	}

	files, _ := repo.GetAllFiles(ctx)
	got := files["/data/scrubbed.txt"]
	if got == nil {
		t.Fatal("file not found")
	}
	if got.ScrubCount != 3 {
		t.Errorf("expected scrub_count=3, got %d", got.ScrubCount)
	}
	if got.LastScrubbed == nil {
		t.Error("expected LastScrubbed to be preserved")
	}
}

func TestRepository_StaleShadowRemovedOnOpen(t *testing.T) {
	dir := t.TempDir()

	// Create a stale shadow file.
	shadowPath := filepath.Join(dir, "bitrot.db.shadow")
	if err := os.WriteFile(shadowPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Opening a new repository should silently remove the stale shadow.
	repo, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = repo.Rollback() }()

	// Shadow must still exist (newly created), but must NOT contain "stale".
	data, err := os.ReadFile(shadowPath)
	if err != nil {
		t.Fatalf("read shadow: %v", err)
	}
	if string(data) == "stale" {
		t.Error("expected stale shadow to be replaced by fresh copy")
	}
}

func TestRepository_RollbackWhenShadowGone(t *testing.T) {
	repo, dir := openRepo(t)
	// Manually remove the shadow file before calling Rollback.
	shadow := filepath.Join(dir, "bitrot.db.shadow")
	_ = os.Remove(shadow)
	// Rollback must not panic or return an error when shadow is already gone.
	if err := repo.Rollback(); err != nil {
		t.Errorf("Rollback with missing shadow: %v", err)
	}
}

func TestRepository_MultipleRollbacks(t *testing.T) {
	repo, _ := openRepo(t)
	// First rollback removes shadow.
	if err := repo.Rollback(); err != nil {
		t.Fatalf("first Rollback: %v", err)
	}
	// Second rollback is idempotent.
	if err := repo.Rollback(); err != nil {
		t.Errorf("second Rollback: %v", err)
	}
}

func TestRepository_GetFilesForScrub_ZeroResults(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()
	ctx := context.Background()

	// Empty database: GetFilesForScrub should return an empty slice, not an error.
	files, err := repo.GetFilesForScrub(ctx, 100, nil)
	if err != nil {
		t.Fatalf("GetFilesForScrub on empty DB: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files from empty DB, got %d", len(files))
	}
}

func TestRepository_UpsertFile_UpdatesHash(t *testing.T) {
	repo, _ := openRepo(t)
	defer func() { _ = repo.Rollback() }()
	ctx := context.Background()

	rec := &domain.FileRecord{
		AbsPath:  "/data/update.txt",
		Hash:     "oldhash",
		FileSize: 10,
		Mtime:    time.Now(),
	}
	_ = repo.UpsertFile(ctx, rec)

	// Update the hash via a second upsert.
	rec.Hash = "newhash"
	_ = repo.UpsertFile(ctx, rec)

	files, _ := repo.GetAllFiles(ctx)
	if files["/data/update.txt"].Hash != "newhash" {
		t.Errorf("expected updated hash 'newhash', got %q", files["/data/update.txt"].Hash)
	}
}

func TestRepository_OpenInvalidDir(t *testing.T) {
	// Trying to open a repository in a non-existent directory should fail.
	_, err := storage.Open("/nonexistent/path/xyz/abc")
	if err == nil {
		t.Error("expected error opening repo in nonexistent dir")
	}
}

func TestRepository_CommitThenRollback(t *testing.T) {
	dir := t.TempDir()

	repo, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = repo.UpsertFile(ctx, &domain.FileRecord{
		AbsPath:  "/data/x.txt",
		Hash:     "h",
		FileSize: 1,
		Mtime:    time.Now(),
	})
	if err := repo.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Rollback after commit is idempotent (shadow is already gone).
	if err := repo.Rollback(); err != nil {
		t.Errorf("Rollback after Commit: %v", err)
	}
}

func TestRepository_InsertRunHistory_And_GetHistory(t *testing.T) {
	ctx := context.Background()
	repo, dir := openRepo(t)

	rec := &domain.RunRecord{
		DriveID:        dir,
		DriveName:      "test-drive",
		StartedAt:      time.Now().Truncate(time.Second),
		DurationMs:     1234,
		FilesScanned:   10,
		FilesAdded:     3,
		FilesModified:  1,
		FilesRemoved:   0,
		FilesMoved:     0,
		FilesValidated: 5,
		FilesCorrupted: 0,
		SyncErrors:     0,
		ScrubErrors:    0,
	}
	if err := repo.InsertRunHistory(ctx, rec); err != nil {
		t.Fatalf("InsertRunHistory: %v", err)
	}
	if err := repo.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	recs, err := storage.GetRunHistoryForPath(ctx, dir, 10)
	if err != nil {
		t.Fatalf("GetRunHistoryForPath: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("expected at least 1 run record after InsertRunHistory + Commit")
	}
	if recs[0].DriveName != "test-drive" {
		t.Errorf("expected DriveName=test-drive, got %q", recs[0].DriveName)
	}
	if recs[0].FilesScanned != 10 {
		t.Errorf("expected FilesScanned=10, got %d", recs[0].FilesScanned)
	}
}

func TestRepository_InsertRunHistory_MultipleRecords(t *testing.T) {
	ctx := context.Background()
	repo, dir := openRepo(t)

	for i := 0; i < 3; i++ {
		if err := repo.InsertRunHistory(ctx, &domain.RunRecord{
			DriveID:   dir,
			DriveName: "drive",
			StartedAt: time.Now(),
		}); err != nil {
			t.Fatalf("InsertRunHistory[%d]: %v", i, err)
		}
	}
	if err := repo.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	recs, err := storage.GetRunHistoryForPath(ctx, dir, 10)
	if err != nil {
		t.Fatalf("GetRunHistoryForPath: %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("expected 3 records, got %d", len(recs))
	}
}

func TestGetRunHistoryForPath_NoDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// No database created → should return nil without error.
	recs, err := storage.GetRunHistoryForPath(ctx, dir, 10)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if recs != nil {
		t.Errorf("expected nil records, got %v", recs)
	}
}

func TestGetRunsByIDs_NoDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	recs, err := storage.GetRunsByIDs(ctx, dir, []int64{1, 2})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if recs != nil {
		t.Errorf("expected nil records, got %v", recs)
	}
}

func TestGetRunsByIDs_EmptySlice(t *testing.T) {
	ctx := context.Background()
	recs, err := storage.GetRunsByIDs(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if recs != nil {
		t.Errorf("expected nil for empty ids, got %v", recs)
	}
}

func TestGetRunsByIDs_AfterInsert(t *testing.T) {
	ctx := context.Background()
	repo, dir := openRepo(t)

	if err := repo.InsertRunHistory(ctx, &domain.RunRecord{
		DriveID:   dir,
		DriveName: "drive",
		StartedAt: time.Now(),
		DurationMs: 500,
	}); err != nil {
		t.Fatalf("InsertRunHistory: %v", err)
	}
	if err := repo.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	all, err := storage.GetRunHistoryForPath(ctx, dir, 1)
	if err != nil || len(all) == 0 {
		t.Fatalf("GetRunHistoryForPath: err=%v, len=%d", err, len(all))
	}
	id := all[0].ID

	recs, err := storage.GetRunsByIDs(ctx, dir, []int64{id})
	if err != nil {
		t.Fatalf("GetRunsByIDs: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].ID != id {
		t.Errorf("expected ID=%d, got %d", id, recs[0].ID)
	}
}

func TestGetCorruptionHistory_NoDB(t *testing.T) {
	ctx := context.Background()
	recs, err := storage.GetCorruptionHistory(ctx, t.TempDir(), 10)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if recs != nil {
		t.Errorf("expected nil, got %v", recs)
	}
}

func TestGetCorruptionHistory_FiltersOnCorrupted(t *testing.T) {
	ctx := context.Background()
	repo, dir := openRepo(t)

	// Insert one clean run and one corrupted run.
	if err := repo.InsertRunHistory(ctx, &domain.RunRecord{
		DriveID:        dir,
		DriveName:      "drive",
		StartedAt:      time.Now(),
		FilesCorrupted: 0,
	}); err != nil {
		t.Fatalf("InsertRunHistory clean: %v", err)
	}
	if err := repo.InsertRunHistory(ctx, &domain.RunRecord{
		DriveID:        dir,
		DriveName:      "drive",
		StartedAt:      time.Now(),
		FilesCorrupted: 2,
	}); err != nil {
		t.Fatalf("InsertRunHistory corrupted: %v", err)
	}
	if err := repo.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	recs, err := storage.GetCorruptionHistory(ctx, dir, 10)
	if err != nil {
		t.Fatalf("GetCorruptionHistory: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 corrupted record, got %d", len(recs))
	}
	if recs[0].FilesCorrupted != 2 {
		t.Errorf("expected FilesCorrupted=2, got %d", recs[0].FilesCorrupted)
	}
}
