package session

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// altRecordingHook records BOTH hook shapes: the plain primary-id call and
// the richer call carrying the pre-delete-captured alternate identity.
type altRecordingHook struct {
	mu       sync.Mutex
	primary  []string
	alt      map[string]string // sessionID -> alternateID
	events   []SessionEvent
	onCall   func(string, SessionEvent)
	altCalls int
}

func (h *altRecordingHook) OnSessionEvent(_ context.Context, sessionID string, event SessionEvent) {
	h.mu.Lock()
	h.primary = append(h.primary, sessionID)
	h.events = append(h.events, event)
	cb := h.onCall
	h.mu.Unlock()
	if cb != nil {
		cb(sessionID, event)
	}
}

func (h *altRecordingHook) OnSessionEventWithAlternate(_ context.Context, sessionID, alternateID string, event SessionEvent) {
	h.mu.Lock()
	if h.alt == nil {
		h.alt = make(map[string]string)
	}
	h.alt[sessionID] = alternateID
	h.events = append(h.events, event)
	h.altCalls++
	h.mu.Unlock()
}

// TestSQLiteDeleteCarriesConversationID pins the delete-path teardown
// identity fix: the store DELETEs the row BEFORE firing the lifecycle
// hook, so a hook that resolves the conversation id via a post-delete
// Get sees nothing. The hook event must instead carry the alternate
// identity captured under the store lock before deletion.
func TestSQLiteDeleteCarriesConversationID(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()
	hook := &altRecordingHook{}
	store.SetLifecycleHook(hook)

	sess, err := store.Create("delete-identity")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ConversationID == "" || sess.ConversationID == sess.ID {
		t.Fatalf("fixture invalid: ConversationID=%q ID=%q must be distinct", sess.ConversationID, sess.ID)
	}

	if !store.Delete(sess.ID) {
		t.Fatal("Delete returned false")
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()
	if hook.altCalls != 1 {
		t.Fatalf("alternate-carrying hook calls = %d, want 1 (primary-only calls: %v)", hook.altCalls, hook.primary)
	}
	if got := hook.alt[sess.ID]; got != sess.ConversationID {
		t.Fatalf("alternate identity = %q, want conversation id %q", got, sess.ConversationID)
	}
	if len(hook.events) != 1 || hook.events[0] != SessionEventDelete {
		t.Fatalf("events = %v, want exactly one delete event", hook.events)
	}
}

// TestMemoryStoreDeleteCarriesConversationID pins the same contract on the
// in-memory store (same Delete-then-fire shape).
func TestMemoryStoreDeleteCarriesConversationID(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	hook := &altRecordingHook{}
	store.SetLifecycleHook(hook)

	sess, err := store.Create("delete-identity-mem")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !store.Delete(sess.ID) {
		t.Fatal("Delete returned false")
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()
	if got := hook.alt[sess.ID]; got != sess.ConversationID {
		t.Fatalf("alternate identity = %q, want conversation id %q", got, sess.ConversationID)
	}
}

// TestPlainHookUnaffectedByAlternateCapture pins backward compatibility: a
// hook implementing ONLY SessionLifecycleHook still receives the plain
// primary-id call even though the store captured an alternate identity.
func TestPlainHookUnaffectedByAlternateCapture(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	hook := &recordingHook{}
	store.SetLifecycleHook(hook)

	sess, err := store.Create("plain-hook")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.Delete(sess.ID)

	got := hook.recorded()
	if len(got) != 1 || got[0].sessionID != sess.ID {
		t.Fatalf("plain hook calls = %v, want [{%s delete}]", got, sess.ID)
	}
}

// TestIdleReaperStartStopRaceSafe runs Start/Stop from concurrent goroutines
// under -race: r.started was written inside startOnce.Do and read
// unsynchronized by Stop (plain data race).
func TestIdleReaperStartStopRaceSafe(t *testing.T) {
	tracker := NewActivityTracker()
	r := NewIdleReaper(tracker, &recordingHook{}, slog.Default())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); r.Start() }()
		go func() { defer wg.Done(); r.Stop() }()
	}
	wg.Wait()
	r.Stop() // idempotent; waits for goroutine exit
}

// TestActivityTrackerEvictBoundsMap pins the eviction rule: deleted
// sessions are removed from the tracker, so its map does not grow
// monotonically with every session ever served.
func TestActivityTrackerEvictBoundsMap(t *testing.T) {
	tracker := NewActivityTracker()
	const n = 50
	for i := 0; i < n; i++ {
		tracker.RecordActivity("sess-"+string(rune('a'+i%26))+string(rune('0'+i)), "client")
	}
	if tracker.Len() != n {
		t.Fatalf("Len = %d, want %d", tracker.Len(), n)
	}
	for i := 0; i < n; i++ {
		tracker.Evict("sess-" + string(rune('a'+i%26)) + string(rune('0'+i)))
	}
	if tracker.Len() != 0 {
		t.Fatalf("Len after evicting all = %d, want 0", tracker.Len())
	}
}

// TestIdleReaperEvictsStaleIdleFired pins idleFired pruning: a session that
// fired idle and is then removed from the tracker (deleted) must not leave
// a permanent entry in idleFired.
func TestIdleReaperEvictsStaleIdleFired(t *testing.T) {
	tracker := NewActivityTracker()
	hook := &recordingHook{}
	r := NewIdleReaper(tracker, hook, slog.Default())
	r.idleCheckInterval = time.Hour // tick manually
	r.SetIdleRelease(time.Millisecond)

	tracker.RecordActivity("sess-live", "")
	tracker.RecordActivity("sess-gone", "")
	time.Sleep(5 * time.Millisecond)

	r.reaperTick(context.Background())

	r.mu.Lock()
	firedAfterFirst := len(r.idleFired)
	r.mu.Unlock()
	if firedAfterFirst != 2 {
		t.Fatalf("idleFired after first tick = %d, want 2", firedAfterFirst)
	}

	// Simulate deletion: the store evicts the session from the tracker.
	tracker.Evict("sess-gone")
	r.reaperTick(context.Background())

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, still := r.idleFired["sess-gone"]; still {
		t.Fatalf("idleFired still holds evicted session; map = %v", r.idleFired)
	}
	if len(r.idleFired) != 1 {
		t.Fatalf("idleFired size = %d, want 1 (live session only)", len(r.idleFired))
	}
}

// TestSetLifecycleHookConcurrentWithDelete exercises the atomic hook field:
// an external SetLifecycleHook write concurrent with store deletes must not
// race (previously an unsynchronized field write vs collectLocked reads).
func TestSetLifecycleHookConcurrentWithDelete(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			store.SetLifecycleHook(&altRecordingHook{})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s, err := store.Create("race")
			if err != nil {
				return
			}
			store.Delete(s.ID)
		}
	}()
	wg.Wait()
}
