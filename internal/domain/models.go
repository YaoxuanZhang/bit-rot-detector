// Package domain defines the core data models for the Bit Rot Detector.
//
// All types in this package are plain data structures with no behaviour; they
// are shared across the hasher, storage, scanner, mailer and coordinator
// packages to avoid import cycles.
package domain

import "time"

// FileRecord represents a single file entry stored in the integrity database.
// One record is kept per absolute path; the record is updated whenever the
// file is synced or scrubbed.
type FileRecord struct {
	// AbsPath is the canonical, absolute path to the file on disk.
	AbsPath string

	// Hash is the hex-encoded BLAKE3 digest recorded during the last sync.
	Hash string

	// AddedAt is the time the file was first observed by a sync operation.
	AddedAt time.Time

	// LastSeen is the time of the most recent successful sync for this file.
	LastSeen time.Time

	// LastScrubbed is the time of the most recent successful scrub for this
	// file, or nil if the file has never been scrubbed.
	LastScrubbed *time.Time

	// ScrubCount is the total number of times this file has been scrubbed.
	ScrubCount int

	// FileSize is the size of the file in bytes at the time of the last sync.
	FileSize int64

	// Mtime is the filesystem modification time of the file at the time of
	// the last sync.  It is used together with FileSize to detect changes
	// without re-hashing every file on every run.
	Mtime time.Time
}

// SyncResult holds the statistics gathered during a single sync operation over
// one directory tree.
type SyncResult struct {
	// FilesScanned is the total number of files visited by the walker.
	FilesScanned int

	// FilesAdded is the number of new files inserted into the database.
	FilesAdded int

	// FilesModified is the number of files whose size or mtime changed.
	FilesModified int

	// FilesMoved is the number of files detected as relocated (same
	// size+mtime at a new path).
	FilesMoved int

	// FilesRemoved is the number of database entries deleted because the
	// corresponding file no longer exists on disk.
	FilesRemoved int

	// Errors contains human-readable descriptions of non-fatal errors
	// encountered during the sync (e.g. permission denied).
	Errors []string
}

// ScrubResult holds the statistics gathered during a single scrub operation.
type ScrubResult struct {
	// FilesValidated is the number of files whose hash matched the stored value.
	FilesValidated int

	// FilesCorrupted contains the absolute paths of files whose current hash
	// differs from the stored value – these are potential bit-rot events.
	FilesCorrupted []string

	// Errors contains human-readable descriptions of non-fatal errors
	// encountered during the scrub (e.g. file disappeared between sync and
	// scrub).
	Errors []string
}

// DriveHealth holds disk-usage and SMART statistics for a single drive.
// Fields that cannot be determined on a given platform are left at their zero
// value.
type DriveHealth struct {
	// DriveName is a human-readable label derived from the mount-point base name.
	DriveName string

	// TotalSpace is the total capacity of the filesystem in bytes.
	TotalSpace uint64

	// UsedSpace is the number of bytes currently in use.
	UsedSpace uint64

	// FreeSpace is the number of bytes available to unprivileged users.
	FreeSpace uint64

	// Temperature is the drive temperature in degrees Celsius, or nil if
	// SMART data is unavailable.
	Temperature *int

	// SmartStatus is a short human-readable SMART health summary
	// (e.g. "PASSED", "FAILED", "UNKNOWN").
	SmartStatus string

	// SmartErrors contains any SMART error log entries detected.
	SmartErrors []string

	// IsRotational is true when the drive is backed by a spinning platter
	// (HDD), as determined by reading /sys/block/<dev>/queue/rotational.
	IsRotational bool
}

// WorkItem is a unit of work sent from the directory-walker goroutine (the
// producer) to the hashing worker pool (the consumers) via a buffered channel.
type WorkItem struct {
	// Path is the absolute path to the file to be hashed.
	Path string

	// Size is the file's size in bytes at the time of the walk.
	Size int64

	// Mtime is the file's modification time at the time of the walk.
	Mtime time.Time
}

// WorkResult is the result produced by a hashing worker and sent back to the
// collector goroutine.
type WorkResult struct {
	// Path is the absolute path that was hashed (mirrors WorkItem.Path).
	Path string

	// Hash is the hex-encoded BLAKE3 digest, or empty if Err is non-nil.
	Hash string

	// Size mirrors WorkItem.Size.
	Size int64

	// Mtime mirrors WorkItem.Mtime.
	Mtime time.Time

	// Err is non-nil if hashing failed (e.g. permission denied, I/O error).
	Err error
}

