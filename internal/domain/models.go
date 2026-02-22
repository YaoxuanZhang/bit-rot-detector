// Package domain defines the core data models for the Bit Rot Detector.
package domain

import "time"

// FileRecord represents a file entry stored in the database.
type FileRecord struct {
	AbsPath     string
	Hash        string
	AddedAt     time.Time
	LastSeen    time.Time
	LastScrubbed *time.Time
	ScrubCount  int
	FileSize    int64
	Mtime       time.Time
}

// SyncResult holds the statistics from a sync operation.
type SyncResult struct {
	FilesScanned  int
	FilesAdded    int
	FilesModified int
	FilesMoved    int
	FilesRemoved  int
	Errors        []string
}

// ScrubResult holds the statistics from a scrub operation.
type ScrubResult struct {
	FilesValidated int
	FilesCorrupted []string
	Errors         []string
}

// DriveHealth holds disk usage and SMART statistics for a drive.
type DriveHealth struct {
	DriveName   string
	TotalSpace  uint64
	UsedSpace   uint64
	FreeSpace   uint64
	Temperature *int
	SmartStatus string
	SmartErrors []string
	IsRotational bool
}

// WorkItem is a unit of work sent from the producer to the worker pool.
type WorkItem struct {
	Path  string
	Size  int64
	Mtime time.Time
}

// WorkResult is the result returned by a worker after processing a WorkItem.
type WorkResult struct {
	Path    string
	Hash    string
	Size    int64
	Mtime   time.Time
	Err     error
}
