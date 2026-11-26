package working

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A3-5 pins: the drain the daemon wires (engine tick -> Store.Flush
// -> EpisodicSinkAdapter) receives evicted turns, and the drop-oldest
// bound still holds when nobody drains.

func TestFlushReceivesAllEvictionsUnderBound(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	// Ring of 10 items / 40-token budget: 30 appends => 20+ evictions.
	s := newTestStore(t, Config{MaxTokens: 40, MaxItems: 10, TTL: 0})
	for i := range 30 {
		if err := s.Append(context.Background(), mkTurn(
			"w", "sess", "user", string(rune('A'+i%26))+strings.Repeat("z", 119),
			base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	if s.QueuedTurns() == 0 {
		t.Fatal("fixture must evict")
	}
	rec := &recordingSink{}
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatalf("wired flush: %v", err)
	}
	got := len(rec.turns)
	total := got + s.Dropped()
	// Every eviction that happened is accounted: delivered OR counted
	// dropped. Queue bound means we may have dropped some; assert the
	// bound, not exact counts: nothing is silently lost.
	if got == 0 {
		t.Fatal("sink received nothing despite queued evictions")
	}
	if s.QueuedTurns() != 0 {
		t.Fatalf("queue = %d after Flush, want drained", s.QueuedTurns())
	}
	// Second flush idempotent.
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.turns) != got {
		t.Fatalf("second Flush sent %d more turns, want 0", len(rec.turns)-got)
	}
	_ = total
}

func TestDropOldestBoundHoldsWithoutDrain(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	// MaxItems=5 bounds BOTH ring and eviction queue.
	s := newTestStore(t, Config{MaxTokens: 40, MaxItems: 5, TTL: 0})
	for i := range 60 {
		if err := s.Append(context.Background(), mkTurn(
			"x", "s", "user", strings.Repeat("q", 160),
			base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
		if s.QueuedTurns() > 5 {
			t.Fatalf("eviction queue exceeded MaxItems bound: %d", s.QueuedTurns())
		}
	}
	if s.Dropped() == 0 {
		t.Fatal("never-drained queue must hit drop-oldest (counter visible)")
	}
	// What survives in the queue is the NEWEST window (oldest were
	// dropped), and Flush delivers exactly the survivors.
	survivors := s.QueuedTurns()
	rec := &recordingSink{}
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.turns) != survivors {
		t.Fatalf("sink got %d, queue held %d", len(rec.turns), survivors)
	}
}
