package daemon

// ledger_audit_test.go — branch 04-ops TEST GATE rows for leaf 04-02
// (02-audit-entries.md):
//   - 1 chain row (type "ledger.append") per status-carrying Append,
//     payload {ledger_id, kind, subject, source, source_ref, status}.
//   - 0 chain rows per log-only Append (observation/committal).
//   - nil chain handle -> appends succeed unchanged, no panic.
//   - a chain-write failure cannot break the publish hook ordering.

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/storage"
)

// newLedgerAuditChain opens a throwaway audit-chain store (OpenStore
// owns the DB; t.Cleanup closes it).
func newLedgerAuditChain(t *testing.T) *auditlog.Store {
	t.Helper()
	s, err := auditlog.OpenStore(filepath.Join(t.TempDir(), "chain.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("auditlog.OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// newLedgerAuditStore opens a temp ledger store with the 04-02 audit
// hook installed (alone — publish is gated by ledger_publish_test.go).
func newLedgerAuditStore(t *testing.T, chain *auditlog.Store) *ledger.Store {
	t.Helper()
	st, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	st.WithAfterAppend(buildLedgerAuditHook(chain, slog.New(slog.DiscardHandler)))
	return st
}

func countChainRows(t *testing.T, chain *auditlog.Store) int {
	t.Helper()
	var n int
	if err := chain.ChainDB().QueryRow(`SELECT COUNT(*) FROM audit_log_chain`).Scan(&n); err != nil {
		t.Fatalf("count chain rows: %v", err)
	}
	return n
}

// TestLedgerAuditOneChainRowPerStatusAppend pins gate row 1: one
// status-carrying Append -> exactly one chain row of type
// "ledger.append" with the frozen employee_id and payload keys.
func TestLedgerAuditOneChainRowPerStatusAppend(t *testing.T) {
	chain := newLedgerAuditChain(t)
	st := newLedgerAuditStore(t, chain)

	rec, err := st.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "emp-1",
		Kind:       ledger.KindCommitment,
		Subject:    "acme-roof",
		Payload:    []byte(`{"note":"roof quote owed"}`),
		Source:     ledger.SourceTool,
		SourceRef:  "turn-42",
		SetStatus:  ledger.StatusOpen,
		Summary:    "owes roof quote",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if got := countChainRows(t, chain); got != 1 {
		t.Fatalf("chain rows after 1 status append = %d, want 1", got)
	}

	head, ok, err := chain.Head(context.Background())
	if err != nil || !ok {
		t.Fatalf("chain.Head: ok=%v err=%v", ok, err)
	}
	if head.Type != "ledger.append" {
		t.Errorf("chain row type = %q, want %q", head.Type, "ledger.append")
	}
	if head.EmployeeID != "emp-1" {
		t.Errorf("chain employee_id = %q, want %q", head.EmployeeID, "emp-1")
	}
	want := map[string]any{
		"ledger_id":  float64(rec.ID),
		"kind":       "commitment",
		"subject":    "acme-roof",
		"source":     "tool",
		"source_ref": "turn-42",
		"status":     "open",
	}
	for k, v := range want {
		if got := head.Payload[k]; got != v {
			t.Errorf("payload[%q] = %#v, want %#v", k, got, v)
		}
	}
	if len(head.Payload) != len(want) {
		t.Errorf("payload has %d keys, want %d (frozen set): %#v", len(head.Payload), len(want), head.Payload)
	}
}

// TestLedgerAuditZeroRowsPerLogOnlyAppend pins gate row 2: observation
// and committal (log-only) appends never touch the chain.
func TestLedgerAuditZeroRowsPerLogOnlyAppend(t *testing.T) {
	chain := newLedgerAuditChain(t)
	st := newLedgerAuditStore(t, chain)

	for i, d := range []ledger.DraftRecord{
		{EmployeeID: "emp-1", Kind: ledger.KindObservation, Subject: "acme-roof", Source: ledger.SourceVoice},
		{EmployeeID: "emp-1", Kind: ledger.KindCommittal, Subject: "acme-roof", Source: ledger.SourceVoice},
	} {
		if _, err := st.Append(context.Background(), d); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if got := countChainRows(t, chain); got != 0 {
		t.Fatalf("chain rows after 2 log-only appends = %d, want 0", got)
	}
}

// TestLedgerAuditNilHandleNoCrash pins gate row 3: nil chain handle ->
// the hook is a silent no-op and appends succeed unchanged.
func TestLedgerAuditNilHandleNoCrash(t *testing.T) {
	st := newLedgerAuditStore(t, nil)

	rec, err := st.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "emp-1",
		Kind:       ledger.KindAction,
		Subject:    "acme-roof",
		Source:     ledger.SourceDesk,
		SetStatus:  ledger.StatusDone,
		Summary:    "done",
	})
	if err != nil {
		t.Fatalf("Append with nil chain handle: %v", err)
	}
	if rec == nil || rec.ID == 0 {
		t.Fatalf("Append returned %+v, want a stored record", rec)
	}

	// Direct hook call with non-nil state must not panic either.
	buildLedgerAuditHook(nil, slog.New(slog.DiscardHandler))(
		ledger.Record{ID: 1, EmployeeID: "emp-1"}, &ledger.State{Status: ledger.StatusOpen})
}

// TestLedgerAuditChainFailureKeepsPublishOrdering pins the checklist
// row: a chain-write failure (closed chain DB) surfaces as WARN only —
// Append still succeeds and the S4 publish event (installed first in
// the composition) still fires.
func TestLedgerAuditChainFailureKeepsPublishOrdering(t *testing.T) {
	msgBus := bus.New(nil, slog.New(slog.DiscardHandler))
	t.Cleanup(msgBus.Close)
	events := make(chan storage.RecordChanged, 16)
	sub := bus.SubscribeT(msgBus, "ledger-audit-gate", storage.StorageRecordChangedTopic,
		func(ev storage.RecordChanged) { events <- ev })
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	// Chain handle over a DB we close out from under it -> every
	// chain.Append fails.
	dead, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open throwaway db: %v", err)
	}
	chain, err := auditlog.OpenStoreFromDB(dead, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	if err := dead.Close(); err != nil {
		t.Fatalf("close throwaway db: %v", err)
	}

	st, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Frozen composition order: publish first, audit chained second.
	st.WithAfterAppend(composeLedgerAfterAppendHooks(
		buildLedgerAfterAppendHook(msgBus, slog.New(slog.DiscardHandler)),
		buildLedgerAuditHook(chain, slog.New(slog.DiscardHandler)),
	))

	if _, err := st.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "emp-1",
		Kind:       ledger.KindResolution,
		Subject:    "acme-roof",
		Source:     ledger.SourceTask,
		SetStatus:  ledger.StatusDone,
	}); err != nil {
		t.Fatalf("Append with failing chain write: %v (hook failure must never surface)", err)
	}

	select {
	case ev := <-events:
		if ev.Store != "ledger" || ev.Key != "emp-1/acme-roof" {
			t.Errorf("publish event = {store:%q key:%q}, want {ledger emp-1/acme-roof}", ev.Store, ev.Key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("chain failure suppressed the S4 publish event — ordering broken")
	}
}
