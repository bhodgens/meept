package daemon

// ledger_publish_test.go — branch 02-surfaces TEST GATE (§3), leaf
// 02-surfaces/00T. Gates impl leaf 02-surfaces/03
// (internal/daemon/ledger_publish.go). Tests ONLY; RED until the
// implementation lands.
//
// Pinned shape (03-publish.md): buildLedgerAfterAppendHook(bus
// *bus.MessageBus, logger *slog.Logger) returns the hook passed to
// ledger.Store.WithAfterAppend. Body contract:
//   - nil bus -> no-op hook (never panics).
//   - State non-nil (status-carrying row): publish
//     storage.RecordChanged{Store: "ledger", Key: emp+"/"+subject,
//     Op: ChangeOpPut, Value: <State JSON>} on the frozen
//     storage.StorageRecordChangedTopic ("storage.record.changed") —
//     never a new topic, never PublishBlockingT.
//   - State nil (log-only row): Key = emp+"/" (trailing slash per
//     03-publish task 2), Value = <Record JSON> — S4 "carries the new
//     snapshot".
//
// §3 matrix: a successful Append delivers EXACTLY ONE event with
// Store=="ledger", Key=="<emp>/<subject>", Op=="put", Value decodes to
// the State JSON; a rejected Append (illegal transition) delivers ZERO.
// The subscriber is registered BEFORE the Append runs — the post-commit
// ordering proof: inside the delivery callback we read the store back
// and the row MUST already be durable (on a pre-commit publish the
// single-connection WAL pool deadlocks the callback and this test
// hangs/fails).

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/storage"
)

// ledgerPublishDrain is how long we wait to PROVE absence (zero events).
const ledgerPublishDrain = 150 * time.Millisecond

// newLedgerPublishBus subscribes a recorder to the frozen S4 topic
// BEFORE anything else runs (registration-order proof) and returns the
// bus + event channel.
func newLedgerPublishBus(t *testing.T) (*bus.MessageBus, chan storage.RecordChanged) {
	t.Helper()
	msgBus := bus.New(nil, slog.New(slog.DiscardHandler))
	t.Cleanup(msgBus.Close)

	events := make(chan storage.RecordChanged, 16)
	sub := bus.SubscribeT(msgBus, "ledger-publish-gate", storage.StorageRecordChangedTopic,
		func(ev storage.RecordChanged) { events <- ev })
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	return msgBus, events
}

// ledgerPublishStore opens a temp store with the gate hook installed.
func ledgerPublishStore(t *testing.T, msgBus *bus.MessageBus) *ledger.Store {
	t.Helper()
	st, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	st.WithAfterAppend(buildLedgerAfterAppendHook(msgBus, slog.New(slog.DiscardHandler)))
	return st
}

func recvLedgerEvent(t *testing.T, events chan storage.RecordChanged) storage.RecordChanged {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no storage.record.changed delivered within 2s of a successful Append")
		return storage.RecordChanged{}
	}
}

func assertNoLedgerEvent(t *testing.T, events chan storage.RecordChanged, why string) {
	t.Helper()
	select {
	case ev := <-events:
		t.Fatalf("%s: unexpected event %+v", why, ev)
	case <-time.After(ledgerPublishDrain):
	}
}

// TestLedgerPublishExactlyOneOnSuccessfulAppend pins §3 row 1: one
// status-carrying Append -> exactly one S4 event, {store:"ledger",
// key:"<emp>/<subject>", op:"put", value:State JSON}. The callback-time
// re-read proves the publish rode AFTER commit.
func TestLedgerPublishExactlyOneOnSuccessfulAppend(t *testing.T) {
	msgBus, events := newLedgerPublishBus(t)
	st := ledgerPublishStore(t, msgBus)

	rec, err := st.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "eve",
		Kind:       ledger.KindAction,
		Subject:    "acme-roof",
		Payload:    json.RawMessage(`{"did":"sanded"}`),
		Source:     ledger.SourceTool,
		SetStatus:  ledger.StatusOpen,
		Summary:    "quote requested",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	ev := recvLedgerEvent(t, events)

	if ev.Store != "ledger" {
		t.Errorf("Store = %q, want %q", ev.Store, "ledger")
	}
	if ev.Key != "eve/acme-roof" {
		t.Errorf("Key = %q, want %q", ev.Key, "eve/acme-roof")
	}
	if ev.Op != storage.ChangeOpPut {
		t.Errorf("Op = %q, want %q", ev.Op, storage.ChangeOpPut)
	}
	// Value decodes to the State JSON — status/summary from the row.
	var stateMap map[string]any
	if err := json.Unmarshal(ev.Value, &stateMap); err != nil {
		t.Fatalf("Value %s is not State JSON: %v", ev.Value, err)
	}
	flat := strings.ToLower(string(ev.Value))
	for _, want := range []string{"open", "quote requested", "acme-roof", "eve"} {
		if !strings.Contains(flat, strings.ToLower(want)) {
			t.Errorf("State JSON %s missing %q", ev.Value, want)
		}
	}

	// Post-commit proof part 2: the row the event announced is durable.
	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil {
		t.Fatalf("QueryLog after publish: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != rec.ID {
		t.Fatalf("published event announced an uncommitted row: rows=%v rec=%+v", rows, rec)
	}

	// EXACTLY ONE: nothing else on the topic.
	assertNoLedgerEvent(t, events, "second event for a single append")
}

// TestLedgerPublishZeroOnRejectedAppend pins §3 row 2: an illegal
// transition Append delivers ZERO events (and wrote no rows).
func TestLedgerPublishZeroOnRejectedAppend(t *testing.T) {
	msgBus, events := newLedgerPublishBus(t)
	st := ledgerPublishStore(t, msgBus)
	ctx := context.Background()

	if _, err := st.Append(ctx, ledger.DraftRecord{
		EmployeeID: "eve", Kind: ledger.KindAction, Subject: "acme-roof",
		Source: ledger.SourceTool, SetStatus: ledger.StatusDone, Summary: "done",
	}); err != nil {
		t.Fatalf("seed Append: %v", err)
	}
	recvLedgerEvent(t, events) // consume the seed's one event

	// done -> cancelled is illegal: zero events, no hook fire.
	if _, err := st.Append(ctx, ledger.DraftRecord{
		EmployeeID: "eve", Kind: ledger.KindAction, Subject: "acme-roof",
		Source: ledger.SourceTool, SetStatus: ledger.StatusCancelled, Summary: "nope",
	}); err == nil {
		t.Fatal("illegal transition Append must error")
	}
	assertNoLedgerEvent(t, events, "rejected append must publish ZERO")
}

// TestLedgerPublishLogOnlyAppend pins the 03-publish task-2 branch: a
// log-only row (no SetStatus) publishes Key = "<emp>/" with the Record
// JSON as the new snapshot (S4: every durable write announces).
func TestLedgerPublishLogOnlyAppend(t *testing.T) {
	msgBus, events := newLedgerPublishBus(t)
	st := ledgerPublishStore(t, msgBus)

	rec, err := st.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "bob", Kind: ledger.KindObservation, Subject: "weather",
		Payload: json.RawMessage(`{"t":"rain"}`), Source: ledger.SourceTask,
	})
	if err != nil {
		t.Fatalf("log-only Append: %v", err)
	}

	ev := recvLedgerEvent(t, events)
	if ev.Store != "ledger" {
		t.Errorf("Store = %q, want ledger", ev.Store)
	}
	if ev.Key != "bob/" {
		t.Errorf("log-only Key = %q, want %q (emp + slash per 03-publish task 2)", ev.Key, "bob/")
	}
	if ev.Op != storage.ChangeOpPut {
		t.Errorf("Op = %q, want put", ev.Op)
	}
	// Value is the Record JSON: must carry the row subject and id.
	if !strings.Contains(string(ev.Value), `"weather"`) {
		t.Errorf("Record JSON %s missing subject", ev.Value)
	}
	if !strings.Contains(string(ev.Value), `"ID"`) && !strings.Contains(string(ev.Value), `"id"`) {
		t.Errorf("Record JSON %s missing row id field", ev.Value)
	}
	assertNoLedgerEvent(t, events, "log-only append must publish exactly one")
	_ = rec
}

// TestLedgerPublishNilBusNoOp pins 03-publish task 1: a nil bus yields a
// no-op hook — appends succeed and nothing panics.
func TestLedgerPublishNilBusNoOp(t *testing.T) {
	hook := buildLedgerAfterAppendHook(nil, slog.New(slog.DiscardHandler))
	st, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	st.WithAfterAppend(hook)

	if _, err := st.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "eve", Kind: ledger.KindAction, Subject: "acme-roof",
		Source: ledger.SourceTool, SetStatus: ledger.StatusOpen, Summary: "s",
	}); err != nil {
		t.Fatalf("nil-bus hook must not break appends: %v", err)
	}
}
