// Package storage provides the SQLite-backed Repository with an
// Atomic Shadow-DB Swap pattern for safe crash recovery.
//
// # Shadow-DB Swap
//
// On [Open], the production database (bitrot.db) is copied to a temporary
// shadow file (bitrot.db.shadow).  All reads and writes during a run target
// the shadow.  On a successful [Repository.Commit], the shadow is atomically
// renamed over the production file using [os.Rename], which is guaranteed to
// be atomic on POSIX filesystems when both files reside on the same device.
//
// If the process is interrupted at any point before Commit, the shadow file
// can be safely deleted (or will be overwritten on the next run) and the
// production database remains unchanged.
//
// # Driver
//
// The CGO-free [modernc.org/sqlite] driver is used so the binary can be built
// without a C toolchain and deployed as a fully static binary.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/hasher"
	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

const (
	prodDBName   = "bitrot.db"
	shadowSuffix = ".shadow"
)

// Repository implements domain.Repository backed by SQLite.
type Repository struct {
	dir        string
	prodPath   string
	shadowPath string
	db         *sql.DB
}

// Open creates a new Repository for the given directory.
// The production DB is copied to a shadow file; all subsequent
// operations target the shadow file.
func Open(dir string) (*Repository, error) {
	prodPath := filepath.Join(dir, prodDBName)
	shadowPath := prodPath + shadowSuffix

	// Remove any leftover shadow file (and associated WAL/SHM) from a previous crashed run.
	for _, p := range []string{shadowPath, shadowPath + "-wal", shadowPath + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Warn("could not remove stale shadow DB file", "path", p, "err", err)
		}
	}

	// Copy production DB to shadow (creates shadow if production doesn't exist).
	if err := copyFile(prodPath, shadowPath); err != nil {
		return nil, fmt.Errorf("create shadow DB: %w", err)
	}

	db, err := openSQLite(shadowPath)
	if err != nil {
		_ = os.Remove(shadowPath)
		return nil, err
	}

	r := &Repository{
		dir:        dir,
		prodPath:   prodPath,
		shadowPath: shadowPath,
		db:         db,
	}

	if err := r.createSchema(); err != nil {
		_ = db.Close()
		_ = os.Remove(shadowPath)
		return nil, err
	}

	return r, nil
}

// openSQLite opens (or creates) a SQLite database at path.
// DELETE journal mode is used instead of WAL so that no additional sidecar
// files (-wal, -shm) are created; this keeps the shadow-swap copy operation
// simple and correct.
func openSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer
	if _, err := db.Exec("PRAGMA journal_mode=DELETE;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite pragma: %w", err)
	}
	// Read and assert the integrity_check result (not just exec-and-discard).
	var ic string
	if err := db.QueryRow("PRAGMA integrity_check;").Scan(&ic); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite integrity_check: %w", err)
	}
	if ic != "ok" {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite integrity_check failed: %s", ic)
	}
	return db, nil
}

// copyFile copies src to dst, creating dst if it does not exist.
// If src does not exist, dst is created as an empty file.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			// No production DB yet; create an empty shadow.
			f, createErr := os.Create(dst)
			if createErr != nil {
				return fmt.Errorf("create empty shadow: %w", createErr)
			}
			return f.Close()
		}
		return fmt.Errorf("open src %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return out.Sync()
}

func (r *Repository) createSchema() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			abs_path     TEXT    PRIMARY KEY,
			hash         TEXT    NOT NULL,
			added_at     INTEGER NOT NULL,
			last_seen    INTEGER NOT NULL,
			last_scrubbed INTEGER,
			scrub_count  INTEGER NOT NULL DEFAULT 0,
			file_size    INTEGER NOT NULL,
			mtime        INTEGER NOT NULL
		)`)
	return err
}

// GetAllFiles returns all file records keyed by absolute path.
func (r *Repository) GetAllFiles(ctx context.Context) (map[string]*domain.FileRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT abs_path, hash, added_at, last_seen, last_scrubbed, scrub_count, file_size, mtime
		 FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]*domain.FileRecord)
	for rows.Next() {
		var rec domain.FileRecord
		var addedAt, lastSeen, mtime int64
		var lastScrubbed sql.NullInt64

		if err := rows.Scan(
			&rec.AbsPath, &rec.Hash, &addedAt, &lastSeen,
			&lastScrubbed, &rec.ScrubCount, &rec.FileSize, &mtime,
		); err != nil {
			return nil, err
		}

		rec.AddedAt = time.Unix(addedAt, 0)
		rec.LastSeen = time.Unix(lastSeen, 0)
		rec.Mtime = time.Unix(mtime, 0)
		if lastScrubbed.Valid {
			t := time.Unix(lastScrubbed.Int64, 0)
			rec.LastScrubbed = &t
		}
		out[rec.AbsPath] = &rec
	}
	return out, rows.Err()
}

// UpsertFile inserts or replaces a file record.
func (r *Repository) UpsertFile(ctx context.Context, rec *domain.FileRecord) error {
	var lastScrubbed *int64
	if rec.LastScrubbed != nil {
		v := rec.LastScrubbed.Unix()
		lastScrubbed = &v
	}
	addedAt := rec.AddedAt.Unix()
	if addedAt == 0 {
		addedAt = time.Now().Unix()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO files
		 (abs_path, hash, added_at, last_seen, last_scrubbed, scrub_count, file_size, mtime)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.AbsPath, rec.Hash,
		addedAt,
		time.Now().Unix(),
		lastScrubbed,
		rec.ScrubCount,
		rec.FileSize,
		rec.Mtime.Unix(),
	)
	return err
}

// DeleteFile removes a file record by absolute path.
func (r *Repository) DeleteFile(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE abs_path = ?`, path)
	return err
}

// GetFilesForScrub returns files eligible for scrubbing ordered by least-recently scrubbed.
func (r *Repository) GetFilesForScrub(ctx context.Context, percentage float64, minAgeDays *int) ([]*domain.FileRecord, error) {
	query := `SELECT abs_path, hash, added_at, last_seen, last_scrubbed, scrub_count, file_size, mtime FROM files`
	var args []any

	if minAgeDays != nil {
		cutoff := time.Now().Add(-time.Duration(*minAgeDays) * 24 * time.Hour).Unix()
		query += ` WHERE last_scrubbed IS NULL OR last_scrubbed < ?`
		args = append(args, cutoff)
	}
	query += ` ORDER BY last_scrubbed ASC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []*domain.FileRecord
	for rows.Next() {
		var rec domain.FileRecord
		var addedAt, lastSeen, mtime int64
		var lastScrubbed sql.NullInt64

		if err := rows.Scan(
			&rec.AbsPath, &rec.Hash, &addedAt, &lastSeen,
			&lastScrubbed, &rec.ScrubCount, &rec.FileSize, &mtime,
		); err != nil {
			return nil, err
		}

		rec.AddedAt = time.Unix(addedAt, 0)
		rec.LastSeen = time.Unix(lastSeen, 0)
		rec.Mtime = time.Unix(mtime, 0)
		if lastScrubbed.Valid {
			t := time.Unix(lastScrubbed.Int64, 0)
			rec.LastScrubbed = &t
		}
		all = append(all, &rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	n := max(1, int(float64(len(all))*percentage/100.0))
	if n > len(all) {
		n = len(all)
	}
	return all[:n], nil
}

// UpdateScrubStatus updates the last-scrubbed timestamp and increments scrub_count.
func (r *Repository) UpdateScrubStatus(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE files SET last_scrubbed = ?, scrub_count = scrub_count + 1 WHERE abs_path = ?`,
		time.Now().Unix(), path,
	)
	return err
}

// ComputeChecksum returns the BLAKE3 hash of the shadow DB file.
func (r *Repository) ComputeChecksum() (string, error) {
	return hasher.ComputeChecksumFile(r.shadowPath)
}

// Commit closes the shadow DB and atomically renames it over the production DB.
func (r *Repository) Commit() error {
	if err := r.db.Close(); err != nil {
		return fmt.Errorf("close shadow DB: %w", err)
	}
	r.db = nil
	if err := os.Rename(r.shadowPath, r.prodPath); err != nil {
		return fmt.Errorf("atomic rename shadow -> prod: %w", err)
	}
	slog.Info("shadow DB committed", "path", r.prodPath)
	return nil
}

// Rollback closes the shadow DB and removes the shadow file, leaving the
// production DB untouched.
func (r *Repository) Rollback() error {
	if r.db != nil {
		_ = r.db.Close()
		r.db = nil
	}
	if err := os.Remove(r.shadowPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove shadow DB: %w", err)
	}
	slog.Info("shadow DB rolled back", "shadow", r.shadowPath)
	return nil
}

// Close releases all database resources without modifying the production DB.
// Use Commit() to persist changes or Rollback() to discard them.
func (r *Repository) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
