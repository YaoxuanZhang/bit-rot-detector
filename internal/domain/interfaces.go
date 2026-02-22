// Package domain defines the core interfaces for the Bit Rot Detector.
//
// Keeping the interfaces here (rather than in the packages that implement
// them) allows every other package to depend solely on this package, avoiding
// import cycles and making it straightforward to substitute alternative
// implementations in tests.
package domain

import "context"

// Hasher computes a cryptographic hash of a file on disk.
//
// The only built-in implementation uses BLAKE3 (internal/hasher), but the
// interface is defined here so tests can supply a fake.
type Hasher interface {
	// ComputeHash returns the lowercase hex-encoded BLAKE3 digest of the
	// file at path.  Implementations must honour ctx cancellation between
	// read chunks so that large files can be interrupted promptly.
	ComputeHash(ctx context.Context, path string) (string, error)
}

// Repository abstracts all database operations for a single monitored
// directory.  The concrete implementation in internal/storage uses SQLite
// with an Atomic Shadow-DB Swap pattern:
//
//  1. On [storage.Open], a shadow copy of the production database is created.
//  2. All reads and writes target the shadow.
//  3. On [Repository.Commit], the shadow is atomically renamed over the
//     production database (POSIX rename guarantee).
//  4. On [Repository.Rollback] or a crash, the shadow is discarded and the
//     production database remains the "Last Known Good" state.
type Repository interface {
	// GetAllFiles returns every file record in the database, keyed by
	// absolute path.  Returns an empty (non-nil) map when the database is
	// empty.
	GetAllFiles(ctx context.Context) (map[string]*FileRecord, error)

	// UpsertFile inserts a new file record or replaces the existing record
	// for the same AbsPath.  The AddedAt field is preserved on updates if
	// it is non-zero; otherwise the current time is used.
	UpsertFile(ctx context.Context, record *FileRecord) error

	// DeleteFile removes the record for path from the database.  It is not
	// an error if the record does not exist.
	DeleteFile(ctx context.Context, path string) error

	// GetFilesForScrub returns up to percentage% of all files (rounded up
	// to at least 1), ordered by least-recently scrubbed first.  When
	// minAgeDays is non-nil, only files whose last_scrubbed timestamp is
	// older than minAgeDays days (or that have never been scrubbed) are
	// considered.
	GetFilesForScrub(ctx context.Context, percentage float64, minAgeDays *int) ([]*FileRecord, error)

	// UpdateScrubStatus sets last_scrubbed to now and increments scrub_count
	// for the file at path.
	UpdateScrubStatus(ctx context.Context, path string) error

	// ComputeChecksum returns the lowercase hex-encoded BLAKE3 digest of the
	// shadow database file.  This is used to detect database corruption
	// between runs.
	ComputeChecksum() (string, error)

	// Commit finalises the run by atomically renaming the shadow database
	// over the production database.  It must be called at most once, after
	// all writes have succeeded.  Subsequent calls to any other method will
	// return errors because the underlying connection is closed.
	Commit() error

	// Rollback discards the shadow database, leaving the production database
	// untouched.  It is safe to call Rollback multiple times (idempotent).
	Rollback() error

	// Close releases the database connection without modifying either the
	// shadow or the production database.  Call Commit or Rollback before
	// Close to control which database survives.
	Close() error
}

