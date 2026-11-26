package activity

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRing_AppendAndTail(t *testing.T) {
	r := NewRing(3)

	// Append 5 records with distinct timestamps
	base := time.Now()
	for i := range 5 {
		r.Append(ActivityRecord{
			Kind:    "test",
			At:      base.Add(time.Duration(i) * time.Second),
			Summary: "record",
		})
	}

	// Tail(10) should return 3 newest records in newest-last order
	// (chronological ASCENDING: oldest of the 3 first, newest last)
	tail, dropped := r.Tail(10)
	if len(tail) != 3 {
		t.Errorf("expected 3 records, got %d", len(tail))
	}
	if dropped != 2 {
		t.Errorf("expected dropped=2, got %d", dropped)
	}

	// Verify newest-last order (chronological ASCENDING)
	// The 3 records kept are indices 2,3,4 (times base+2s, base+3s, base+4s)
	// newest-last means: [base+2s, base+3s, base+4s]
	if tail[0].At.Unix() != base.Add(2*time.Second).Unix() {
		t.Errorf("first record should be the oldest of the 3 (index 2, base+2s), got %v", tail[0].At)
	}
	if tail[2].At.Unix() != base.Add(4*time.Second).Unix() {
		t.Errorf("last record should be the newest (index 4, base+4s), got %v", tail[2].At)
	}
}

func TestRing_TailZero(t *testing.T) {
	r := NewRing(3)
	r.Append(ActivityRecord{Summary: "a"})
	r.Append(ActivityRecord{Summary: "b"})

	tail, dropped := r.Tail(0)
	if len(tail) != 0 {
		t.Errorf("Tail(0) should return empty slice")
	}
	if dropped != 0 {
		t.Errorf("Tail(0) should return dropped=0")
	}
}

func TestRing_Concurrent(t *testing.T) {
	r := NewRing(100)
	var wg sync.WaitGroup

	// 10 goroutines appending
	for range 10 {
		wg.Go(func() {
			for range 100 {
				r.Append(ActivityRecord{
					Kind:    "concurrent",
					AgentID: "agent",
					At:      time.Now(),
					Summary: "test",
				})
			}
		})
	}

	// 5 goroutines reading
	for range 5 {
		wg.Go(func() {
			for range 100 {
				r.Tail(50)
			}
		})
	}

	wg.Wait()

	// Just verify it doesn't race
	t.Logf("concurrent test passed, dropped=%d", r.dropped)
}

func TestRing_CapacityOne(t *testing.T) {
	r := NewRing(1)
	r.Append(ActivityRecord{Summary: "first"})
	r.Append(ActivityRecord{Summary: "second"})
	r.Append(ActivityRecord{Summary: "third"})

	tail, dropped := r.Tail(10)
	if len(tail) != 1 {
		t.Errorf("expected 1 record, got %d", len(tail))
	}
	if tail[0].Summary != "third" {
		t.Errorf("only the last record should remain")
	}
	if dropped != 2 {
		t.Errorf("expected dropped=2, got %d", dropped)
	}
}

// TestRing_Tail_WrappedRing_NewestN pins bughunt H1/A1-1: with a WRAPPED
// ring and n < size, Tail must return the NEWEST n records in chronological
// (ascending) order — not the oldest n. The old implementation iterated
// from head (oldest) and stopped at n, so a full ring of capacity 8 with
// 20 appends and Tail(3) returned records 13,14,15 instead of 17,18,19.
func TestRing_Tail_WrappedRing_NewestN(t *testing.T) {
	r := NewRing(8)
	base := time.Now()
	for i := range 20 { // wraps twice; survivors are indices 12..19
		r.Append(ActivityRecord{
			Kind:    "test",
			At:      base.Add(time.Duration(i) * time.Second),
			Summary: fmt.Sprintf("rec-%d", i),
		})
	}

	tail, dropped := r.Tail(3)
	if len(tail) != 3 {
		t.Fatalf("expected 3 records, got %d", len(tail))
	}
	if dropped != 12 {
		t.Errorf("expected dropped=12, got %d", dropped)
	}
	// Newest 3 are 17,18,19, chronological ascending.
	want := []string{"rec-17", "rec-18", "rec-19"}
	for i, w := range want {
		if tail[i].Summary != w {
			t.Errorf("tail[%d] = %q, want %q (Tail must return the NEWEST n, oldest-of-window first)", i, tail[i].Summary, w)
		}
	}
	if !(tail[0].At.Before(tail[1].At) && tail[1].At.Before(tail[2].At)) {
		t.Errorf("order must be chronological ascending: %v, %v, %v", tail[0].At, tail[1].At, tail[2].At)
	}

	// n exactly at the boundary (n == size) still returns the full window.
	full, _ := r.Tail(8)
	if len(full) != 8 || full[0].Summary != "rec-12" || full[7].Summary != "rec-19" {
		t.Errorf("Tail(size) window wrong: len=%d first=%v last=%v", len(full), full[0].Summary, full[7].Summary)
	}
}
