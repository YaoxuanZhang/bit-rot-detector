// Package retry provides a bounded in-memory queue of failed operations
// together with exponential-backoff retry scheduling.
package retry

import (
	"fmt"
	"sync"
	"time"
)

// Item represents a single retryable operation.
type Item struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Op       string    `json:"op"`      // "sync" or "scrub"
	Err      string    `json:"err"`
	Attempts int       `json:"attempts"`
	NextAt   time.Time `json:"next_at"`
	Done     bool      `json:"done"`
}

// Queue is a bounded, concurrency-safe retry queue.
type Queue struct {
	mu      sync.Mutex
	items   []*Item
	max     int
	counter int64
}

// New creates a Queue with a maximum capacity of max items.
func New(max int) *Queue {
	return &Queue{max: max}
}

// Add enqueues a new retry item (capped at max).  Silently drops the item
// when the queue is already full.
func (q *Queue) Add(path, op, errMsg string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= q.max {
		return
	}
	q.counter++
	id := fmt.Sprintf("%d", q.counter)
	q.items = append(q.items, &Item{
		ID:     id,
		Path:   path,
		Op:     op,
		Err:    errMsg,
		NextAt: time.Now().Add(time.Minute),
	})
}

// All returns a snapshot of all items (done and pending).
func (q *Queue) All() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.items))
	for i, it := range q.items {
		out[i] = *it
	}
	return out
}

// Due returns pointers to items whose NextAt is before now and Done is false.
func (q *Queue) Due() []*Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	var out []*Item
	for _, it := range q.items {
		if !it.Done && it.NextAt.Before(now) {
			out = append(out, it)
		}
	}
	return out
}

// MarkDone marks the item with the given id as done.
func (q *Queue) MarkDone(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.ID == id {
			it.Done = true
			return
		}
	}
}

// MarkFailed increments the attempt counter and reschedules with exponential
// backoff (2^attempts minutes, capped at 24 h).
func (q *Queue) MarkFailed(id, errMsg string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.ID == id {
			it.Attempts++
			it.Err = errMsg
			// Cap the shift to avoid overflow on large attempt counts.
			shift := it.Attempts
			if shift > 20 {
				shift = 20
			}
			delay := time.Duration(1<<uint(shift)) * time.Minute
			if delay > 24*time.Hour {
				delay = 24 * time.Hour
			}
			it.NextAt = time.Now().Add(delay)
			return
		}
	}
}
