package activity

import (
	"sync"
)

// Ring is a fixed-capacity, append-only ring buffer for ActivityRecord.
// It implements a bounded history buffer where the oldest records are
// evicted when capacity is exceeded. Append and Tail are mutex-protected
// and safe for concurrent use.
type Ring struct {
	mu       sync.Mutex
	capacity int
	buf      []ActivityRecord
	head     int // index of the oldest element
	size     int // current number of elements
	dropped  int // total count of evicted/overflowed events
}

// NewRing creates a new Ring with the given capacity.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 1
	}
	return &Ring{
		capacity: capacity,
		buf:      make([]ActivityRecord, capacity),
	}
}

// Append adds a record to the ring. If the ring is full, the oldest
// record is evicted and the dropped counter is incremented.
func (r *Ring) Append(rec ActivityRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size < r.capacity {
		// Ring not full: append at the end
		idx := (r.head + r.size) % r.capacity
		r.buf[idx] = rec
		r.size++
	} else {
		// Ring full: overwrite the oldest (head) and advance head
		r.buf[r.head] = rec
		r.head = (r.head + 1) % r.capacity
		r.dropped++
	}
}

// Tail returns the newest n records in newest-last order (chronological
// ASCENDING: oldest of the returned slice is first, newest is last),
// along with the total dropped counter. If n >= size, all records are returned.
func (r *Ring) Tail(n int) ([]ActivityRecord, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n <= 0 {
		return []ActivityRecord{}, r.dropped
	}

	// Collect up to n newest records in chronological order (oldest first)
	result := make([]ActivityRecord, 0)
	for i := 0; i < r.size && len(result) < n; i++ {
		idx := (r.head + i) % r.capacity
		result = append(result, r.buf[idx])
	}

	return result, r.dropped
}
