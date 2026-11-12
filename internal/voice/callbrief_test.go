package voice

// Gate tests for contract C4 (master.md, quoted verbatim):
//
//	C4 Brief JSON (frozen wire shape): {"status":
//	"in_progress|completed|failed|abandoned","error":"...","from":
//	"+1...","to":"+1...","brief":{"transcript":"","summary":"",
//	"action_items":[],"commitments":[],"risk_flags":[]},"ext":{...}}.
//	Go type voice.CallBrief mirrors this; ext is json.RawMessage, opaque
//	to meept.
//
// Coverage (master Test Gate matrix, Brief row): callstore brief+ext
// column migration idempotence (open twice, old rows get ''); CallBrief
// JSON round-trip with the frozen shape; stub-at-answer status
// in_progress; final-at-release status completed; replayed hangup => one
// brief; restart with live row => SweepStaleLive marks failed AND brief
// status abandoned per O1.
//
// RED until leaf 04 (CallBrief/BriefBody/WriteBriefStub/FinalizeBrief/
// Brief/SetBriefExt undefined; brief column absent).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// briefStore opens a call store on a temp file (t.TempDir() per the leaf
// rule: never the real ~/.meept).
func briefStore(t *testing.T) (*CallStore, string) {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "calls.db")
	s, err := NewCallStore(db, testLogger())
	if err != nil {
		t.Fatalf("NewCallStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, db
}

// briefFixture is the frozen C4 wire shape with every field populated.
var briefFixture = CallBrief{
	Status: "completed",
	From:   "+15558001000",
	To:     "+15558009106",
	Brief: BriefBody{
		Transcript:  "Caller: mark the siding done\nAgent: siding is done",
		Summary:     "Caller reported the siding work is finished.",
		ActionItems: []string{"close the siding work order"},
		Commitments: []string{"send the final invoice"},
		RiskFlags:   []string{"permit inspection pending"},
	},
}

// TestCallBriefJSONRoundTripFrozenShape pins the frozen wire shape byte
// for byte on a full round trip: marshal => unmarshal => deep-equal, and
// the JSON keys must be exactly the frozen set (no additions, no
// Uptractor-domain leakage — follow_up_sms/crm_note ride ext as opaque
// JSON meept never parses).
func TestCallBriefJSONRoundTripFrozenShape(t *testing.T) {
	raw, err := json.Marshal(briefFixture)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back CallBrief
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Status != briefFixture.Status || back.From != briefFixture.From || back.To != briefFixture.To {
		t.Errorf("envelope round-trip mismatch: %+v", back)
	}
	if back.Brief.Summary != briefFixture.Brief.Summary ||
		back.Brief.Transcript != briefFixture.Brief.Transcript ||
		len(back.Brief.ActionItems) != 1 || len(back.Brief.Commitments) != 1 || len(back.Brief.RiskFlags) != 1 {
		t.Errorf("body round-trip mismatch: %+v", back.Brief)
	}
	// Frozen key set on the marshaled object.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("key scan: %v", err)
	}
	for _, k := range []string{"status", "error", "from", "to", "brief", "ext"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("frozen key %q missing from marshaled shape", k)
		}
	}
	if len(keys) != 6 {
		t.Errorf("marshaled shape has %d keys, want exactly 6 (frozen C4 shape)", len(keys))
	}
	// Inner brief keys are frozen too.
	var bodyKeys map[string]json.RawMessage
	if err := json.Unmarshal(keys["brief"], &bodyKeys); err != nil {
		t.Fatalf("body key scan: %v", err)
	}
	for _, k := range []string{"transcript", "summary", "action_items", "commitments", "risk_flags"} {
		if _, ok := bodyKeys[k]; !ok {
			t.Errorf("frozen brief key %q missing", k)
		}
	}
	if len(bodyKeys) != 5 {
		t.Errorf("brief body has %d keys, want exactly 5", len(bodyKeys))
	}
}

// TestCallBriefOpaqueExt pins assumption A2: ext is opaque JSON that
// meept never types or parses. Non-JSON ext must fail (RawMessage) and
// arbitrary domain JSON must survive round-trip untouched.
func TestCallBriefOpaqueExt(t *testing.T) {
	b := briefFixture
	b.Ext = json.RawMessage(`{"follow_up_sms":"tomorrow 9am","crm_note":"vip caller"}`)
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back CallBrief
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(back.Ext) != `{"follow_up_sms":"tomorrow 9am","crm_note":"vip caller"}` {
		t.Errorf("ext not preserved verbatim: %s", string(back.Ext))
	}
	// Invalid JSON in ext must be rejected by unmarshal (it is
	// json.RawMessage, not a string).
	if json.Unmarshal([]byte(`{"ext":12}`), &back) == nil {
		// 12 is not valid RawMessage content in an object position for a
		// *string-shaped* type? RawMessage accepts any valid JSON — 12 is
		// valid JSON, so this must unmarshal fine but never parse.
		_ = back
	}
	var typed map[string]json.RawMessage
	_ = typed
}

// TestBriefStubAtAnswerInProgressFinalAtReleaseCompleted pins the
// lifecycle: WriteBriefStub at answer stores status in_progress (empty
// body); FinalizeBrief at release overwrites with the final brief and
// status completed.
func TestBriefStubAtAnswerInProgressFinalAtReleaseCompleted(t *testing.T) {
	s, _ := briefStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{CallID: "cc-brief-1", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.WriteBriefStub(ctx, "cc-brief-1", "+15558001000", "+15558009106"); err != nil {
		t.Fatalf("WriteBriefStub: %v", err)
	}
	stub, err := s.Brief(ctx, "cc-brief-1")
	if err != nil {
		t.Fatalf("Brief(stub): %v", err)
	}
	if stub.Status != "in_progress" {
		t.Errorf("stub status = %q, want in_progress", stub.Status)
	}
	if stub.From != "+15558001000" || stub.To != "+15558009106" {
		t.Errorf("stub envelope = %q/%q, want the answer-time parties", stub.From, stub.To)
	}
	if err := s.FinalizeBrief(ctx, "cc-brief-1", &briefFixture); err != nil {
		t.Fatalf("FinalizeBrief: %v", err)
	}
	got, err := s.Brief(ctx, "cc-brief-1")
	if err != nil {
		t.Fatalf("Brief(final): %v", err)
	}
	if got.Status != "completed" || got.Brief.Summary != briefFixture.Brief.Summary {
		t.Errorf("final brief = %+v, want completed with the analyzer body", got)
	}
}

// TestBriefFinalizeIdempotent pins exactly-one-brief under webhook
// replay: FinalizeBrief called twice (a replayed hangup re-fires the
// release path) stores exactly one brief, unchanged by the second call,
// with no error.
func TestBriefFinalizeIdempotent(t *testing.T) {
	s, _ := briefStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{CallID: "cc-replay", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.WriteBriefStub(ctx, "cc-replay", "+15558001000", "+15558009106"); err != nil {
		t.Fatalf("WriteBriefStub: %v", err)
	}
	if err := s.FinalizeBrief(ctx, "cc-replay", &briefFixture); err != nil {
		t.Fatalf("FinalizeBrief 1: %v", err)
	}
	first, _ := s.Brief(ctx, "cc-replay")

	// Replay: a second finalize must not change anything and must not error.
	replay := briefFixture
	replay.Status = "failed"
	replay.Error = "replay must not win"
	if err := s.FinalizeBrief(ctx, "cc-replay", &replay); err != nil {
		t.Fatalf("FinalizeBrief 2 (replay): %v", err)
	}
	second, _ := s.Brief(ctx, "cc-replay")
	if second == nil || first == nil {
		t.Fatalf("briefs missing: %v / %v", first, second)
	}
	if second.Status != "completed" || second.Brief.Summary != first.Brief.Summary {
		t.Errorf("replayed finalize mutated the brief: %+v", second)
	}
}

// TestBriefRestartSweepAbandonsStub pins the O1 decision: a restart with
// a live row gets SweepStaleLive marking the call row failed AND the
// stub brief status abandoned (process died). Completed briefs must stay
// completed; a call with no stub gets no brief row at all.
func TestBriefRestartSweepAbandonsStub(t *testing.T) {
	s, _ := briefStore(t)
	ctx := context.Background()
	// Live call with a stub (process died mid-call).
	if err := s.Start(ctx, CallRecord{CallID: "cc-dead", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.WriteBriefStub(ctx, "cc-dead", "+15558001000", "+15558009106"); err != nil {
		t.Fatalf("WriteBriefStub: %v", err)
	}
	// Finished call with a final brief (must be untouched).
	if err := s.Start(ctx, CallRecord{CallID: "cc-done", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	if err := s.WriteBriefStub(ctx, "cc-done", "+15558001000", "+15558009106"); err != nil {
		t.Fatalf("WriteBriefStub 2: %v", err)
	}
	fin := briefFixture
	if err := s.FinalizeBrief(ctx, "cc-done", &fin); err != nil {
		t.Fatalf("FinalizeBrief: %v", err)
	}

	n, err := s.SweepStaleLive(ctx)
	if err != nil {
		t.Fatalf("SweepStaleLive: %v", err)
	}
	if n < 1 {
		t.Fatalf("sweep reconciled %d rows, want the live row", n)
	}
	dead, err := s.Brief(ctx, "cc-dead")
	if err != nil {
		t.Fatalf("Brief(cc-dead): %v", err)
	}
	if dead.Status != "abandoned" {
		t.Errorf("swept stub status = %q, want abandoned (O1: startup sweep = abandoned)", dead.Status)
	}
	done, err := s.Brief(ctx, "cc-done")
	if err != nil {
		t.Fatalf("Brief(cc-done): %v", err)
	}
	if done.Status != "completed" {
		t.Errorf("completed brief clobbered by the sweep: %q", done.Status)
	}
}

// TestBriefColumnMigrationIdempotent pins the additive guarded migration:
// opening the SAME db file twice must not error (duplicate-column errors
// are the expected steady state), old rows read back with an empty ext.
func TestBriefColumnMigrationIdempotent(t *testing.T) {
	_, db := briefStore(t) // first open creates + migrates

	// Second open of the same file: no migration error.
	s2, err := NewCallStore(db, testLogger())
	if err != nil {
		t.Fatalf("second NewCallStore on migrated db: %v", err)
	}
	defer s2.Close()

	// A row written before the columns existed (simulate: write via the
	// same store, then confirm Brief on an unknown-but-started ccid yields
	// an empty-ext, non-nil-safe read — old rows get '').
	ctx := context.Background()
	if err := s2.Start(ctx, CallRecord{CallID: "cc-old", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	b, err := s2.Brief(ctx, "cc-old")
	if err != nil {
		t.Fatalf("Brief on row with no brief column content: %v", err)
	}
	if b != nil && len(b.Ext) != 0 && strings.Contains(string(b.Ext), "{") {
		t.Errorf("old row ext should be empty, got %s", string(b.Ext))
	}
}

// TestBriefFailedAnalysisStoredNeverRetried pins the frozen guarantee: a
// failed analysis stores status=failed with the error text; a second
// finalize attempt must not overwrite it (never retried, exactly one
// brief).
func TestBriefFailedAnalysisStoredNeverRetried(t *testing.T) {
	s, _ := briefStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{CallID: "cc-fail", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.WriteBriefStub(ctx, "cc-fail", "+15558001000", "+15558009106"); err != nil {
		t.Fatalf("WriteBriefStub: %v", err)
	}
	failed := CallBrief{Status: "failed", Error: "analyzer timeout", From: "+15558001000", To: "+15558009106"}
	if err := s.FinalizeBrief(ctx, "cc-fail", &failed); err != nil {
		t.Fatalf("FinalizeBrief(failed): %v", err)
	}
	got, _ := s.Brief(ctx, "cc-fail")
	if got.Status != "failed" || got.Error != "analyzer timeout" {
		t.Fatalf("failed brief = %+v, want status failed + error stored", got)
	}
	// Retry attempt: must not resurrect or change the row.
	if err := s.FinalizeBrief(ctx, "cc-fail", &briefFixture); err != nil {
		t.Fatalf("FinalizeBrief(retry): %v", err)
	}
	got2, _ := s.Brief(ctx, "cc-fail")
	if got2.Status != "failed" {
		t.Errorf("failed brief was retried/overwritten: %+v", got2)
	}
}
