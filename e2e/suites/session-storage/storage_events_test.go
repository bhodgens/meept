//go:build e2e

// G5 gate (03-session-storage/00T-tests.md): storage.record.changed put/delete
// post-commit ordering. Pins contract S4 (.hermes/contracts/03-session-storage.md)
// and leaf 04-storage-events (SetStorageEventEmitter typed-nil setter seam):
//   - StorageRecordChangedTopic = bus.NewTopic[RecordChanged]("storage.record.changed")
//   - RecordChanged{Store,Key,Op,Value}; op "put" carries the new snapshot in
//     Value, op "delete" carries no value
//   - events fire strictly AFTER the matching commit, outside the store lock,
//     best-effort (never fails the save/delete)
//
// Per the 00T doc the e2e spins its own SQLiteStore + MessageBus in-process
// (the surface under test IS the store/bus). RED until leaf 04-storage-events
// lands (SetStorageEventEmitter + internal/storage symbols undefined today).

package session_storage_test

import (
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/storage"
)

// recordedEvent is one StorageRecordChangedTopic payload plus arrival order.
type recordedEvent struct {
	rc storage.RecordChanged
	at int
}

func TestStorageEvents_PutDeletePostCommit(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "sessions.db")

	msgBus := bus.New(nil, slog.Default())
	store, err := session.NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	store.SetStorageEventEmitter(storage.NewChangeEmitter(msgBus))

	// Subscribe BEFORE any commit so no event is missed. Typed subscribe
	// (SubscribeT) delivers the decoded RecordChanged via callback.
	var (
		mu     sync.Mutex
		events []recordedEvent
	)
	sub := bus.SubscribeT(msgBus, "g5-test", storage.StorageRecordChangedTopic, func(rc storage.RecordChanged) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, recordedEvent{rc: rc, at: len(events)})
	})
	defer bus.Unsubscribe(msgBus, sub)

	waitEvents := func(n int, what string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := len(events)
			mu.Unlock()
			if got >= n {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timeout waiting for %d event(s) after %s (got %d)", n, what, len(events))
	}

	sess, err := store.Create("g5-events")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1. Save a message: a put event must arrive after the commit returns.
	msg := session.Message{
		ID:        1,
		SessionID: sess.ID,
		Role:      "user",
		Content:   "hello g5",
		Timestamp: time.Now(),
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(sess.ID, []session.Message{msg}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}
	// Post-commit ordering: the message row is readable NOW (commit done),
	// so any put event that arrives is strictly post-commit.
	msgs, err := store.GetMessages(sess.ID, 0, 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("GetMessages after save: %v, n=%d", err, len(msgs))
	}
	waitEvents(1, "SaveMessages commit")

	// 2. Delete the session: a delete event must arrive, with no value.
	if !store.Delete(sess.ID) {
		t.Fatal("Delete returned false")
	}
	if s := store.Get(sess.ID); s != nil {
		t.Error("session still visible after Delete; events must follow commits, not precede them")
	}
	waitEvents(2, "Delete commit")

	// 3. Assert the recorded sequence: a put with a value snapshot, then a
	// delete with no value.
	mu.Lock()
	defer mu.Unlock()
	var puts, dels int
	for _, ev := range events {
		switch ev.rc.Op {
		case storage.ChangeOpPut:
			puts++
			if ev.rc.Store == "" || ev.rc.Key == "" {
				t.Errorf("put event missing store/key: %+v", ev.rc)
			}
			if len(ev.rc.Value) == 0 {
				t.Error("put event must carry the new value snapshot")
			}
		case storage.ChangeOpDelete:
			dels++
			if len(ev.rc.Value) != 0 {
				t.Errorf("delete event must not carry a value, got %s", string(ev.rc.Value))
			}
		default:
			t.Errorf("unknown op %q on the typed topic; contract allows put|delete only", ev.rc.Op)
		}
	}
	if puts < 1 {
		t.Errorf("got %d put events, want >= 1 (saved message)", puts)
	}
	if dels < 1 {
		t.Errorf("got %d delete events, want >= 1 (deleted session)", dels)
	}
	// Ordering: the FIRST event is the put (save happened first).
	if events[0].rc.Op != storage.ChangeOpPut {
		t.Errorf("first event op = %q, want put (save precedes delete)", events[0].rc.Op)
	}
}

func TestStorageEvents_NilEmitterNoop(t *testing.T) {
	// A nil-bus emitter must no-op without panicking; the store commit path
	// must be unaffected (publish never blocks or fails the commit).
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "sessions.db")
	store, err := session.NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	store.SetStorageEventEmitter(storage.NewChangeEmitter(nil))

	sess, err := store.Create("g5-nilbus")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	msg := session.Message{
		ID:        1,
		SessionID: sess.ID,
		Role:      "user",
		Content:   "silent",
		Timestamp: time.Now(),
		EntryType: "message",
		BranchID:  "main",
	}
	if err := store.SaveMessages(sess.ID, []session.Message{msg}); err != nil {
		t.Fatalf("SaveMessages with nil-bus emitter must still commit: %v", err)
	}
	if !store.Delete(sess.ID) {
		t.Fatal("Delete with nil-bus emitter must still commit")
	}
}
