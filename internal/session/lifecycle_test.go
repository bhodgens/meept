package session

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// --- test doubles ---

// recordingHook is a SessionLifecycleHook test double that records every
// event it receives. The error field (hookErr) is deliberately unused by
// OnSessionEvent — the interface is notification-only — but tests can flip
// panicOnCall to prove a panicking hook never breaks Archive/Delete callers.
type recordingHook struct {
	mu      sync.Mutex
	events  []lifecycleEventRecord
	onCall  func(sessionID string, event SessionEvent)
}

type lifecycleEventRecord struct {
	sessionID string
	event     SessionEvent
}

func (h *recordingHook) OnSessionEvent(_ context.Context, sessionID string, event SessionEvent) {
	h.mu.Lock()
	h.events = append(h.events, lifecycleEventRecord{sessionID: sessionID, event: event})
	h.mu.Unlock()
	if h.onCall != nil {
		h.onCall(sessionID, event)
	}
}

func (h *recordingHook) recorded() []lifecycleEventRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]lifecycleEventRecord(nil), h.events...)
}

// --- hook compile-time assertions ---

var _ SessionLifecycleHook = (*recordingHook)(nil)

// --- SetLifecycleHook typed-nil guard tests ---

func TestSetLifecycleHook_TypedNilGuard(t *testing.T) {
	mem := NewMemoryStore(slog.Default())
	// Plain nil and typed-nil (*recordingHook)(nil) must both be no-ops
	// (not panic, not store the typed-nil wrapper).
	mem.SetLifecycleHook(nil)
	mem.SetLifecycleHook((*recordingHook)(nil))

	sqlStore, _ := testHelper(t)
	defer sqlStore.Close()
	sqlStore.SetLifecycleHook(nil)
	sqlStore.SetLifecycleHook((*recordingHook)(nil))
}

// --- MemoryStore hook fire tests (leaf task 1/2: RED before lifecycle.go
// existed — SetLifecycleHook did not compile, which is the RED evidence). ---

func TestLifecycleHookFiresOnArchive_Memory(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	s, err := store.Create("hook-archive-mem")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Archive(s.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	events := hook.recorded()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event after Archive, got %d: %+v", len(events), events)
	}
	if events[0].sessionID != s.ID || events[0].event != SessionEventArchive {
		t.Errorf("event = (%s, %s), want (%s, %s)", events[0].sessionID, events[0].event, s.ID, SessionEventArchive)
	}

	// Unarchive is a lifecycle restore, not a teardown: no event.
	if err := store.Archive(s.ID, false); err != nil {
		t.Fatalf("un-Archive: %v", err)
	}
	if got := len(hook.recorded()); got != 1 {
		t.Errorf("unarchive must not fire the hook, got %d events", got)
	}
}

func TestLifecycleHookFiresOnDelete_Memory(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	s, err := store.Create("hook-delete-mem")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !store.Delete(s.ID) {
		t.Fatal("Delete returned false for existing session")
	}

	events := hook.recorded()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event after Delete, got %d: %+v", len(events), events)
	}
	if events[0].sessionID != s.ID || events[0].event != SessionEventDelete {
		t.Errorf("event = (%s, %s), want (%s, %s)", events[0].sessionID, events[0].event, s.ID, SessionEventDelete)
	}
}

func TestLifecycleHook_NoDeleteOnMissingSession_Memory(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	if store.Delete("does-not-exist") {
		t.Fatal("Delete returned true for missing session")
	}
	if err := store.Archive("does-not-exist", true); err == nil {
		t.Fatal("Archive on missing session should error")
	}
	if got := len(hook.recorded()); got != 0 {
		t.Errorf("failed teardown paths must not fire the hook, got %d events", got)
	}
}

// --- SQLiteStore hook fire tests ---

func TestLifecycleHookFiresOnArchive_SQLite(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	s, err := store.Create("hook-archive-sql")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Archive(s.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	events := hook.recorded()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event after Archive, got %d: %+v", len(events), events)
	}
	if events[0].sessionID != s.ID || events[0].event != SessionEventArchive {
		t.Errorf("event = (%s, %s), want (%s, %s)", events[0].sessionID, events[0].event, s.ID, SessionEventArchive)
	}
}

func TestLifecycleHookFiresOnDelete_SQLite(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	s, err := store.Create("hook-delete-sql")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !store.Delete(s.ID) {
		t.Fatal("Delete returned false for existing session")
	}

	events := hook.recorded()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event after Delete, got %d: %+v", len(events), events)
	}
	if events[0].sessionID != s.ID || events[0].event != SessionEventDelete {
		t.Errorf("event = (%s, %s), want (%s, %s)", events[0].sessionID, events[0].event, s.ID, SessionEventDelete)
	}
}

func TestLifecycleHook_NoFireOnNoOpPaths_SQLite(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	// Delete of a missing row: RowsAffected == 0 → not a teardown, no event.
	if store.Delete("does-not-exist") {
		t.Fatal("Delete returned true for missing session")
	}
	// Archive of a missing session errors: no event.
	if err := store.Archive("does-not-exist", true); err == nil {
		t.Fatal("Archive on missing session should error")
	}
	if got := len(hook.recorded()); got != 0 {
		t.Errorf("no-op paths must not fire the hook, got %d events", got)
	}
}

// --- hook failures never propagate (leaf task 1 assertion) ---

func TestLifecycleHook_PanickingHookDoesNotPropagate(t *testing.T) {
	hook := &recordingHook{onCall: func(string, SessionEvent) { panic("hook exploded") }}

	// MemoryStore
	mem := NewMemoryStore(slog.Default())
	mem.SetLifecycleHook(hook)
	s, err := mem.Create("hook-panic-mem")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mem.Archive(s.ID, true); err != nil {
		t.Fatalf("Archive must survive a panicking hook: %v", err)
	}
	if !mem.Delete(s.ID) {
		t.Fatal("Delete must survive a panicking hook")
	}

	// SQLiteStore
	sqlStore, _ := testHelper(t)
	defer sqlStore.Close()
	sqlStore.SetLifecycleHook(hook)
	s2, err := sqlStore.Create("hook-panic-sql")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sqlStore.Archive(s2.ID, true); err != nil {
		t.Fatalf("Archive must survive a panicking hook: %v", err)
	}
}

// --- lifecycle.go unit helpers ---

func TestInvokeLifecycleHook_Safety(t *testing.T) {
	if err := recover(); err != nil {
		t.Fatalf("unexpected panic: %v", err)
	}
	// nil interface: no-op
	invokeLifecycleHook(nil, context.Background(), "s1", SessionEventDelete)
	// typed-nil interface: no-op (the typed-nil trap)
	var typedNil *recordingHook
	invokeLifecycleHook(typedNil, context.Background(), "s1", SessionEventDelete)

	// panicking hook: swallowed
	boom := &recordingHook{onCall: func(string, SessionEvent) { panic("boom") }}
	invokeLifecycleHook(boom, context.Background(), "s1", SessionEventArchive)
}

// --- idle reaper tests (leaf task 3) ---

// TestReaperIdleOncePerEpisode reuses the existing ActivityTracker as the
// last-activity source, rewinds one session's LastActivity deterministically
// (the same coarse-monotonic-clock-safe trick activity_tracker_test.go uses),
// runs reaperTick, and asserts exactly ONE idle event. Then activity resumes
// (re-arm) and the session lapses again: exactly one more event.
func TestReaperIdleOncePerEpisode(t *testing.T) {
	tracker := NewActivityTracker()
	hook := &recordingHook{}

	r := NewIdleReaper(tracker, hook, slog.Default())
	r.SetIdleRelease(50 * time.Millisecond)
	r.idleCheckInterval = 5 * time.Millisecond
	r.Start()
	defer r.Stop()

	tracker.RecordActivity("sess-a", "client-1")

	// Make sess-a older than the idle window deterministically.
	tracker.mu.Lock()
	tracker.activity["sess-a"].LastActivity = tracker.activity["sess-a"].LastActivity.Add(-200 * time.Millisecond)
	tracker.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for len(hook.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	events := hook.recorded()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 idle event per episode, got %d: %+v", len(events), events)
	}
	if events[0].sessionID != "sess-a" || events[0].event != SessionEventIdle {
		t.Errorf("event = (%s, %s), want (sess-a, %s)", events[0].sessionID, events[0].event, SessionEventIdle)
	}

	// Wait one full reaper tick to prove no duplicate fires while still idle.
	time.Sleep(20 * time.Millisecond)
	if got := len(hook.recorded()); got != 1 {
		t.Fatalf("idle event must fire ONCE per episode, got %d after extra ticks", got)
	}

	// New activity re-arms the episode. The un-arm lands on the reaper's
	// next tick (activity must be OBSERVED fresh by a tick before the
	// session can re-lapse), so wait one interval after recording.
	tracker.RecordActivity("sess-a", "client-1")
	time.Sleep(20 * time.Millisecond)

	// Now let it lapse again (deterministic rewind past the window).
	tracker.mu.Lock()
	tracker.activity["sess-a"].LastActivity = tracker.activity["sess-a"].LastActivity.Add(-200 * time.Millisecond)
	tracker.mu.Unlock()

	deadline = time.Now().Add(2 * time.Second)
	for len(hook.recorded()) < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	events = hook.recorded()
	if len(events) != 2 {
		t.Fatalf("expected re-armed idle episode (2 total), got %d: %+v", len(events), events)
	}
	if events[1].sessionID != "sess-a" || events[1].event != SessionEventIdle {
		t.Errorf("re-armed event = (%s, %s), want (sess-a, %s)", events[1].sessionID, events[1].event, SessionEventIdle)
	}

	// Still idle after the second fire: no further events.
	time.Sleep(20 * time.Millisecond)
	if got := len(hook.recorded()); got != 2 {
		t.Errorf("idle event re-fired while still idle: %d total", got)
	}
}

// TestReaperZeroIdleReleaseDisables pins the leaf-02-threading contract:
// SetIdleRelease(0) (the zero value, also the "config absent" shape) disables
// reaping entirely — no ticker side effects, no events.
func TestReaperZeroIdleReleaseDisables(t *testing.T) {
	tracker := NewActivityTracker()
	hook := &recordingHook{}

	r := NewIdleReaper(tracker, hook, slog.Default())
	// Deliberately never SetIdleRelease: zero disables.
	r.idleCheckInterval = 5 * time.Millisecond
	tracker.RecordActivity("sess-a", "client-1")

	// Direct tick invocation: disabled → nothing fires, nothing marks armed.
	tracker.mu.Lock()
	tracker.activity["sess-a"].LastActivity = tracker.activity["sess-a"].LastActivity.Add(-time.Hour)
	tracker.mu.Unlock()
	r.reaperTick(context.Background())
	if got := len(hook.recorded()); got != 0 {
		t.Errorf("disabled reaper must not fire, got %d events", got)
	}
}

// TestReaperStartStopLifecycle pins the no-leaked-ticker requirement: Start
// after Start is a no-op, Stop without Start is safe, and Stop releases the
// goroutine (second Stop is a no-op).
func TestReaperStartStopLifecycle(t *testing.T) {
	tracker := NewActivityTracker()
	r := NewIdleReaper(tracker, &recordingHook{}, slog.Default())
	r.idleCheckInterval = 5 * time.Millisecond

	r.Start()
	r.Start() // second Start must not spawn a second goroutine
	r.SetIdleRelease(50 * time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	r.Stop()
	r.Stop() // idempotent

	// Post-Stop tick invocations would panic on a closed/stopped state if the
	// lifecycle were wrong; run one manually to make sure state stays sane.
	r.reaperTick(context.Background())

	// A fresh Start after full Stop is legal (daemon restart-in-tests shape).
	r.Start()
	r.Stop()
}
