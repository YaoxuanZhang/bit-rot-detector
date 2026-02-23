// Package settings provides in-memory and file-backed storage for
// user-configurable application settings that are separate from the
// environment-variable-driven config package.
package settings

import (
	"encoding/json"
	"os"
	"sync"
)

// DiskThresholds holds configurable disk-usage warning/critical levels.
type DiskThresholds struct {
	WarnPercent  int `json:"warn_pct"`  // default 75
	ErrorPercent int `json:"error_pct"` // default 90
}

// NotificationRules holds per-event email notification toggles.
type NotificationRules struct {
	OnSuccess    bool `json:"on_success"`
	OnWarning    bool `json:"on_warning"`
	OnCorruption bool `json:"on_corruption"`
	OnError      bool `json:"on_error"`
}

// Settings is the persisted application configuration (separate from env config).
type Settings struct {
	DiskThresholds    DiskThresholds    `json:"disk_thresholds"`
	NotificationRules NotificationRules `json:"notification_rules"`
}

// DefaultSettings returns factory defaults.
func DefaultSettings() Settings {
	return Settings{
		DiskThresholds: DiskThresholds{
			WarnPercent:  75,
			ErrorPercent: 90,
		},
		NotificationRules: NotificationRules{
			OnSuccess:    false,
			OnWarning:    true,
			OnCorruption: true,
			OnError:      true,
		},
	}
}

// Store holds settings in memory with optional file persistence.
type Store struct {
	mu       sync.RWMutex
	data     Settings
	filePath string
}

// New creates a Store.  If filePath is non-empty and the file exists, it loads
// from disk.  Otherwise it starts with defaults.
func New(filePath string) *Store {
	s := &Store{
		data:     DefaultSettings(),
		filePath: filePath,
	}
	if filePath != "" {
		raw, err := os.ReadFile(filePath)
		if err == nil {
			var loaded Settings
			if jsonErr := json.Unmarshal(raw, &loaded); jsonErr == nil {
				s.data = loaded
			}
		}
	}
	return s
}

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

// Set replaces settings and persists to disk if a filePath was configured.
func (s *Store) Set(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = v
	if s.filePath == "" {
		return nil
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, raw, 0o644)
}
