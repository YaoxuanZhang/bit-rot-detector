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
	defer repo.Rollback()

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
	defer repo.Rollback()

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
	defer repo2.Rollback()

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
	defer repo3.Rollback()

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
	defer repo.Rollback()
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
	defer repo.Rollback()
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
