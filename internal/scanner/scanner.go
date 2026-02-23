// Package scanner implements directory walking, file-integrity sync, and
// periodic scrubbing using a producer-consumer worker pool.
//
// # Sync
//
// A sync operation walks an entire directory tree and, for each regular file,
// determines whether the file is new, modified, moved, or deleted relative to
// the contents of the integrity database.  Only files whose size or mtime
// differ from the stored values are re-hashed; unchanged files are recorded as
// "seen" without any I/O.
//
// # Scrub
//
// A scrub operation re-hashes a configurable percentage of files that were
// previously recorded by a sync.  Files are selected in least-recently-scrubbed
// order so that all files are eventually verified.  A hash mismatch indicates
// silent data corruption (bit rot).
//
// # Worker pool
//
// The directory walker runs as a producer goroutine that sends [domain.WorkItem]
// values into a buffered channel.  A pool of N consumer goroutines reads from
// the channel, hashes each file using [domain.Hasher], and sends
// [domain.WorkResult] values back through a second channel.  The collector
// goroutine receives results and writes them to the [domain.Repository].
//
// The worker count N is supplied by the caller and is typically determined by
// [monitor.Detect] to avoid head thrashing on spinning-platter drives.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
)

const workBufferSize = 512

// Syncer orchestrates a sync operation: it walks the directory tree
// (producer), hashes new/modified files via a worker pool (consumer), and
// writes results to the repository.
type Syncer struct {
	hasher     domain.Hasher
	numWorkers int
}

// NewSyncer creates a Syncer.
func NewSyncer(h domain.Hasher, numWorkers int) *Syncer {
	if numWorkers < 1 {
		numWorkers = 1
	}
	return &Syncer{hasher: h, numWorkers: numWorkers}
}

// SyncDirectory walks rootPath, compares each file against the repository,
// and records new, modified, moved, and deleted files.
func (s *Syncer) SyncDirectory(ctx context.Context, rootPath string, repo domain.Repository) (*domain.SyncResult, error) {
	// Respect cancellation before any work begins.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slog.Info("sync started", "path", rootPath, "workers", s.numWorkers)

	// Load existing records from the shadow DB.
	dbFiles, err := repo.GetAllFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("load db files: %w", err)
	}
	slog.Info("loaded existing records", "count", len(dbFiles))

	// work channel: producer -> workers
	work := make(chan domain.WorkItem, workBufferSize)
	// results channel: workers -> collector
	results := make(chan domain.WorkResult, workBufferSize)

	// Walk-phase bookkeeping (populated by walker, read by collector).
	// Maps abs_path -> (size, mtime) for every file actually seen on disk.
	var (
		seenMu  sync.Mutex
		seenFiles = make(map[string]struct{ size int64; mtime time.Time })
	)

	// Start worker pool.
	var wg sync.WaitGroup
	for i := 0; i < s.numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				select {
				case <-ctx.Done():
					return
				default:
				}
				// Short-circuit: reuse the stored hash if the file is unchanged.
				if item.KnownHash != "" {
					results <- domain.WorkResult{
						Path:  item.Path,
						Hash:  item.KnownHash,
						Size:  item.Size,
						Mtime: item.Mtime,
					}
					continue
				}
				hash, herr := s.hasher.ComputeHash(ctx, item.Path)
				results <- domain.WorkResult{
					Path:  item.Path,
					Hash:  hash,
					Size:  item.Size,
					Mtime: item.Mtime,
					Err:   herr,
				}
			}
		}()
	}

	// Goroutine to close results once all workers are done.
	go func() {
		wg.Wait()
		close(results)
	}()

	result := &domain.SyncResult{}
	// resultMu protects result.Errors and all result counter fields that are
	// written from both the walker goroutine and the collector goroutine.
	var resultMu sync.Mutex

	// Producer: walk directory tree.
	walkErr := make(chan error, 1)
	go func() {
		defer close(work)
		err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				slog.Warn("walk error", "path", path, "err", err)
				resultMu.Lock()
				result.Errors = append(result.Errors, err.Error())
				resultMu.Unlock()
				return nil // continue walking
			}
			if d.IsDir() {
				return skipSystemDir(d.Name())
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil // skip symlinks
			}

			// Skip the database files (including WAL and SHM files) and canary.
			base := filepath.Base(path)
			if base == "bitrot.db" || base == "bitrot.db.shadow" ||
				base == "bitrot.db-wal" || base == "bitrot.db-shm" ||
				base == "bitrot.db.shadow-wal" || base == "bitrot.db.shadow-shm" ||
				base == ".bitrot-canary" {
				return nil
			}

			info, serr := d.Info()
			if serr != nil {
				slog.Warn("stat error", "path", path, "err", serr)
				resultMu.Lock()
				result.Errors = append(result.Errors, serr.Error())
				resultMu.Unlock()
				return nil
			}

			absPath, _ := filepath.Abs(path)
			size := info.Size()
			mtime := info.ModTime()

			seenMu.Lock()
			seenFiles[absPath] = struct {
				size  int64
				mtime time.Time
			}{size, mtime}
			seenMu.Unlock()

			resultMu.Lock()
			result.FilesScanned++
			scanned := result.FilesScanned
			resultMu.Unlock()
			if scanned%1000 == 0 {
				slog.Info("walking...", "scanned", scanned)
			}

			// Short-circuit: if size and mtime match the stored record, skip
			// re-hashing entirely and reuse the stored hash.
			item := domain.WorkItem{Path: absPath, Size: size, Mtime: mtime}
			if existing, ok := dbFiles[absPath]; ok &&
				existing.FileSize == size &&
				existing.Mtime.Unix() == mtime.Unix() {
				item.KnownHash = existing.Hash
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case work <- item:
			}
			return nil
		})
		walkErr <- err
	}()

	// Collector: process worker results and write to the repository.
	for res := range results {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if res.Err != nil {
			slog.Warn("hash error", "path", res.Path, "err", res.Err)
			resultMu.Lock()
			result.Errors = append(result.Errors, fmt.Sprintf("hash %s: %v", res.Path, res.Err))
			resultMu.Unlock()
			continue
		}

		existing, inDB := dbFiles[res.Path]
		rec := &domain.FileRecord{
			AbsPath:  res.Path,
			Hash:     res.Hash,
			FileSize: res.Size,
			Mtime:    res.Mtime,
		}

		if inDB {
			// Check whether file was actually modified.
			sizeChanged := existing.FileSize != res.Size
			mtimeChanged := existing.Mtime.Unix() != res.Mtime.Unix()

			if sizeChanged || mtimeChanged {
				resultMu.Lock()
				result.FilesModified++
				resultMu.Unlock()
				slog.Info("file modified", "path", res.Path)
			}
			rec.AddedAt = existing.AddedAt
			rec.LastScrubbed = existing.LastScrubbed
			rec.ScrubCount = existing.ScrubCount
		} else {
			// Check for a moved file (same size+mtime at a different path).
			// Protect seenFiles with the mutex since the walker goroutine writes to it concurrently.
			seenMu.Lock()
			movedFrom := findMovedFile(res.Path, res.Size, res.Mtime, dbFiles, seenFiles)
			seenMu.Unlock()
			if movedFrom != "" {
				old := dbFiles[movedFrom]
				rec.AddedAt = old.AddedAt
				rec.LastScrubbed = old.LastScrubbed
				rec.ScrubCount = old.ScrubCount
				rec.Hash = old.Hash // reuse stored hash — file content unchanged
				if err := repo.DeleteFile(ctx, movedFrom); err != nil {
					slog.Warn("delete moved-from record", "path", movedFrom, "err", err)
				}
				resultMu.Lock()
				result.FilesMoved++
				resultMu.Unlock()
				slog.Info("file moved", "from", movedFrom, "to", res.Path)
			} else {
				rec.AddedAt = time.Now()
				resultMu.Lock()
				result.FilesAdded++
				resultMu.Unlock()
				slog.Debug("file added", "path", res.Path)
			}
		}

		if err := repo.UpsertFile(ctx, rec); err != nil {
			slog.Warn("upsert file", "path", res.Path, "err", err)
			resultMu.Lock()
			result.Errors = append(result.Errors, fmt.Sprintf("upsert %s: %v", res.Path, err))
			resultMu.Unlock()
		}
	}

	// Wait for walker.
	if werr := <-walkErr; werr != nil {
		if ctx.Err() != nil {
			// Walk was stopped by context cancellation; propagate as cancellation.
			return result, ctx.Err()
		}
		return result, fmt.Errorf("walk: %w", werr)
	}

	// Phase 3: detect deleted files (in DB but not seen on disk).
	seenMu.Lock()
	defer seenMu.Unlock()
	for absPath := range dbFiles {
		if _, seen := seenFiles[absPath]; !seen {
			slog.Info("file deleted", "path", absPath)
			if err := repo.DeleteFile(ctx, absPath); err != nil {
				slog.Warn("delete missing record", "path", absPath, "err", err)
			}
			result.FilesRemoved++
		}
	}

	slog.Info("sync complete",
		"scanned", result.FilesScanned,
		"added", result.FilesAdded,
		"modified", result.FilesModified,
		"moved", result.FilesMoved,
		"removed", result.FilesRemoved,
		"errors", len(result.Errors),
	)
	return result, nil
}

// ScrubFiles re-verifies a percentage of files to detect bit rot.
func (s *Syncer) ScrubFiles(ctx context.Context, repo domain.Repository, percentage float64, minAgeDays *int) (*domain.ScrubResult, error) {
	// Respect cancellation before any work begins.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slog.Info("scrub started", "percentage", percentage)

	files, err := repo.GetFilesForScrub(ctx, percentage, minAgeDays)
	if err != nil {
		return nil, fmt.Errorf("get files for scrub: %w", err)
	}
	slog.Info("files selected for scrub", "count", len(files))

	res := &domain.ScrubResult{}

	for _, rec := range files {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		if _, serr := os.Stat(rec.AbsPath); os.IsNotExist(serr) {
			slog.Warn("file not found during scrub", "path", rec.AbsPath)
			res.Errors = append(res.Errors, fmt.Sprintf("not found: %s", rec.AbsPath))
			continue
		}

		hash, herr := s.hasher.ComputeHash(ctx, rec.AbsPath)
		if herr != nil {
			slog.Warn("hash error during scrub", "path", rec.AbsPath, "err", herr)
			res.Errors = append(res.Errors, fmt.Sprintf("hash %s: %v", rec.AbsPath, herr))
			continue
		}

		if hash != rec.Hash {
			slog.Error("BIT ROT DETECTED", "path", rec.AbsPath,
				"expected", rec.Hash, "got", hash)
			res.FilesCorrupted = append(res.FilesCorrupted, rec.AbsPath)
			continue
		}

		if uerr := repo.UpdateScrubStatus(ctx, rec.AbsPath); uerr != nil {
			slog.Warn("update scrub status", "path", rec.AbsPath, "err", uerr)
		}
		res.FilesValidated++
		if res.FilesValidated%100 == 0 {
			slog.Info("scrub progress", "validated", res.FilesValidated, "total", len(files))
		}
	}

	slog.Info("scrub complete",
		"validated", res.FilesValidated,
		"corrupted", len(res.FilesCorrupted),
		"errors", len(res.Errors),
	)
	return res, nil
}

// skipSystemDir returns filepath.SkipDir for well-known Windows system folders
// and hidden directories to avoid unnecessary traversal.
func skipSystemDir(name string) error {
	switch name {
	case "$RECYCLE.BIN", "System Volume Information", "Recovery",
		"$Recycle.Bin", "Windows", "Program Files", "Program Files (x86)",
		"ProgramData":
		return filepath.SkipDir
	}
	return nil
}

// findMovedFile searches dbFiles for a record whose size and mtime match and
// whose old path is no longer present on disk (seenFiles).  This prevents
// false-positive move detection when duplicates exist.
func findMovedFile(
	newPath string,
	size int64,
	mtime time.Time,
	dbFiles map[string]*domain.FileRecord,
	seenFiles map[string]struct{ size int64; mtime time.Time },
) string {
	for dbPath, rec := range dbFiles {
		if dbPath == newPath {
			continue
		}
		// Old path must no longer be visible on disk.
		if _, stillExists := seenFiles[dbPath]; stillExists {
			continue
		}
		if rec.FileSize == size && rec.Mtime.Unix() == mtime.Unix() {
			return dbPath
		}
	}
	return ""
}
