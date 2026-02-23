// Package scheduler provides a background runner that fires a trigger function
// when a schedule entry (stored in the config) comes due.
package scheduler

import (
"context"
"log/slog"
"strconv"
"time"

"github.com/YaoxuanZhang/bit-rot-detector/internal/config"
)

// ScheduleEntry is an alias for config.ScheduleEntry for convenience.
type ScheduleEntry = config.ScheduleEntry

// Runner checks config schedule entries on a tick and calls triggerFn when due.
type Runner struct {
store     *config.Store
triggerFn func(ctx context.Context, id string)
}

// NewRunner creates a Runner backed by the given config.Store.
func NewRunner(store *config.Store, triggerFn func(ctx context.Context, id string)) *Runner {
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
cfg := r.store.Get()
for _, e := range cfg.Schedule {
if !e.Enabled {
continue
}
if isDue(e, now) {
slog.Info("scheduler: triggering", "id", e.ID)
id := e.ID
_ = r.store.Update(func(c *config.Config) error {
for i, se := range c.Schedule {
if se.ID == id {
c.Schedule[i].LastRun = now
break
}
}
return nil
})
go r.triggerFn(ctx, id)
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
