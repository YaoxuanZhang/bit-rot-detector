package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/watcher"
)

func TestNew_InvalidDir(t *testing.T) {
	_, err := watcher.New([]string{"/nonexistent/path/xyz"}, time.Second, func(context.Context, []string) {})
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestNew_EmptyRoots(t *testing.T) {
	w, err := watcher.New([]string{}, time.Second, func(context.Context, []string) {})
	if err != nil {
		t.Fatalf("unexpected error for empty roots: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	w.Close()
}

func TestNew_DebounceTooSmall(t *testing.T) {
	dir := t.TempDir()
	// Debounce < 100ms should be silently clamped.
	w, err := watcher.New([]string{dir}, 1*time.Millisecond, func(context.Context, []string) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w.Close()
}

func TestWatcher_FileChangeTriggersCallback(t *testing.T) {
	dir := t.TempDir()

	var callCount atomic.Int32
	gotCh := make(chan []string, 1)

	w, err := watcher.New([]string{dir}, 150*time.Millisecond, func(_ context.Context, changed []string) {
		callCount.Add(1)
		select {
		case gotCh <- changed:
		default:
		}
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go w.Run(ctx)

	// Give fsnotify time to register the watch.
	time.Sleep(50 * time.Millisecond)

	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + callback.
	select {
	case got := <-gotCh:
		if len(got) == 0 {
			t.Error("changed slice was empty")
		}
	case <-time.After(2 * time.Second):
		t.Error("callback was not triggered after file creation")
	}
}

func TestWatcher_InternalFilesIgnored(t *testing.T) {
	dir := t.TempDir()

	var callCount atomic.Int32

	w, err := watcher.New([]string{dir}, 150*time.Millisecond, func(_ context.Context, _ []string) {
		callCount.Add(1)
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go w.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	// Writing internal files should NOT trigger the callback.
	for _, name := range []string{"bitrot.db", "bitrot.db.shadow", ".bitrot-canary"} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}

	// Wait to confirm callback is NOT called.
	time.Sleep(500 * time.Millisecond)
	if callCount.Load() != 0 {
		t.Errorf("callback triggered %d time(s) for internal-file changes (expected 0)", callCount.Load())
	}
}

func TestWatcher_ContextCancellationStopsRun(t *testing.T) {
	dir := t.TempDir()

	w, err := watcher.New([]string{dir}, time.Second, func(context.Context, []string) {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// OK: Run returned after context expiry.
	case <-time.After(time.Second):
		t.Error("Run did not return within 1 s after context cancellation")
	}
}

func TestWatcher_Close(t *testing.T) {
	dir := t.TempDir()
	w, err := watcher.New([]string{dir}, time.Second, func(context.Context, []string) {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Close must not panic or error.
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestWatcher_SubdirCreatedIsWatched(t *testing.T) {
	dir := t.TempDir()

	var callCount atomic.Int32
	w, err := watcher.New([]string{dir}, 150*time.Millisecond, func(_ context.Context, _ []string) {
		callCount.Add(1)
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go w.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	// Create a subdirectory and then a file inside it.
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let watcher pick up the new dir

	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if callCount.Load() == 0 {
		t.Error("callback not triggered for file in newly created subdirectory")
	}
}
