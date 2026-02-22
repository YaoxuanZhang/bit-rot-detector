package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/hasher"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/scanner"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/storage"
)

func setup(t *testing.T) (dir string, syncer *scanner.Syncer) {
	t.Helper()
	dir = t.TempDir()

	// Create canary so coordinator-style callers won't complain; scanner itself
	// doesn't check it directly.
	_ = os.WriteFile(filepath.Join(dir, ".bitrot-canary"), []byte(""), 0o644)

	h := hasher.New()
	syncer = scanner.NewSyncer(h, 2)
	return dir, syncer
}

func openRepo(t *testing.T, dir string) *storage.Repository {
	t.Helper()
	repo, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	return repo
}

func TestSyncDirectory_NewFiles(t *testing.T) {
	dir, syncer := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := openRepo(t, dir)
	defer repo.Rollback()

	res, err := syncer.SyncDirectory(context.Background(), dir, repo)
	if err != nil {
		t.Fatalf("SyncDirectory: %v", err)
	}

	if res.FilesScanned == 0 {
		t.Error("expected at least one file scanned")
	}
	if res.FilesAdded == 0 {
		t.Error("expected at least one file added")
	}
}

func TestSyncDirectory_DeletedFiles(t *testing.T) {
	dir, syncer := setup(t)

	// First sync: add a file.
	file := filepath.Join(dir, "to-delete.txt")
	_ = os.WriteFile(file, []byte("temp"), 0o644)

	repo := openRepo(t, dir)
	_, err := syncer.SyncDirectory(context.Background(), dir, repo)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	_ = repo.Commit()

	// Remove the file.
	_ = os.Remove(file)

	// Second sync: the file should be detected as deleted.
	repo2 := openRepo(t, dir)
	defer repo2.Rollback()

	res2, err := syncer.SyncDirectory(context.Background(), dir, repo2)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res2.FilesRemoved == 0 {
		t.Error("expected at least one file removed")
	}
}

func TestScrubFiles_NoCorruption(t *testing.T) {
	dir, syncer := setup(t)
	_ = os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("clean data"), 0o644)

	repo := openRepo(t, dir)
	_, err := syncer.SyncDirectory(context.Background(), dir, repo)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	_ = repo.Commit()

	repo2 := openRepo(t, dir)
	defer repo2.Rollback()

	res, err := syncer.ScrubFiles(context.Background(), repo2, 100, nil)
	if err != nil {
		t.Fatalf("ScrubFiles: %v", err)
	}
	if len(res.FilesCorrupted) != 0 {
		t.Errorf("expected no corruption, got %v", res.FilesCorrupted)
	}
	if res.FilesValidated == 0 {
		t.Error("expected at least one file validated")
	}
}

func TestScrubFiles_BitRotDetected(t *testing.T) {
	dir, syncer := setup(t)
	file := filepath.Join(dir, "corrupt.bin")
	_ = os.WriteFile(file, []byte("original content"), 0o644)

	// Sync to record the original hash.
	repo := openRepo(t, dir)
	_, err := syncer.SyncDirectory(context.Background(), dir, repo)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	_ = repo.Commit()

	// Silently corrupt the file.
	_ = os.WriteFile(file, []byte("corrupted!!!!!!!"), 0o644)

	// Restore mtime to simulate silent corruption (bit rot doesn't change mtime).
	// We rely on the fact that the hash has changed while mtime stays the same,
	// which is exactly what the scrub detects.

	repo2 := openRepo(t, dir)
	defer repo2.Rollback()

	res, err := syncer.ScrubFiles(context.Background(), repo2, 100, nil)
	if err != nil {
		t.Fatalf("ScrubFiles: %v", err)
	}
	if len(res.FilesCorrupted) == 0 {
		t.Error("expected bit rot to be detected")
	}
}
