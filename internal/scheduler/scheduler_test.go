package scheduler_test

import (
"context"
"sync/atomic"
"testing"
"time"

"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
"github.com/YaoxuanZhang/bit-rot-detector/internal/scheduler"
)

// newStore creates an in-memory config.Store with the given schedule entries.
func newStore(entries ...scheduler.ScheduleEntry) *config.Store {
cfg := config.DefaultConfig()
cfg.Schedule = entries
return config.NewStore(cfg, "")
}

func TestStore_NewEmpty(t *testing.T) {
store := newStore()
if entries := store.Get().Schedule; len(entries) != 0 {
t.Errorf("expected empty schedule, got %d entries", len(entries))
}
}

func TestStore_UpsertAndGetAll(t *testing.T) {
store := newStore()
entry := scheduler.ScheduleEntry{
ID:       "sync",
Label:    "Daily Sync",
Enabled:  true,
CronExpr: "@daily",
}
if err := store.Update(func(c *config.Config) error {
c.Schedule = append(c.Schedule, entry)
return nil
}); err != nil {
t.Fatalf("Update: %v", err)
}
all := store.Get().Schedule
if len(all) != 1 {
t.Fatalf("expected 1 entry, got %d", len(all))
}
if all[0].ID != "sync" {
t.Errorf("expected ID=sync, got %q", all[0].ID)
}
}

func TestStore_Upsert_ReplaceExisting(t *testing.T) {
store := newStore(
scheduler.ScheduleEntry{ID: "sync", Label: "v1", Enabled: true, CronExpr: "@daily"},
)
if err := store.Update(func(c *config.Config) error {
for i, e := range c.Schedule {
if e.ID == "sync" {
c.Schedule[i] = scheduler.ScheduleEntry{ID: "sync", Label: "v2", Enabled: false, CronExpr: "@weekly"}
return nil
}
}
c.Schedule = append(c.Schedule, scheduler.ScheduleEntry{ID: "sync", Label: "v2", Enabled: false, CronExpr: "@weekly"})
return nil
}); err != nil {
t.Fatal(err)
}
all := store.Get().Schedule
if len(all) != 1 {
t.Fatalf("expected 1 entry after upsert, got %d", len(all))
}
if all[0].Label != "v2" {
t.Errorf("expected Label=v2 after upsert, got %q", all[0].Label)
}
if all[0].Enabled {
t.Error("expected Enabled=false after upsert")
}
}

func TestStore_MultipleEntries(t *testing.T) {
store := newStore(
scheduler.ScheduleEntry{ID: "sync", Label: "Sync", CronExpr: "@daily"},
scheduler.ScheduleEntry{ID: "scrub", Label: "Scrub", CronExpr: "@weekly"},
)
if n := len(store.Get().Schedule); n != 2 {
t.Errorf("expected 2 entries, got %d", n)
}
}

func TestStore_GetAll_ReturnsCopy(t *testing.T) {
store := newStore(scheduler.ScheduleEntry{ID: "sync", Enabled: true, CronExpr: "@daily"})

copy1 := store.Get().Schedule
copy1[0].Enabled = false

// Mutating the copy should not affect the store.
copy2 := store.Get().Schedule
if !copy2[0].Enabled {
t.Error("Get() should return an independent copy, not a shared reference")
}
}

func TestRunner_ContextCancel(t *testing.T) {
store := newStore()
var triggered int32
r := scheduler.NewRunner(store, func(_ context.Context, id string) {
atomic.AddInt32(&triggered, 1)
})

ctx, cancel := context.WithCancel(context.Background())
done := make(chan struct{})
go func() {
defer close(done)
r.Run(ctx)
}()

cancel()
select {
case <-done:
case <-time.After(2 * time.Second):
t.Fatal("Runner.Run did not stop after context cancel")
}
if atomic.LoadInt32(&triggered) != 0 {
t.Errorf("expected 0 triggers, got %d", triggered)
}
}

func TestIsDue_HHMMExpr(t *testing.T) {
store := newStore()
now := time.Now()
expr := now.Format("15:04")
if err := store.Update(func(c *config.Config) error {
c.Schedule = []scheduler.ScheduleEntry{{
ID:       "hhmm-test",
Enabled:  true,
CronExpr: expr,
}}
return nil
}); err != nil {
t.Fatal(err)
}
all := store.Get().Schedule
if len(all) == 0 || all[0].CronExpr != expr {
t.Errorf("expected CronExpr=%q, got %q", expr, all[0].CronExpr)
}
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
defer cancel()
r := scheduler.NewRunner(store, func(_ context.Context, _ string) {})
r.Run(ctx) // exits fast
}
