// Package domain defines the core interfaces for the Bit Rot Detector.
package domain

import "context"

// Hasher computes a cryptographic hash of a file.
type Hasher interface {
	// ComputeHash returns the BLAKE3 hex-encoded hash of the file at path.
	ComputeHash(ctx context.Context, path string) (string, error)
}

// Repository abstracts all database operations.
type Repository interface {
	// GetAllFiles returns all file records keyed by absolute path.
	GetAllFiles(ctx context.Context) (map[string]*FileRecord, error)

	// UpsertFile inserts or updates a file record.
	UpsertFile(ctx context.Context, record *FileRecord) error

	// DeleteFile removes a file record by its absolute path.
	DeleteFile(ctx context.Context, path string) error

	// GetFilesForScrub returns files eligible for scrubbing.
	GetFilesForScrub(ctx context.Context, percentage float64, minAgeDays *int) ([]*FileRecord, error)

	// UpdateScrubStatus updates the last-scrubbed timestamp and increments scrub_count.
	UpdateScrubStatus(ctx context.Context, path string) error

	// ComputeChecksum returns the BLAKE3 checksum of the underlying database file.
	ComputeChecksum() (string, error)

	// Commit finalises the shadow DB by atomically replacing the production DB.
	// It must be called once after all writes succeed.
	Commit() error

	// Rollback discards the shadow DB, leaving the production DB untouched.
	Rollback() error

	// Close releases all database resources.
	Close() error
}
