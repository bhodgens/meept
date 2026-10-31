package activity

import (
	"sync"
	"testing"
	"time"
)

func TestRing_AppendAndTail(t *testing.T) {
	r := NewRing(3)

	// Append 5 records
	for i := 0; i < 5; i++ {
		r.Append(ActivityRecord{
			Kind:    "test",
			At:      time.Now().Add(time.Duration(i) * time.Second),
			Summary: "record",
		})
	}

	// Tail(10) should return 3 newest records (newest-first)
	tail, dropped := r.Tail(10)
	if len(tail) != 3 {
		t.Errorf("expected 3 records, got %d", len(tail))
	}
	if dropped != 2 {
		t.Errorf("expected dropped=2, got %d", dropped)
	}

	// Verify newest-first order (last appended should be first)
	if tail[0].At.Unix() != time.Now().Add(4*time.Second).Unix() {
		t.Errorf("first record should be the newest (index 4)")
	}
	if tail[2].At.Unix() != time.Now().Add(2*time.Second).Unix() {
		t.Errorf("last record should be the oldest of the 3 (index 2)")
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
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.Append(ActivityRecord{
					Kind:    "concurrent",
					AgentID: "agent",
					At:      time.Now(),
					Summary: "test",
				})
			}
		}(g)
	}

	// 5 goroutines reading
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.Tail(50)
			}
		}()
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
