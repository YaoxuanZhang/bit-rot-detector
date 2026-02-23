package scheduler

import (
"context"
"sync/atomic"
"testing"
"time"

"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
)

// newTestStore returns an in-memory config.Store with the given schedule entries.
func newTestStore(entries ...ScheduleEntry) *config.Store {
cfg := config.DefaultConfig()
cfg.Schedule = entries
return config.NewStore(cfg, "")
}

// TestIsDue_BuiltinExpressions exercises all branches of the unexported isDue
// function via direct calls (white-box, same package).
func TestIsDue_BuiltinExpressions(t *testing.T) {
// @hourly fires when Minute == 0.
at00 := time.Date(2024, 1, 1, 5, 0, 0, 0, time.UTC)
at30 := time.Date(2024, 1, 1, 5, 30, 0, 0, time.UTC)

if !isDue(ScheduleEntry{CronExpr: "@hourly"}, at00) {
t.Error("@hourly should be due at HH:00")
}
if isDue(ScheduleEntry{CronExpr: "@hourly"}, at30) {
t.Error("@hourly should NOT be due at HH:30")
}

// @daily fires when Hour == 0 && Minute == 0.
midnight := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
noon := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

if !isDue(ScheduleEntry{CronExpr: "@daily"}, midnight) {
t.Error("@daily should be due at midnight")
}
if isDue(ScheduleEntry{CronExpr: "@daily"}, noon) {
t.Error("@daily should NOT be due at noon")
}

// @weekly fires on Sunday midnight.
sunday := time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC) // Jan 7 2024 is Sunday
monday := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)

if sunday.Weekday() != time.Sunday {
t.Fatalf("test assumption wrong: expected Sunday, got %v", sunday.Weekday())
}
if !isDue(ScheduleEntry{CronExpr: "@weekly"}, sunday) {
t.Error("@weekly should be due on Sunday midnight")
}
if isDue(ScheduleEntry{CronExpr: "@weekly"}, monday) {
t.Error("@weekly should NOT be due on Monday midnight")
}

// HH:MM format.
t14h30 := time.Date(2024, 1, 1, 14, 30, 0, 0, time.UTC)
t14h31 := time.Date(2024, 1, 1, 14, 31, 0, 0, time.UTC)
if !isDue(ScheduleEntry{CronExpr: "14:30"}, t14h30) {
t.Error("14:30 should be due at 14:30")
}
if isDue(ScheduleEntry{CronExpr: "14:30"}, t14h31) {
t.Error("14:30 should NOT be due at 14:31")
}

// Invalid / unrecognised expression returns false.
if isDue(ScheduleEntry{CronExpr: "not-valid"}, t14h30) {
t.Error("invalid cron expr should never be due")
}
if isDue(ScheduleEntry{CronExpr: "25:99"}, t14h30) {
t.Error("out-of-range HH:MM should never be due")
}
if isDue(ScheduleEntry{CronExpr: ""}, t14h30) {
t.Error("empty cron expr should never be due")
}
}

// TestRunner_Check_FiresEnabledEntry verifies that Runner.check triggers the
// callback for an enabled entry whose cron expression matches the given time.
func TestRunner_Check_FiresEnabledEntry(t *testing.T) {
store := newTestStore(ScheduleEntry{
ID:       "test-entry",
Enabled:  true,
CronExpr: "@hourly",
})

var triggered int32
r := NewRunner(store, func(_ context.Context, id string) {
if id == "test-entry" {
atomic.AddInt32(&triggered, 1)
}
})

// Call check with a time where Minute == 0 (triggers @hourly).
at00 := time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC)
r.check(context.Background(), at00)

// Give the goroutine time to execute.
time.Sleep(20 * time.Millisecond)
if atomic.LoadInt32(&triggered) == 0 {
t.Error("expected trigger callback to be called for @hourly entry at HH:00")
}
}

// TestRunner_Check_SkipsDisabledEntry verifies that disabled entries are not fired.
func TestRunner_Check_SkipsDisabledEntry(t *testing.T) {
store := newTestStore(ScheduleEntry{
ID:       "disabled",
Enabled:  false,
CronExpr: "@hourly",
})

var triggered int32
r := NewRunner(store, func(_ context.Context, _ string) {
atomic.AddInt32(&triggered, 1)
})

at00 := time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC)
r.check(context.Background(), at00)
time.Sleep(10 * time.Millisecond)

if atomic.LoadInt32(&triggered) != 0 {
t.Error("disabled entry should not be triggered")
}
}

// TestRunner_Check_SkipsNonDueEntry verifies that an entry not matching the
// current time is not triggered.
func TestRunner_Check_SkipsNonDueEntry(t *testing.T) {
store := newTestStore(ScheduleEntry{
ID:       "not-due",
Enabled:  true,
CronExpr: "@daily", // only fires at midnight
})

var triggered int32
r := NewRunner(store, func(_ context.Context, _ string) {
atomic.AddInt32(&triggered, 1)
})

// Noon — not due for @daily.
noon := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
r.check(context.Background(), noon)
time.Sleep(10 * time.Millisecond)

if atomic.LoadInt32(&triggered) != 0 {
t.Error("@daily entry should not be triggered at noon")
}
}
