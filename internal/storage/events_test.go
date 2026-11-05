package storage

// Leaf 03-session-storage/04 tasks T1-T3 pins (contract S4):
//   - T1: payload shape {store,key,op,value}; op put/delete; topic name exact
//   - T2: nil-safe ChangeEmitter; delete omits Value
//   - T3: unknown op rejected (log + skip), never emitted

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"log/slog"
)

func TestStorageEvents_T1_PayloadShapeAndTopicName(t *testing.T) {
	if StorageRecordChangedTopic.Name != "storage.record.changed" {
		t.Fatalf("topic name = %q, want storage.record.changed", StorageRecordChangedTopic.Name)
	}

	rc := RecordChanged{
		Store: "session",
		Key:   "session-abc",
		Op:    ChangeOpPut,
		Value: json.RawMessage(`{"role":"user"}`),
	}
	raw, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"store", "key", "op", "value"} {
		if _, ok := m[k]; !ok {
			t.Errorf("payload missing %q: %s", k, raw)
		}
	}
	if m["op"] != "put" {
		t.Errorf("op = %v, want put", m["op"])
	}

	// delete tombstone: value omitted entirely (omitempty).
	del := RecordChanged{Store: "session", Key: "session-abc", Op: ChangeOpDelete}
	rawDel, err := json.Marshal(del)
	if err != nil {
		t.Fatalf("marshal delete: %v", err)
	}
	if got := string(rawDel); got != `{"store":"session","key":"session-abc","op":"delete"}` {
		t.Errorf("delete payload = %s, want value omitted", got)
	}
}

func TestStorageEvents_T2_EmitterPublishAndNilSafety(t *testing.T) {
	b := bus.New(nil, slog.New(slog.DiscardHandler))
	em := NewChangeEmitter(b)

	var (
		mu   sync.Mutex
		got  []RecordChanged
		done = make(chan struct{}, 1)
	)
	sub := bus.SubscribeT(b, "t2-test", StorageRecordChangedTopic, func(rc RecordChanged) {
		mu.Lock()
		got = append(got, rc)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})
	defer bus.Unsubscribe(b, sub)

	em.Publish("t2", RecordChanged{
		Store: "session", Key: "k1", Op: ChangeOpPut,
		Value: json.RawMessage(`{"n":1}`),
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("typed topic never received the put event")
	}
	mu.Lock()
	rc := got[0]
	mu.Unlock()
	if rc.Store != "session" || rc.Key != "k1" || rc.Op != ChangeOpPut || len(rc.Value) == 0 {
		t.Errorf("received payload = %+v, want store/key/put/value", rc)
	}

	// Nil-bus emitter: no-op, no panic.
	NewChangeEmitter(nil).Publish("t2", RecordChanged{Store: "s", Key: "k", Op: ChangeOpPut})
	// Nil-receiver emitter: no-op, no panic.
	var nilEm *ChangeEmitter
	nilEm.Publish("t2", RecordChanged{Store: "s", Key: "k", Op: ChangeOpDelete})
}

func TestStorageEvents_T3_UnknownOpRejected(t *testing.T) {
	b := bus.New(nil, slog.New(slog.DiscardHandler))
	em := NewChangeEmitter(b)

	var (
		mu  sync.Mutex
		got []RecordChanged
	)
	sub := bus.SubscribeT(b, "t3-test", StorageRecordChangedTopic, func(rc RecordChanged) {
		mu.Lock()
		got = append(got, rc)
		mu.Unlock()
	})
	defer bus.Unsubscribe(b, sub)

	em.Publish("t3", RecordChanged{Store: "session", Key: "k", Op: ChangeOp("upsert")})
	// Give a rogue delivery a chance to arrive; the assertion is absence.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 0 {
		t.Errorf("unknown op was emitted on the topic: %+v", got)
	}
	if !ValidOp(ChangeOpPut) || !ValidOp(ChangeOpDelete) || ValidOp(ChangeOp("upsert")) {
		t.Error("ValidOp must accept exactly put|delete")
	}
}
