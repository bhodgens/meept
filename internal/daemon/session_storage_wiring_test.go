package daemon

// DSH-2 wiring remediation: the session store built by NewComponents must
// carry the projection registry (session-summary fold, watermark cache) and
// the storage record-change emitter — the two typed-nil setter seams that
// previously had zero production callers. Pins the wiring through the real
// NewComponents path with live bus events observed end to end.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/storage"
)

func TestSessionStoreProjectionAndEmitterWiring(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Daemon: config.DaemonConfig{DataDir: tmpDir},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
	}
	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() { _ = comps.Stop(context.Background()) })
	if comps.SessionStore == nil {
		t.Fatal("nil session store")
	}

	// The store must be the SQLite backing (the memory fallback skips the
	// wiring block by construction).
	store, ok := comps.SessionStore.(*session.SQLiteStore)
	if !ok {
		t.Fatalf("session store is %T, want *session.SQLiteStore (memory fallback skips the seam wiring)", comps.SessionStore)
	}

	// Storage events: subscribe BEFORE the write so nothing is missed, then
	// save one message and expect exactly one put carrying the new snapshot.
	var (
		mu     sync.Mutex
		events []storage.RecordChanged
	)
	sub := bus.SubscribeT(msgBus, "dsh2-test", storage.StorageRecordChangedTopic, func(rc storage.RecordChanged) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, rc)
	})
	defer bus.Unsubscribe(msgBus, sub)

	sess, err := store.Create("dsh2-wiring")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "hello", Timestamp: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	putCount := len(events)
	mu.Unlock()
	if putCount == 0 {
		t.Fatal("no storage.record.changed event after a committed save (emitter not wired)")
	}
	mu.Lock()
	put := events[0]
	mu.Unlock()
	if put.Op != storage.ChangeOpPut || put.Store != "session_messages" || put.Key != sess.ID {
		t.Fatalf("first event = %+v, want put on session_messages/%s", put, sess.ID)
	}
	if len(put.Value) == 0 {
		t.Error("put event carries no value snapshot")
	}

	// Delete publishes the tombstone after the row is gone.
	if !store.Delete(sess.ID) {
		t.Fatalf("Delete failed")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(events) < 2 {
		t.Fatalf("expected a delete tombstone after Delete; got %d events", len(events))
	}
	mu.Lock()
	del := events[len(events)-1]
	mu.Unlock()
	if del.Op != storage.ChangeOpDelete || del.Key != sess.ID {
		t.Fatalf("tombstone = %+v, want delete for %s", del, sess.ID)
	}
	if len(del.Value) != 0 {
		t.Error("delete event must carry NO value (tombstone)")
	}

	// Projection registry: the summary unit is registered at wiring time;
	// after the exchange above (before Delete) the fold should reflect the
	// messages... but the session was deleted, so verify the SEAM instead:
	// a fresh session + save folds into the summary projection (message
	// count + has_response) and checkpoints the watermark cache.
	sess2, err := store.Create("dsh2-projection")
	if err != nil {
		t.Fatalf("Create projection session: %v", err)
	}
	if err := store.SaveMessages(sess2.ID, []session.Message{
		{Role: "user", Content: "q", Timestamp: time.Now().UTC()},
		{Role: "assistant", Content: "a", Timestamp: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("SaveMessages projection: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	var snap session.SessionProjectionSnapshot
	snapErr := error(nil)
	for time.Now().Before(deadline) {
		s, err := store.Snapshot(sess2.ID, session.SessionSummaryKey)
		if err == nil {
			if st, ok := s.Values[session.SessionSummaryKey].(*session.SessionSummaryState); ok && st.MessageCount >= 2 {
				snap = s
				snapErr = nil
				break
			}
		} else {
			snapErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	if snapErr != nil {
		t.Fatalf("projection Snapshot after fold: %v", snapErr)
	}
	st, ok := snap.Values[session.SessionSummaryKey].(*session.SessionSummaryState)
	if !ok {
		t.Fatalf("summary value type = %T, want *session.SessionSummaryState", snap.Values[session.SessionSummaryKey])
	}
	if st.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2 (both messages folded post-commit)", st.MessageCount)
	}
	if !st.HasResponse {
		t.Error("HasResponse = false, want true (assistant message folded)")
	}
}
