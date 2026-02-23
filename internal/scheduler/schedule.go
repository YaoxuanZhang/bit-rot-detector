// Package scheduler provides persistent schedule entries and a background
// runner that fires a trigger function when an entry comes due.
package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"
)

// ScheduleEntry defines when an operation should run automatically.
type ScheduleEntry struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Enabled  bool      `json:"enabled"`
	CronExpr string    `json:"cron_expr"` // "@daily", "@weekly", "@hourly", or "HH:MM"
	LastRun  time.Time `json:"last_run,omitempty"`
	NextRun  time.Time `json:"next_run,omitempty"`
}

// Store persists schedule entries in memory with optional file backing.
type Store struct {
	mu       sync.RWMutex
	entries  []ScheduleEntry
	filePath string
}

// New creates a Store.  If filePath is non-empty and the file exists its
// content is loaded; otherwise the store starts empty.
func New(filePath string) *Store {
	s := &Store{filePath: filePath}
	if filePath != "" {
		raw, err := os.ReadFile(filePath)
		if err == nil {
			var loaded []ScheduleEntry
			if jsonErr := json.Unmarshal(raw, &loaded); jsonErr == nil {
				s.entries = loaded
			}
		}
	}
	return s
}

// GetAll returns a snapshot copy of all schedule entries.
func (s *Store) GetAll() []ScheduleEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ScheduleEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Upsert inserts or replaces the entry with the same ID, then persists.
func (s *Store) Upsert(e ScheduleEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ex := range s.entries {
		if ex.ID == e.ID {
			s.entries[i] = e
			return s.persist()
		}
	}
	s.entries = append(s.entries, e)
	return s.persist()
}

// persist writes entries to disk; a no-op when filePath is empty.
func (s *Store) persist() error {
	if s.filePath == "" {
		return nil
	}
	raw, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, raw, 0o644)
}

// Runner checks schedule entries on a tick and calls triggerFn when due.
type Runner struct {
	store     *Store
	triggerFn func(ctx context.Context, id string)
}

// NewRunner creates a Runner backed by the given Store.
func NewRunner(store *Store, triggerFn func(ctx context.Context, id string)) *Runner {
	return &Runner{store: store, triggerFn: triggerFn}
}

// Run blocks until ctx is cancelled, checking every minute.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			r.check(ctx, t)
		}
	}
}

func (r *Runner) check(ctx context.Context, now time.Time) {
	entries := r.store.GetAll()
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		if isDue(e, now) {
			slog.Info("scheduler: triggering", "id", e.ID)
			e.LastRun = now
			if err := r.store.Upsert(e); err != nil {
				slog.Warn("scheduler: failed to update last_run", "id", e.ID, "err", err)
			}
			go r.triggerFn(ctx, e.ID)
		}
	}
}

// isDue reports whether entry e should be triggered at time t.
func isDue(e ScheduleEntry, t time.Time) bool {
	switch e.CronExpr {
	case "@hourly":
		return t.Minute() == 0
	case "@daily":
		return t.Hour() == 0 && t.Minute() == 0
	case "@weekly":
		return t.Weekday() == time.Sunday && t.Hour() == 0 && t.Minute() == 0
	default:
		// Simple "HH:MM" format.
		if len(e.CronExpr) == 5 && e.CronExpr[2] == ':' {
			h, err1 := strconv.Atoi(e.CronExpr[:2])
			m, err2 := strconv.Atoi(e.CronExpr[3:])
			if err1 == nil && err2 == nil {
				return t.Hour() == h && t.Minute() == m
			}
		}
		return false
	}
}
