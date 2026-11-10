package daemon

// Leaf 03-voice/00T §2 test gate: brief -> ledger row mapping via the
// frozen signature
//
//	appendBriefRecords(ctx context.Context, store *ledger.Store,
//	    employeeID, ccid string, brief *briefResult) (int, error)
//
// (briefResult + the function are owned by leaf 03-02; this file only
// reads the frozen JSON shape through encoding/json so the nested item
// type name stays the impl's choice). Tests ONLY — run against a real
// ledger.Store in a temp dir (branch 01 API is complete and green).
//
// Frozen mapping (00T §2):
//   - one committal row, subject "call:"+ccid, payload = full brief;
//   - per action_item a commitment row, same subject;
//   - the FIRST commitment also sets status: owner "them" ->
//     waiting_on_them, else open; Summary = first item text;
//   - non-empty follow_up_message -> a commitment row with payload
//     {"type":"follow_up_sms","text":...};
//   - risk_flags -> one observation row payload {"flags":[...]};
//   - ALL rows Source=voice, SourceRef=ccid; ordering committal ->
//     commitments -> observation (asserted via QueryLog, id ASC).
//   - Summary hygiene: phone-shaped runs (\+\d{7,}) in item text are
//     replaced by "(number on file)" before they reach Summary; the
//     payload JSON keeps the original text.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/ledger"
)

var vbPhoneRun = regexp.MustCompile(`\+\d`)

// vbOpenStore opens a throwaway branch-01 ledger store for the mapping
// tests.
func vbOpenStore(t *testing.T) *ledger.Store {
	t.Helper()
	store, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), testLogger(t))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// vbDecodePayload unmarshals a record payload into a generic map,
// transparently unwrapping the frozen branch-01 PayloadEnvelope
// ({"v":1,"status":...,"summary":...,"data":<user payload>}) that Append
// writes for status-carrying rows, so assertions see the USER payload.
func vbDecodePayload(t *testing.T, rec ledger.Record) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Payload, &m); err != nil {
		t.Fatalf("decode payload of row %d (%q): %v", rec.ID, rec.Payload, err)
	}
	if _, hasV := m["v"]; hasV {
		if data, ok := m["data"].(map[string]any); ok {
			return data
		}
	}
	return m
}

// vbLogRows queries the whole log for the employee, id ASC.
func vbLogRows(t *testing.T, store *ledger.Store, employeeID string) []ledger.Record {
	t.Helper()
	rows, err := store.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: employeeID})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	return rows
}

func TestBriefRecordsFullMapping(t *testing.T) {
	store := vbOpenStore(t)
	brief := vbParseBrief(t, vbBriefJSON) // 2 items (first owner=them), follow-up, 1 risk

	n, err := appendBriefRecords(context.Background(), store, "emp-map", "cc-map", brief)
	if err != nil {
		t.Fatalf("appendBriefRecords: %v", err)
	}
	// 1 committal + 2 action-item commitments + 1 follow_up commitment +
	// 1 observation = 5.
	if n != 5 {
		t.Errorf("returned count = %d, want 5", n)
	}

	rows := vbLogRows(t, store, "emp-map")
	if len(rows) != 5 {
		t.Fatalf("QueryLog rows = %d, want 5: %+v", len(rows), rows)
	}

	// Ordering, kinds, subject, source discipline.
	wantKinds := []ledger.Kind{
		ledger.KindCommittal,
		ledger.KindCommitment,  // action item 1: order siding materials (them)
		ledger.KindCommitment,  // action item 2: send the written quote (me)
		ledger.KindCommitment,  // follow_up_message row
		ledger.KindObservation, // risk flags
	}
	const wantSubject = "call:cc-map"
	for i, rec := range rows {
		if rec.Kind != wantKinds[i] {
			t.Errorf("row %d kind = %q, want %q", i, rec.Kind, wantKinds[i])
		}
		if rec.Source != ledger.SourceVoice {
			t.Errorf("row %d source = %q, want voice", i, rec.Source)
		}
		if rec.SourceRef != "cc-map" {
			t.Errorf("row %d source_ref = %q, want cc-map", i, rec.SourceRef)
		}
		if i < 4 && rec.Subject != wantSubject {
			t.Errorf("row %d subject = %q, want %q", i, rec.Subject, wantSubject)
		}
	}

	// Committal payload carries the full brief.
	head := vbDecodePayload(t, rows[0])
	if head["summary"] != "Caller needs the siding repaired before friday" {
		t.Errorf("committal payload summary = %v", head["summary"])
	}

	// Action-item commitment payloads carry text/owner.
	item1 := vbDecodePayload(t, rows[1])
	if item1["text"] != "order siding materials" || item1["owner"] != "them" {
		t.Errorf("commitment row 1 payload = %v", item1)
	}
	if item1["due"] != "friday" {
		t.Errorf("commitment row 1 due = %v, want friday", item1["due"])
	}
	item2 := vbDecodePayload(t, rows[2])
	if item2["text"] != "send the written quote" || item2["owner"] != "me" {
		t.Errorf("commitment row 2 payload = %v", item2)
	}

	// Follow-up row: {"type":"follow_up_sms","text":...}
	fu := vbDecodePayload(t, rows[3])
	if fu["type"] != "follow_up_sms" {
		t.Errorf("follow-up row type = %v, want follow_up_sms", fu["type"])
	}
	if fu["text"] != "Confirming the siding order — quote goes out today." {
		t.Errorf("follow-up row text = %v", fu["text"])
	}

	// Observation row: {"flags":[...]}
	obs := vbDecodePayload(t, rows[4])
	flags, _ := obs["flags"].([]any)
	if len(flags) != 1 || flags[0] != "weather delay" {
		t.Errorf("observation flags = %v, want [weather delay]", obs["flags"])
	}

	// First commitment's SetStatus: owner them -> waiting_on_them,
	// Summary = first item text.
	st, ok, err := store.GetState(context.Background(), "emp-map", wantSubject)
	if err != nil || !ok {
		t.Fatalf("GetState(%s) ok=%v err=%v — first commitment must set status", wantSubject, ok, err)
	}
	if st.Status != ledger.StatusWaitingThem {
		t.Errorf("state status = %q, want waiting_on_them", st.Status)
	}
	if st.Summary != "order siding materials" {
		t.Errorf("state summary = %q, want first item text", st.Summary)
	}
}

func TestBriefRecordsOwnerMeOpensState(t *testing.T) {
	store := vbOpenStore(t)
	brief := vbParseBrief(t, `{"summary":"needs a re-roof quote","action_items":[{"text":"mail the deposit","owner":"me","due":""}],"commitments":[],"follow_up_message":"","risk_flags":[]}`)

	if _, err := appendBriefRecords(context.Background(), store, "emp-me", "cc-me", brief); err != nil {
		t.Fatalf("appendBriefRecords: %v", err)
	}
	st, ok, err := store.GetState(context.Background(), "emp-me", "call:cc-me")
	if err != nil || !ok {
		t.Fatalf("GetState ok=%v err=%v", ok, err)
	}
	if st.Status != ledger.StatusOpen {
		t.Errorf("state status = %q, want open (first owner is me)", st.Status)
	}
}

func TestBriefRecordsNoOptionalRows(t *testing.T) {
	// No follow-up, no risk flags -> only committal + one commitment; no
	// status set unless an action item exists (here one item -> state row).
	store := vbOpenStore(t)
	brief := vbParseBrief(t, `{"summary":"quick hello","action_items":[{"text":"call back the inspector","owner":"them","due":"tomorrow"}],"commitments":[],"follow_up_message":"","risk_flags":[]}`)

	n, err := appendBriefRecords(context.Background(), store, "emp-min", "cc-min", brief)
	if err != nil {
		t.Fatalf("appendBriefRecords: %v", err)
	}
	if n != 2 {
		t.Errorf("returned count = %d, want 2 (no follow_up/observation rows)", n)
	}
	rows := vbLogRows(t, store, "emp-min")
	if len(rows) != 2 || rows[0].Kind != ledger.KindCommittal || rows[1].Kind != ledger.KindCommitment {
		t.Fatalf("rows = %+v, want [committal, commitment]", rows)
	}
}

func TestBriefRecordsCommitittalOnlyForSummaryOnlyBrief(t *testing.T) {
	// Empty action items, no follow-up, no risks -> a single committal
	// log-only row and NO state row (SetStatus only rides the first
	// commitment).
	store := vbOpenStore(t)
	brief := vbParseBrief(t, `{"summary":"wrong number, polite goodbye","action_items":[],"commitments":[],"follow_up_message":"","risk_flags":[]}`)

	n, err := appendBriefRecords(context.Background(), store, "emp-nil", "cc-nil", brief)
	if err != nil {
		t.Fatalf("appendBriefRecords: %v", err)
	}
	if n != 1 {
		t.Errorf("returned count = %d, want 1", n)
	}
	if _, ok, err := store.GetState(context.Background(), "emp-nil", "call:cc-nil"); err != nil || ok {
		t.Errorf("state row exists (ok=%v err=%v); summary-only brief must be log-only", ok, err)
	}
}

func TestBriefRecordsPhoneMaskingInSummaryOnly(t *testing.T) {
	// Summary hygiene (03-02 task 3): phone-shaped runs (\+\d{7,}) inside
	// item text get replaced by "(number on file)" before they reach the
	// state Summary; the payload JSON keeps the ORIGINAL text
	// (masking is display-path only).
	store := vbOpenStore(t)
	const raw = "call back at +15559874567 about the siding"
	brief := vbParseBrief(t, `{"summary":"caller wants a callback","action_items":[{"text":"`+raw+`","owner":"them","due":""}],"commitments":[],"follow_up_message":"","risk_flags":[]}`)

	if _, err := appendBriefRecords(context.Background(), store, "emp-mask", "cc-mask", brief); err != nil {
		t.Fatalf("appendBriefRecords: %v", err)
	}

	st, ok, err := store.GetState(context.Background(), "emp-mask", "call:cc-mask")
	if err != nil || !ok {
		t.Fatalf("GetState ok=%v err=%v", ok, err)
	}
	if vbPhoneRun.MatchString(st.Summary) {
		t.Errorf("state Summary leaks phone run: %q", st.Summary)
	}
	if !strings.Contains(st.Summary, "(number on file)") {
		t.Errorf("state Summary = %q, want the '(number on file)' substitution", st.Summary)
	}

	// Payload keeps the original digits (both the commitment row and the
	// committal row carry the brief verbatim).
	rows := vbLogRows(t, store, "emp-mask")
	found := false
	for _, rec := range rows {
		if strings.Contains(string(rec.Payload), "5559874567") {
			found = true
		}
	}
	if !found {
		t.Error("no row payload preserves the original phone run — masking must be display-path only")
	}
}
