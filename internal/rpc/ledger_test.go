package rpc

// ledger_test.go — branch 02-surfaces TEST GATE (§2), leaf
// 02-surfaces/00T. Gates impl leaf 02-surfaces/02 (internal/rpc/
// ledger.go). Tests ONLY; RED until the implementation lands.
//
// SEAM POSTURE (pinned for the impl leaf — copy exactly): internal/rpc
// must not import internal/ledger (keep rpc light, per the voice_local.go
// precedent at internal/rpc/voice_local.go: seam func/interface fields +
// small LOCAL result types, never store types). The impl leaf defines in
// internal/rpc/ledger.go:
//
//	// LedgerReader is the narrow read seam the daemon adapts from
//	// *ledger.Store (QueryLog / QueryOpen / max-id head).
//	type LedgerReader interface {
//	    QueryLog(ctx context.Context, employeeID, subject string,
//	        sinceID int64, limit int) ([]LedgerLogEntry, error)
//	    QueryOpen(ctx context.Context, employeeID string) ([]LedgerOpenState, error)
//	    Head(ctx context.Context) (int64, error)
//	}
//
//	// Local wire types (JSON tags frozen to ledger.State / ledger.Record
//	// field names; employee id IS a request field here — client-facing
//	// read, unpermissioned in v1 per D1, unlike the ctx-stamped tools).
//	type LedgerLogEntry struct {
//	    ID         int64           `json:"id"`
//	    EmployeeID string          `json:"employee_id"`
//	    At         string          `json:"at"`
//	    Kind       string          `json:"kind"`
//	    Subject    string          `json:"subject"`
//	    Payload    json.RawMessage `json:"payload,omitempty"`
//	    Source     string          `json:"source"`
//	    SourceRef  string          `json:"source_ref"`
//	    Resolves   int64           `json:"resolves"`
//	}
//	type LedgerOpenState struct {
//	    EmployeeID string `json:"employee_id"`
//	    Subject    string `json:"subject"`
//	    Status     string `json:"status"`
//	    Summary    string `json:"summary"`
//	    UpdatedAt  string `json:"updated_at"`
//	    AsOf       int64  `json:"as_of"`
//	}
//
//	type LedgerHandler struct{ ... }
//	func NewLedgerHandler(reader LedgerReader) *LedgerHandler
//	// (h *LedgerHandler) handleQuery(ctx, params) — "ledger.query":
//	//   req  {employee, mode: "log"|"open", subject?, since_id?, limit?}
//	//   resp LedgerQueryResult
//	// (h *LedgerHandler) handleState(ctx, params) — "ledger.state":
//	//   req  {employee, subject}
//
// 00T §2 response shape (FROZEN): ledger.query returns
// {{records:[...], open:[...], head:int64}} — mode log vs open,
// since_id passthrough, unknown employee -> EMPTY lists, not error
// (records/open serialized as [] not null — the VoiceLocalStatusResult
// "never nil" precedent).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeReader is the seam fake for the pinned LedgerReader interface.
type fakeReader struct {
	logRows  []LedgerLogEntry
	openRows []LedgerOpenState
	head     int64
	headErr  error
	logErr   error
	openErr  error
	gotLogQ  []string // "employee|subject|sinceID" per QueryLog call
	gotOpen  []string // employee per QueryOpen call
}

func (f *fakeReader) QueryLog(_ context.Context, employeeID, subject string, sinceID int64, _ int) ([]LedgerLogEntry, error) {
	f.gotLogQ = append(f.gotLogQ, employeeID+"|"+subject+"|"+itoa64(sinceID))
	return f.logRows, f.logErr
}

func (f *fakeReader) QueryOpen(_ context.Context, employeeID string) ([]LedgerOpenState, error) {
	f.gotOpen = append(f.gotOpen, employeeID)
	return f.openRows, f.openErr
}

func (f *fakeReader) Head(_ context.Context) (int64, error) { return f.head, f.headErr }

func itoa64(v int64) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	return string(b)
}

func queryJSON(t *testing.T, res any) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("result %s not a JSON object: %v", b, err)
	}
	return m
}

func TestLedgerQueryModeLogShape(t *testing.T) {
	f := &fakeReader{
		logRows: []LedgerLogEntry{{
			ID: 7, EmployeeID: "eve", At: "2026-11-09T00:00:00Z", Kind: "action",
			Subject: "acme-roof", Payload: json.RawMessage(`{"did":"x"}`),
			Source: "tool", SourceRef: "", Resolves: 0,
		}},
		head: 7,
	}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{"employee": "eve", "mode": "log", "subject": "acme-roof"})
	res, err := h.handleQuery(context.Background(), raw)
	if err != nil {
		t.Fatalf("ledger.query(log): %v", err)
	}
	m := queryJSON(t, res)
	for _, key := range []string{"records", "open", "head"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("response %s missing frozen key %q", mustJSON(t, m), key)
		}
	}
	if len(f.gotLogQ) != 1 || f.gotLogQ[0] != "eve|acme-roof|0" {
		t.Fatalf("seam passthrough mismatch: %v, want employee+subject to reach QueryLog", f.gotLogQ)
	}
	// records carry the row; head is the store-wide max id seen.
	if !strings.Contains(string(m["records"]), `"acme-roof"`) {
		t.Errorf("records missing subject: %s", m["records"])
	}
	if string(m["head"]) != "7" {
		t.Errorf("head = %s, want 7", m["head"])
	}
}

func TestLedgerQueryModeOpenShape(t *testing.T) {
	f := &fakeReader{
		openRows: []LedgerOpenState{{
			EmployeeID: "eve", Subject: "roof-quote", Status: "open",
			Summary: "owe estimate", UpdatedAt: "2026-11-09T00:00:00Z", AsOf: 3,
		}},
		head: 3,
	}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{"employee": "eve", "mode": "open"})
	res, err := h.handleQuery(context.Background(), raw)
	if err != nil {
		t.Fatalf("ledger.query(open): %v", err)
	}
	m := queryJSON(t, res)
	if len(f.gotOpen) != 1 || f.gotOpen[0] != "eve" {
		t.Fatalf("mode=open must hit QueryOpen once with the request employee: %v", f.gotOpen)
	}
	if !strings.Contains(string(m["open"]), `"roof-quote"`) {
		t.Errorf("open missing subject: %s", m["open"])
	}
	// mode=open still answers the full frozen triple — records present, empty.
	records := string(m["records"])
	if records != "[]" && records != "null" {
		t.Errorf("mode=open records = %s, want empty list []", records)
	}
}

func TestLedgerQuerySinceIDAndLimitPassthrough(t *testing.T) {
	f := &fakeReader{head: 10}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{
		"employee": "eve", "mode": "log", "since_id": 5, "limit": 20,
	})
	if _, err := h.handleQuery(context.Background(), raw); err != nil {
		t.Fatalf("ledger.query(since_id): %v", err)
	}
	if len(f.gotLogQ) != 1 || f.gotLogQ[0] != "eve||5" {
		t.Fatalf("since_id passthrough = %v, want seam called with since_id=5, subject=''", f.gotLogQ)
	}
}

func TestLedgerQueryUnknownEmployeeEmptyListsNotError(t *testing.T) {
	// Empty store: seam answers nil slices + head 0. Unknown employee must
	// surface as EMPTY lists, not an error and not null keys.
	f := &fakeReader{head: 0}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{"employee": "nobody", "mode": "log"})
	res, err := h.handleQuery(context.Background(), raw)
	if err != nil {
		t.Fatalf("unknown employee must NOT error: %v", err)
	}
	m := queryJSON(t, res)
	if got := string(m["records"]); got != "[]" {
		t.Errorf("records = %s, want [] (never null for unknown employee)", got)
	}
	if got := string(m["open"]); got != "[]" {
		t.Errorf("open = %s, want []", got)
	}
	if got := string(m["head"]); got != "0" {
		t.Errorf("head = %s, want 0", got)
	}
	// Same for mode=open.
	raw2, _ := json.Marshal(map[string]any{"employee": "nobody", "mode": "open"})
	res2, err := h.handleQuery(context.Background(), raw2)
	if err != nil {
		t.Fatalf("mode=open unknown employee errored: %v", err)
	}
	m2 := queryJSON(t, res2)
	if got := string(m2["open"]); got != "[]" {
		t.Errorf("mode=open open = %s, want []", got)
	}
}

func TestLedgerQueryInvalidModeRejected(t *testing.T) {
	f := &fakeReader{}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{"employee": "eve", "mode": "bogus"})
	_, err := h.handleQuery(context.Background(), raw)
	if err == nil {
		t.Fatal("mode outside log|open must be a named refusal")
	}
	if len(f.gotLogQ) != 0 || len(f.gotOpen) != 0 {
		t.Errorf("refusal must happen BEFORE the seam is touched: %v %v", f.gotLogQ, f.gotOpen)
	}
}

func TestLedgerQueryReaderErrorPropagates(t *testing.T) {
	f := &fakeReader{logErr: errors.New("db down")}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{"employee": "eve", "mode": "log"})
	_, err := h.handleQuery(context.Background(), raw)
	if err == nil {
		t.Fatal("seam error must propagate to the RPC error channel")
	}
}

func TestLedgerStateHandler(t *testing.T) {
	// ledger.state (02-rpc task 2) — pinned second method: req {employee,
	// subject}. The reader seam above is the only sanctioned dependency;
	// the impl leaf may answer state from QueryOpen rows or a seam method,
	// but the method name + request keys are frozen.
	f := &fakeReader{
		openRows: []LedgerOpenState{{
			EmployeeID: "eve", Subject: "acme-roof", Status: "open",
			Summary: "sanded", UpdatedAt: "2026-11-09T00:00:00Z", AsOf: 2,
		}},
		head: 2,
	}
	h := NewLedgerHandler(f)
	raw, _ := json.Marshal(map[string]any{"employee": "eve", "subject": "acme-roof"})
	res, err := h.handleState(context.Background(), raw)
	if err != nil {
		t.Fatalf("ledger.state: %v", err)
	}
	b := mustJSON(t, res)
	if !strings.Contains(b, `"acme-roof"`) || !strings.Contains(b, `"open"`) {
		t.Errorf("ledger.state result %s must carry the subject and status", b)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
