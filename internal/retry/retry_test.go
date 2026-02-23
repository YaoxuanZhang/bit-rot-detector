package retry_test

import (
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/retry"
)

func TestNew_EmptyQueue(t *testing.T) {
	q := retry.New(10)
	if items := q.All(); len(items) != 0 {
		t.Errorf("expected empty queue, got %d items", len(items))
	}
}

func TestAdd_SingleItem(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "permission denied")

	items := q.All()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Path != "/data" {
		t.Errorf("expected path=/data, got %q", items[0].Path)
	}
	if items[0].Op != "sync" {
		t.Errorf("expected op=sync, got %q", items[0].Op)
	}
	if items[0].Err != "permission denied" {
		t.Errorf("expected err string, got %q", items[0].Err)
	}
	if items[0].Done {
		t.Error("expected Done=false on new item")
	}
}

func TestAdd_CapAtMax(t *testing.T) {
	q := retry.New(3)
	q.Add("/a", "sync", "e")
	q.Add("/b", "sync", "e")
	q.Add("/c", "sync", "e")
	q.Add("/d", "sync", "e") // should be silently dropped

	if n := len(q.All()); n != 3 {
		t.Errorf("expected 3 items (capped), got %d", n)
	}
}

func TestMarkDone(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "scrub", "io error")

	id := q.All()[0].ID
	q.MarkDone(id)

	if !q.All()[0].Done {
		t.Error("expected Done=true after MarkDone")
	}
}

func TestMarkDone_UnknownID(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "err")
	// Marking an unknown ID should not panic or corrupt state.
	q.MarkDone("nonexistent-id")
	if q.All()[0].Done {
		t.Error("unexpected Done=true after MarkDone with nonexistent ID")
	}
}

func TestMarkFailed_BackoffIncreases(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "io error")

	id := q.All()[0].ID
	before := time.Now()

	q.MarkFailed(id, "still failing")
	item := q.All()[0]
	if item.Attempts != 1 {
		t.Errorf("expected Attempts=1, got %d", item.Attempts)
	}
	if item.Err != "still failing" {
		t.Errorf("expected updated err, got %q", item.Err)
	}
	// NextAt should be after now + 2 min (2^1) minus tiny clock slop.
	if item.NextAt.Before(before.Add(time.Minute)) {
		t.Errorf("expected NextAt at least 1min from now, got %v", item.NextAt)
	}
}

func TestMarkFailed_CapAt24h(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "err")
	id := q.All()[0].ID

	// 25 failures should still be capped at 24h.
	for i := 0; i < 25; i++ {
		q.MarkFailed(id, "err")
	}
	item := q.All()[0]
	maxNextAt := time.Now().Add(25 * time.Hour)
	if item.NextAt.After(maxNextAt) {
		t.Errorf("expected NextAt capped at ~24h, got %v ahead", item.NextAt.Sub(time.Now()))
	}
}

func TestDue_ReturnsItemsPastDeadline(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "err")

	// Items added with NextAt = now+1min are not yet due.
	if due := q.Due(); len(due) != 0 {
		t.Errorf("expected 0 due items immediately, got %d", len(due))
	}
}

func TestDue_ExcludesDoneItems(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "err")
	id := q.All()[0].ID
	q.MarkDone(id)

	// Even if the item were past NextAt, Done items are excluded.
	if due := q.Due(); len(due) != 0 {
		t.Errorf("expected 0 due items when done, got %d", len(due))
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	q := retry.New(10)
	q.Add("/data", "sync", "err")
	snapshot1 := q.All()
	q.MarkDone(snapshot1[0].ID)
	// Original snapshot should not reflect mutation.
	if snapshot1[0].Done {
		t.Error("All() should return value copies, not pointers")
	}
}
