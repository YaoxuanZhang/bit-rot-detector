// Package coordinator orchestrates multi-drive scanning with IO-aware
// concurrency.  Each drive is processed by its own goroutine; the number of
// hashing workers per drive is determined by the drive's media type.
package coordinator

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/hasher"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/monitor"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/scanner"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/storage"
)

// DriveResult holds the outcome of processing a single drive.
// A nil SyncResult or ScrubResult means the corresponding phase was not run.
type DriveResult struct {
	// Drive is a human-readable label derived from the mount-point base name.
	Drive string

	// Health contains disk-usage statistics collected at the start of the run.
	Health *domain.DriveHealth

	// SyncResult is the outcome of the sync phase, or nil if RunSync was false.
	SyncResult *domain.SyncResult

	// ScrubResult is the outcome of the scrub phase, or nil if RunScrub was false.
	ScrubResult *domain.ScrubResult

	// Err is non-nil if a fatal error occurred during processing of this drive.
	// When Err is non-nil, SyncResult and ScrubResult may be nil or partial.
	Err error
}

// Options configures what operations to run on each drive.
type Options struct {
	// RunSync enables the sync phase (directory walk + hash comparison).
	RunSync bool

	// RunScrub enables the scrub phase (re-hash a percentage of stored files).
	RunScrub bool

	// ScrubPercentage is the fraction of stored files to re-verify per run
	// (0.1–100.0).  Passed directly to [domain.Repository.GetFilesForScrub].
	ScrubPercentage float64

	// ScrubFrequency controls the minimum-age filter for scrub selection:
	// "daily" (no filter), "weekly" (≥7 days), or "monthly" (≥30 days).
	ScrubFrequency string

	// MaxWorkers is the upper bound on hashing workers per drive.
	// The IO-aware [monitor.Detect] call may further reduce this.
	MaxWorkers int
}

// Run processes all target drives concurrently and returns aggregated results.
func Run(ctx context.Context, targetPaths []string, opts Options) ([]DriveResult, time.Duration) {
	start := time.Now()

	numDrives := len(targetPaths)
	if numDrives == 0 {
		return nil, 0
	}

	// Limit outer parallelism to MaxWorkers or number of drives.
	semaphore := make(chan struct{}, min(opts.MaxWorkers, numDrives))

	results := make([]DriveResult, numDrives)
	var wg sync.WaitGroup

	for i, path := range targetPaths {
		wg.Add(1)
		go func(idx int, drivePath string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			results[idx] = processDrive(ctx, drivePath, opts)
		}(i, path)
	}

	wg.Wait()
	return results, time.Since(start)
}

// processDrive handles canary checks, shadow-DB open/commit/rollback, sync,
// and scrub for a single drive.
func processDrive(ctx context.Context, drivePath string, opts Options) DriveResult {
	res := DriveResult{Drive: filepath.Base(drivePath)}
	if res.Drive == "." || res.Drive == "" {
		res.Drive = drivePath
	}

	// Collect disk-usage health.
	res.Health = diskHealth(drivePath)

	// ── Canary pre-check ────────────────────────────────────────────────────
	canaryPath := filepath.Join(drivePath, ".bitrot-canary")
	storedChecksum, err := readCanary(canaryPath)
	if err != nil {
		res.Err = fmt.Errorf("canary pre-check failed: %w", err)
		slog.Error("canary check failed", "drive", res.Drive, "err", err)
		return res
	}
	slog.Info("canary OK", "drive", res.Drive, "path", canaryPath)

	// ── Open shadow DB ──────────────────────────────────────────────────────
	repo, err := storage.Open(drivePath)
	if err != nil {
		res.Err = fmt.Errorf("open repository: %w", err)
		return res
	}

	// Ensure the shadow is always cleaned up if we exit early.
	committed := false
	defer func() {
		if !committed {
			if rbErr := repo.Rollback(); rbErr != nil {
				slog.Warn("rollback failed", "drive", res.Drive, "err", rbErr)
			}
		}
	}()

	// ── Verify stored DB checksum (if present) ──────────────────────────────
	if storedChecksum != "" {
		current, err := repo.ComputeChecksum()
		if err != nil {
			slog.Warn("could not compute DB checksum; skipping verification",
				"drive", res.Drive, "err", err)
		} else if current != storedChecksum {
			// Treat mismatch as a recoverable warning rather than a fatal error.
			// This handles the case where a previous canary write succeeded but
			// the DB commit did not (or vice versa), which would otherwise cause
			// every subsequent run to fail permanently.
			slog.Warn("DB checksum mismatch – continuing with best-effort recovery; "+
				"canary will be rewritten on successful commit",
				"drive", res.Drive,
				"stored_prefix", storedChecksum[:min(16, len(storedChecksum))],
				"current_prefix", current[:min(16, len(current))],
			)
		} else {
			slog.Info("DB checksum verified", "drive", res.Drive)
		}
	}

	// ── IO-aware worker count ────────────────────────────────────────────────
	driveInfo := monitor.Detect(drivePath)
	res.Health.IsRotational = driveInfo.Type == monitor.DriveTypeRotational
	numWorkers := driveInfo.Workers
	if numWorkers > opts.MaxWorkers {
		numWorkers = opts.MaxWorkers
	}

	h := hasher.New()
	syncer := scanner.NewSyncer(h, numWorkers)

	// ── Sync ─────────────────────────────────────────────────────────────────
	if opts.RunSync {
		syncRes, err := syncer.SyncDirectory(ctx, drivePath, repo)
		if err != nil {
			res.Err = fmt.Errorf("sync: %w", err)
			return res
		}
		res.SyncResult = syncRes
	}

	// ── Scrub ────────────────────────────────────────────────────────────────
	if opts.RunScrub {
		var minAgeDays *int
		switch opts.ScrubFrequency {
		case "weekly":
			d := 7
			minAgeDays = &d
		case "monthly":
			d := 30
			minAgeDays = &d
		}
		scrubRes, err := syncer.ScrubFiles(ctx, repo, opts.ScrubPercentage, minAgeDays)
		if err != nil {
			res.Err = fmt.Errorf("scrub: %w", err)
			return res
		}
		res.ScrubResult = scrubRes
	}

	// ── Canary post-check ────────────────────────────────────────────────────
	// Verify canary still exists after the scan (mount may have disappeared).
	if _, err := os.Stat(canaryPath); err != nil {
		res.Err = fmt.Errorf("canary post-check failed: %w", err)
		slog.Error("canary disappeared during scan", "drive", res.Drive)
		return res
	}

	// ── Commit shadow DB ─────────────────────────────────────────────────────
	if err := repo.Commit(); err != nil {
		res.Err = fmt.Errorf("commit: %w", err)
		return res
	}
	committed = true
	slog.Info("drive processed successfully", "drive", res.Drive)

	// ── Update canary checksum ───────────────────────────────────────────────
	newChecksum, err := hasher.ComputeChecksumFile(filepath.Join(drivePath, "bitrot.db"))
	if err != nil {
		slog.Warn("could not compute new DB checksum for canary", "err", err)
	} else if err := writeCanary(canaryPath, newChecksum); err != nil {
		slog.Warn("could not update canary", "err", err)
	}

	return res
}

// readCanary reads the canary file and returns the stored checksum (may be empty).
// Returns an error if the canary file does not exist.
func readCanary(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(".bitrot-canary not found at %s", path)
		}
		return "", err
	}
	return string(data), nil // may be empty on first run
}

// writeCanary writes the checksum to the canary file.
func writeCanary(path, checksum string) error {
	return os.WriteFile(path, []byte(checksum), fs.FileMode(0o644))
}

// diskHealth collects disk usage statistics for a drive path.
func diskHealth(drivePath string) *domain.DriveHealth {
	h := &domain.DriveHealth{
		DriveName:   filepath.Base(drivePath),
		SmartStatus: "UNKNOWN",
	}
	if h.DriveName == "." {
		h.DriveName = drivePath
	}

	var stat syscallStatFS
	if err := statFS(drivePath, &stat); err != nil {
		slog.Warn("disk usage unavailable", "path", drivePath, "err", err)
		return h
	}
	h.TotalSpace = stat.Total
	h.UsedSpace = stat.Used
	h.FreeSpace = stat.Free
	return h
}

// toMailerSyncEntries converts DriveResult slice to mailer.SyncEntry slice.
func ToMailerSyncEntries(results []DriveResult) []mailer.SyncEntry {
	var out []mailer.SyncEntry
	for _, r := range results {
		if r.SyncResult != nil {
			out = append(out, mailer.SyncEntry{Drive: r.Drive, Result: r.SyncResult})
		}
	}
	return out
}

// ToMailerScrubEntries converts DriveResult slice to mailer.ScrubEntry slice.
func ToMailerScrubEntries(results []DriveResult) []mailer.ScrubEntry {
	var out []mailer.ScrubEntry
	for _, r := range results {
		if r.ScrubResult != nil {
			out = append(out, mailer.ScrubEntry{Drive: r.Drive, Result: r.ScrubResult})
		}
	}
	return out
}

// ToHealthSlice extracts the DriveHealth pointers from results.
func ToHealthSlice(results []DriveResult) []*domain.DriveHealth {
	out := make([]*domain.DriveHealth, 0, len(results))
	for _, r := range results {
		if r.Health != nil {
			out = append(out, r.Health)
		}
	}
	return out
}

// CollectErrors returns a flat list of error strings from all drive results.
func CollectErrors(results []DriveResult) []string {
	var errs []string
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, fmt.Sprintf("[%s] %v", r.Drive, r.Err))
		}
	}
	return errs
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
