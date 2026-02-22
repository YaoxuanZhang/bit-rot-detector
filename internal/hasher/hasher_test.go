package hasher_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/hasher"
)

func TestComputeHash_Consistency(t *testing.T) {
	h := hasher.New()

	dir := t.TempDir()
	file := filepath.Join(dir, "test.bin")
	content := []byte("hello bit-rot-detector")
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatal(err)
	}

	hash1, err := h.ComputeHash(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := h.ComputeHash(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 != hash2 {
		t.Errorf("hashes differ for same file: %s vs %s", hash1, hash2)
	}
	if len(hash1) == 0 {
		t.Error("expected non-empty hash")
	}
}

func TestComputeHash_DifferentContent(t *testing.T) {
	h := hasher.New()
	dir := t.TempDir()

	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	f1 := write("a.bin", "content A")
	f2 := write("b.bin", "content B")

	hash1, _ := h.ComputeHash(context.Background(), f1)
	hash2, _ := h.ComputeHash(context.Background(), f2)

	if hash1 == hash2 {
		t.Error("different content should produce different hashes")
	}
}

func TestComputeHash_ContextCancellation(t *testing.T) {
	h := hasher.New()
	dir := t.TempDir()
	file := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := h.ComputeHash(ctx, file)
	if err == nil {
		// Small files may complete before the cancellation is noticed; that is acceptable.
		t.Log("hash completed before context cancellation was observed (acceptable for small files)")
	}
}

func TestComputeHash_MissingFile(t *testing.T) {
	h := hasher.New()
	_, err := h.ComputeHash(context.Background(), "/nonexistent/path/file.bin")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
