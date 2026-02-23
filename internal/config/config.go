// Package config loads application configuration from a YAML file.
//
// Non-secret configuration is stored in config.yaml.  Secrets
// (SMTP_USERNAME, SMTP_PASSWORD) are always read from environment variables
// and are never written to disk or returned by the API.
//
// # Quick start
//
//	cfg, err := config.Load("config.yaml")
//
// If the file does not exist a defaults file is written and returned.
// Secrets are overlaid from the environment after every load.
//
// See config.yaml.example in the repository root for a documented list of
// all supported keys and their default values.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
)

// DiskThresholds holds configurable disk-usage warning/critical levels.
type DiskThresholds struct {
	WarnPercent  int `yaml:"warn_pct"  json:"warn_pct"`  // default 75
	ErrorPercent int `yaml:"error_pct" json:"error_pct"` // default 90
}

// NotificationRules holds per-event email notification toggles.
type NotificationRules struct {
	OnSuccess    bool `yaml:"on_success"    json:"on_success"`
	OnWarning    bool `yaml:"on_warning"    json:"on_warning"`
	OnCorruption bool `yaml:"on_corruption" json:"on_corruption"`
	OnError      bool `yaml:"on_error"      json:"on_error"`
}

// ScheduleEntry defines when an operation should run automatically.
type ScheduleEntry struct {
	ID       string    `yaml:"id,omitempty"       json:"id"`
	Label    string    `yaml:"label"              json:"label"`
	Enabled  bool      `yaml:"enabled"            json:"enabled"`
	CronExpr string    `yaml:"cron_expr"          json:"cron_expr"`
	LastRun  time.Time `yaml:"last_run,omitempty" json:"last_run,omitempty"`
	NextRun  time.Time `yaml:"next_run,omitempty" json:"next_run,omitempty"`
}

// Config holds all application settings resolved at startup.
type Config struct {
	// TargetPaths is the list of directory paths to monitor.
	TargetPaths []string `yaml:"target_paths" json:"target_paths"`

	// SMTP contains the email notification settings.
	// Username and Password are secret fields overlaid from env vars and are
	// never written to disk.
	SMTP mailer.Config `yaml:"smtp" json:"smtp"`

	// ScrubPercentage is the fraction of files to re-verify per run (0.1–100.0).
	ScrubPercentage float64 `yaml:"scrub_percentage" json:"scrub_percentage"`

	// ScrubFrequency controls the minimum age filter for scrubbing:
	// "daily" (no age filter), "weekly" (≥7 days), or "monthly" (≥30 days).
	ScrubFrequency string `yaml:"scrub_frequency" json:"scrub_frequency"`

	// MaxWorkers is the upper bound on concurrent hashing goroutines.
	MaxWorkers int `yaml:"max_workers" json:"max_workers"`

	// LogLevel controls the minimum severity for console log output.
	// Valid values: DEBUG, INFO, WARN, ERROR.
	LogLevel string `yaml:"log_level" json:"log_level"`

	// LogRetentionDays is the number of days to retain old log files.
	LogRetentionDays int `yaml:"log_retention_days" json:"log_retention_days"`

	// DiskThresholds holds disk-usage warning/critical alert levels.
	DiskThresholds DiskThresholds `yaml:"disk_thresholds" json:"disk_thresholds"`

	// NotificationRules holds per-event email notification toggles.
	NotificationRules NotificationRules `yaml:"notification_rules" json:"notification_rules"`

	// Schedule holds auto-run schedule entries.
	Schedule []ScheduleEntry `yaml:"schedule" json:"schedule"`
}

// DefaultConfig returns a Config pre-filled with sensible defaults.
func DefaultConfig() Config {
	return Config{
		SMTP: mailer.Config{
			Host: "mail.smtp2go.com",
			Port: 587,
		},
		ScrubPercentage:  1.0,
		ScrubFrequency:   "daily",
		MaxWorkers:       4,
		LogLevel:         "INFO",
		LogRetentionDays: 7,
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
		Schedule: []ScheduleEntry{},
	}
}

// Load reads the config from path. If the file does not exist, defaults are
// written to path and returned. After loading, secrets are overlaid from env:
//   - SMTP_USERNAME → cfg.SMTP.Username
//   - SMTP_PASSWORD → cfg.SMTP.Password
//
// cfg.SMTP.NotifyOnSuccess is derived from cfg.NotificationRules.OnSuccess.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
		// File does not exist: write defaults so the user has a starting point.
		if saveErr := Save(&cfg, path); saveErr != nil {
			// Non-fatal: we can still run with in-memory defaults.
			_ = saveErr
		}
	} else {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}

	// Validate required fields.
	if len(cfg.TargetPaths) == 0 {
		return nil, fmt.Errorf("config: target_paths must contain at least one path")
	}
	for _, p := range cfg.TargetPaths {
		if p == "" {
			continue
		}
		if _, statErr := os.Stat(p); statErr != nil {
			return nil, fmt.Errorf("config: target path does not exist: %s", p)
		}
	}

	overlaySecrets(&cfg)
	return &cfg, nil
}

// Save atomically writes cfg to path in YAML format.
// Fields tagged yaml:"-" (SMTP credentials) are excluded automatically.
func Save(cfg *Config, path string) error {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("config: mkdir %s: %w", dir, err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

// overlaySecrets sets credential fields from environment variables.
// These fields are tagged yaml:"-" and are never persisted.
func overlaySecrets(cfg *Config) {
	if u := os.Getenv("SMTP_USERNAME"); u != "" {
		cfg.SMTP.Username = u
	}
	if p := os.Getenv("SMTP_PASSWORD"); p != "" {
		cfg.SMTP.Password = p
	}
	cfg.SMTP.NotifyOnSuccess = cfg.NotificationRules.OnSuccess
}

// Store provides thread-safe access to Config with optional YAML persistence.
type Store struct {
	mu       sync.RWMutex
	cfg      Config
	filePath string
}

// NewStore creates a Store wrapping cfg. If filePath is non-empty, Update
// calls will persist changes to disk.
func NewStore(cfg Config, filePath string) *Store {
	return &Store{cfg: cfg, filePath: filePath}
}

// Get returns a deep copy of the current config.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopy(s.cfg)
}

// deepCopy returns a copy of cfg with independent slice fields.
func deepCopy(cfg Config) Config {
	out := cfg
	if cfg.TargetPaths != nil {
		out.TargetPaths = make([]string, len(cfg.TargetPaths))
		copy(out.TargetPaths, cfg.TargetPaths)
	}
	if cfg.Schedule != nil {
		out.Schedule = make([]ScheduleEntry, len(cfg.Schedule))
		copy(out.Schedule, cfg.Schedule)
	}
	return out
}

// Update applies fn to a copy of the config, saves to disk if filePath is
// set, then atomically swaps the in-memory copy. fn must not block.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := s.cfg // copy
	if err := fn(&updated); err != nil {
		return err
	}
	overlaySecrets(&updated)
	if s.filePath != "" {
		if err := Save(&updated, s.filePath); err != nil {
			return err
		}
	}
	s.cfg = updated
	return nil
}
