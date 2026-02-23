// Package watcher provides real-time filesystem change detection using
// fsnotify.  When a file is created, modified, or removed under a watched
// directory tree, the watcher triggers a resync callback after a configurable
// debounce window so that rapid bursts of changes produce a single callback.
//
// # Usage
//
//	w, err := watcher.New(paths, 3*time.Second, onChangeFn)
//	if err != nil { ... }
//	defer w.Close()
//	w.Run(ctx) // blocks until ctx is cancelled
package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeFn is called after the debounce window expires following one or more
// filesystem events under the watched paths.  changed contains the set of
// top-level watched directories that received at least one event.
type ChangeFn func(ctx context.Context, changed []string)

// Watcher watches one or more directory trees for filesystem changes and
// calls a ChangeFn after a debounce window.
type Watcher struct {
	fw       *fsnotify.Watcher
	roots    []string
	debounce time.Duration
	fn       ChangeFn
}

// New creates a Watcher for the given root directories.  Sub-directories that
// exist at creation time are recursively added.  debounce controls how long
// the watcher waits after the last event before invoking fn.  A minimum of
// 100 ms is enforced.
func New(roots []string, debounce time.Duration, fn ChangeFn) (*Watcher, error) {
	if debounce < 100*time.Millisecond {
		debounce = 100 * time.Millisecond
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{fw: fw, roots: roots, debounce: debounce, fn: fn}

	for _, root := range roots {
		if err := w.addTree(root); err != nil {
			_ = fw.Close()
			return nil, err
		}
	}
	return w, nil
}

// addTree recursively adds root and all sub-directories to the fsnotify watcher.
func (w *Watcher) addTree(root string) error {
	// Validate the root exists before walking.
	if _, err := os.Lstat(root); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("watcher: cannot watch path", "path", path, "err", err)
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if err := w.fw.Add(path); err != nil {
				slog.Warn("watcher: cannot add dir", "path", path, "err", err)
			}
		}
		return nil
	})
}

// Run starts the event loop.  It blocks until ctx is cancelled.
// Newly created sub-directories are automatically added to the watch set.
//
// All state (pending map, timer) is owned exclusively by this goroutine, so
// no mutex is needed and there is no data race between the event loop and the
// debounce callback.
func (w *Watcher) Run(ctx context.Context) {
	// pending tracks which root directories have received events since the
	// last callback invocation.
	pending := make(map[string]struct{})

	// timer / timerC implement a reset-able debounce entirely within this
	// goroutine.  timerC is nil when no timer is armed, which causes select
	// to skip that case.
	var timer *time.Timer
	var timerC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return

		case <-timerC:
			// Debounce window elapsed — fire callback in this goroutine (no race).
			if len(pending) > 0 {
				changed := make([]string, 0, len(pending))
				for root := range pending {
					changed = append(changed, root)
				}
				clear(pending) // Go 1.21+: reset map without reallocating
				slog.Info("watcher: changes detected, triggering resync", "drives", changed)
				w.fn(ctx, changed)
			}
			timerC = nil

		case event, ok := <-w.fw.Events:
			if !ok {
				return
			}
			// Skip the DB and shadow-DB files to avoid feedback loops.
			base := filepath.Base(event.Name)
			if base == "bitrot.db" || base == "bitrot.db.shadow" || base == ".bitrot-canary" {
				continue
			}

			// If a new directory was created, watch it recursively.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := w.addTree(event.Name); err != nil {
						slog.Warn("watcher: failed to watch new dir", "path", event.Name, "err", err)
					}
				}
			}

			root := w.rootFor(event.Name)
			pending[root] = struct{}{}

			// Reset the debounce timer.  Stop the old one and drain its channel
			// so the previous tick (if any) does not fire unexpectedly.
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			timer = time.NewTimer(w.debounce)
			timerC = timer.C

		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			slog.Warn("watcher: fsnotify error", "err", err)
		}
	}
}

// Close releases the underlying fsnotify watcher.
func (w *Watcher) Close() error {
	return w.fw.Close()
}

// rootFor returns the watched root directory that contains path,
// or path itself if no match is found.
func (w *Watcher) rootFor(path string) string {
	for _, root := range w.roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && len(rel) > 0 && rel[0] != '.' {
			return root
		}
		if path == root {
			return root
		}
	}
	return path
}
