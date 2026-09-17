package agent

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock gives tests deterministic control over the registry's clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// registryGet is a test-only snapshot reader over the guarded map.
func registryGet(t *testing.T, reg *TurnRegistry, turnID string) (SubmittedTurnRecord, bool) {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	rec, ok := reg.turns[turnID]
	if !ok {
		return SubmittedTurnRecord{}, false
	}
	return *rec, true
}

func TestTurnRegistry_RegisterNewTurn(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	if existing := reg.Register("turn-1", "conv-1"); existing {
		t.Fatal("first Register on a fresh turnID returned existing=true")
	}
	rec, ok := registryGet(t, reg, "turn-1")
	if !ok {
		t.Fatal("registered turn not found")
	}
	if rec.TurnID != "turn-1" || rec.ConversationID != "conv-1" {
		t.Errorf("record = %+v", rec)
	}
	if !rec.SubmittedAt.Equal(start) || !rec.LastProgressAt.Equal(start) {
		t.Errorf("timestamps = %v / %v, want %v", rec.SubmittedAt, rec.LastProgressAt, start)
	}
}

func TestTurnRegistry_RegisterIdempotent(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	reg.Register("turn-1", "conv-original")
	fc.Advance(5 * time.Minute)

	// A retry with the SAME explicit turn_id must be a no-op: existing=true,
	// original record (conversation + timestamps) preserved, NOT overwritten.
	if existing := reg.Register("turn-1", "conv-retry"); !existing {
		t.Fatal("second Register on the same turnID returned existing=false")
	}
	rec, ok := registryGet(t, reg, "turn-1")
	if !ok {
		t.Fatal("registered turn not found")
	}
	if rec.ConversationID != "conv-original" {
		t.Errorf("conversation = %q, want conv-original (idempotent, no overwrite)", rec.ConversationID)
	}
	if !rec.SubmittedAt.Equal(start) {
		t.Errorf("submitted_at = %v, want %v (no overwrite)", rec.SubmittedAt, start)
	}
	if !rec.LastProgressAt.Equal(start) {
		t.Errorf("last_progress_at = %v, want %v (no overwrite)", rec.LastProgressAt, start)
	}
}

func TestTurnRegistry_AttachTask(t *testing.T) {
	reg := NewTurnRegistry()
	reg.Register("turn-1", "conv-1")

	reg.AttachTask("turn-1", "task-42")
	rec, ok := registryGet(t, reg, "turn-1")
	if !ok {
		t.Fatal("registered turn not found")
	}
	if rec.TaskID != "task-42" {
		t.Errorf("task_id = %q, want task-42", rec.TaskID)
	}

	// Unknown turn and empty task are no-ops (must not panic).
	reg.AttachTask("turn-missing", "task-x")
	reg.AttachTask("turn-1", "")
}

func TestTurnRegistry_TouchUpdatesLastProgress(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	reg.Register("turn-1", "conv-1")
	fc.Advance(2 * time.Minute)
	reg.Touch("turn-1")

	rec, ok := registryGet(t, reg, "turn-1")
	if !ok {
		t.Fatal("registered turn not found")
	}
	if want := start.Add(2 * time.Minute); !rec.LastProgressAt.Equal(want) {
		t.Errorf("last_progress_at = %v, want %v", rec.LastProgressAt, want)
	}
	// Touch must not move SubmittedAt.
	if !rec.SubmittedAt.Equal(start) {
		t.Errorf("submitted_at = %v, want %v (Touch moves progress only)", rec.SubmittedAt, start)
	}

	// Unknown turn: no-op, no panic.
	reg.Touch("turn-missing")
}

func TestTurnRegistry_CompleteRemoves(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	reg.Register("turn-1", "conv-1")
	fc.Advance(1 * time.Minute)
	reg.Complete("turn-1")

	if _, ok := registryGet(t, reg, "turn-1"); ok {
		t.Fatal("completed turn still tracked")
	}
	// Complete on unknown turn: no-op, no panic.
	reg.Complete("turn-missing")
}

func TestTurnRegistry_StaleFiltersByLastProgress(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	reg.Register("turn-early", "conv-early") // submitted at T
	fc.Advance(1 * time.Minute)
	reg.Register("turn-late", "conv-late") // submitted at T+1m
	fc.Advance(1 * time.Minute)
	reg.Register("turn-fresh", "conv-fresh") // submitted at T+2m
	reg.Touch("turn-fresh")                  // progress at T+2m

	// At T+2m: turn-early last progress T (2m old), turn-late T+1m (1m old),
	// turn-fresh T+2m (0m old).
	stale := reg.Stale(90 * time.Second)
	if len(stale) != 1 {
		t.Fatalf("Stale(90s) returned %d records (%+v), want 1", len(stale), stale)
	}
	if stale[0].TurnID != "turn-early" {
		t.Errorf("stale turn = %q, want turn-early", stale[0].TurnID)
	}

	// Wider window catches two, oldest first.
	stale = reg.Stale(30 * time.Second)
	if len(stale) != 2 {
		t.Fatalf("Stale(30s) returned %d records, want 2", len(stale))
	}
	if stale[0].TurnID != "turn-early" || stale[1].TurnID != "turn-late" {
		t.Errorf("order = [%s, %s], want [turn-early, turn-late] (oldest first)", stale[0].TurnID, stale[1].TurnID)
	}

	// Nothing older than the whole lifetime.
	if got := reg.Stale(24 * time.Hour); len(got) != 0 {
		t.Errorf("Stale(24h) returned %d records, want 0", len(got))
	}

	// Stale must not remove records — the reaper owns Complete.
	if _, ok := registryGet(t, reg, "turn-early"); !ok {
		t.Error("Stale must not remove tracked turns")
	}
}

// TestTurnRegistry_Concurrent16Goroutines hammers Register/Touch/Complete/
// Stale/AttachTask from 16 goroutines against a SHARED turn id; run under
// -race to prove the mutex guards every map access.
func TestTurnRegistry_Concurrent16Goroutines(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	const goroutines = 16
	const iterations = 100
	const sharedTurn = "turn-shared"

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				turnID := fmt.Sprintf("%s-%d-%d", sharedTurn, g, i%3)
				reg.Register(turnID, "conv-shared")
				reg.Touch(turnID)
				reg.AttachTask(turnID, "task-shared")
				_ = reg.Stale(time.Minute)
				reg.Complete(turnID)
			}
		}(g)
	}
	wg.Wait()
}
