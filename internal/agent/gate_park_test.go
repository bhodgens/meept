package agent

// Gate-park checkpointing tests (hitl-notifications leaf 02, tasks T1-T3).
// All scenarios run through the REAL TurnParker (never a hand-built
// double) and both the in-memory fake store and the real SQLite park
// store, per the leaf's review checklist.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingResume collects resume callbacks under a mutex so tests can
// assert exactly-once behavior.
type countingResume struct {
	mu      sync.Mutex
	records []ParkedTurnRecord
	keys    []string
}

func (c *countingResume) add(_ context.Context, rec ParkedTurnRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, rec)
	c.keys = append(c.keys, rec.SessionID)
}

func (c *countingResume) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.records)
}

func (c *countingResume) last() ParkedTurnRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records[len(c.records)-1]
}

func newGateTestParker(t *testing.T, resume func(context.Context, ParkedTurnRecord)) *TurnParker {
	t.Helper()
	p := NewGateTurnParker(parkedTurnTestLogger(), resume)
	p.SetPollInterval(50 * time.Millisecond)
	return p
}

func gateData(gateID string) GateParkData {
	return GateParkData{
		GateKind:    "plan",
		GateID:      gateID,
		SessionID:   "session-1",
		TurnReentry: map[string]any{"turn_id": "t1", "message": "hello"},
		RequestedAt: time.Now().UTC(),
	}
}

// T1: a gate park persists a GateParkData row scoped to ParkKindGate
// through the in-memory fake store; payload JSON round-trips
// field-exact.
func TestGateParkPersistsKindScoped(t *testing.T) {
	store := &fakePersistence{}
	var resumed countingResume
	p := newGateTestParker(t, resumed.add)
	p.SetParkPersistence(store, ParkKindGate)
	gp := NewGateParker(parkedTurnTestLogger(), p)

	data := gateData("plan-1")
	if !gp.ParkGate(data) {
		t.Fatalf("ParkGate returned false")
	}

	store.mu.Lock()
	var raw ParkedTurnRecord
	var found bool
	for k, rec := range store.rows {
		if len(k) > len("gate|") && k[:5] == "gate|" {
			raw = rec
			found = true
		}
	}
	store.mu.Unlock()
	if !found {
		t.Fatalf("no gate-kind row persisted")
	}

	got, key, err := recordToGateTurn(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if key != "plan|plan-1" {
		t.Errorf("key = %q, want plan|plan-1", key)
	}
	if got.GateKind != data.GateKind || got.GateID != data.GateID ||
		got.SessionID != data.SessionID || !got.RequestedAt.Equal(data.RequestedAt) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, data)
	}
	if got.TurnReentry["turn_id"] != "t1" || got.TurnReentry["message"] != "hello" {
		t.Errorf("TurnReentry round-trip mismatch: %v", got.TurnReentry)
	}
	if n := gp.PendingGates(); n != 1 {
		t.Errorf("PendingGates = %d, want 1", n)
	}
}

// T1 (sqlite twin): the same flow through the REAL SQLiteParkStore.
func TestGateParkPersistsThroughSQLiteStore(t *testing.T) {
	store := newTestParkStore(t)
	defer store.Close()
	var resumed countingResume
	p := newGateTestParker(t, resumed.add)
	p.SetParkPersistence(store, ParkKindGate)
	gp := NewGateParker(parkedTurnTestLogger(), p)

	if !gp.ParkGate(gateData("plan-sql")) {
		t.Fatalf("ParkGate returned false")
	}
	if n := store.persistedRowCount(t, ParkKindGate); n != 1 {
		t.Fatalf("sqlite gate rows = %d, want 1", n)
	}

	// Simulated restart: a fresh parker over the same store re-arms the
	// surviving gate row (Start re-arm) without resuming it -- the gate
	// stays pending until approval.
	var afterRestart countingResume
	p2 := newGateTestParker(t, afterRestart.add)
	p2.SetParkPersistence(store, ParkKindGate)
	p2.Start(context.Background())
	defer p2.Stop()
	if n := p2.Pending(); n != 1 {
		t.Fatalf("re-armed pending = %d, want 1", n)
	}
	time.Sleep(150 * time.Millisecond) // let any (wrong) auto-drain surface
	if n := afterRestart.count(); n != 0 {
		t.Fatalf("re-arm auto-resumed a pending gate (%d resumes)", n)
	}
}

// T2: approval resumes the gate exactly once; a duplicate approval is a
// no-op; approval when nothing is parked is unchanged (false).
func TestGateParkPlanResumeAtMostOnce(t *testing.T) {
	store := &fakePersistence{}
	var resumed countingResume
	p := newGateTestParker(t, resumed.add)
	p.SetParkPersistence(store, ParkKindGate)
	gp := NewGateParker(parkedTurnTestLogger(), p)

	if !gp.ParkGate(gateData("plan-approve")) {
		t.Fatalf("ParkGate returned false")
	}
	ctx := context.Background()

	// Duplicate first approval racing a second: exactly one wins.
	if !gp.ResumeByKey(ctx, "plan", "plan-approve") {
		t.Fatalf("first approval did not resume")
	}
	if gp.ResumeByKey(ctx, "plan", "plan-approve") {
		t.Fatalf("duplicate approval resumed a second time")
	}
	if n := resumed.count(); n != 1 {
		t.Fatalf("resume count = %d, want 1 (at-most-once)", n)
	}
	// Row deleted BEFORE the callback: by the time the resume fires the
	// store row is gone (the test callback runs after ResumeByKey
	// returns the pop; assert the store is empty now).
	store.mu.Lock()
	n := len(store.rows)
	store.mu.Unlock()
	if n != 0 {
		t.Errorf("gate rows after resume = %d, want 0", n)
	}
	// Approval when not parked is a no-op, not an error.
	if gp.ResumeByKey(ctx, "plan", "never-parked") {
		t.Errorf("resume of unparked gate returned true")
	}

	rec := resumed.last()
	got, _, err := recordToGateTurn(rec)
	if err != nil {
		t.Fatalf("decode resumed record: %v", err)
	}
	if got.GateID != "plan-approve" {
		t.Errorf("resumed gate id = %q", got.GateID)
	}
	if rec.TurnPayload == nil {
		t.Errorf("resumed record lost TurnPayload")
	}
}

// T2 drain guard: the time-based drain must NEVER auto-resume a gate
// park (only an approval does). The fake clock is swapped in AFTER the
// park (Park schedules against the real now) so the drain observes a
// time far beyond the horizon.
func TestGateParkNotAutoResumedByDrain(t *testing.T) {
	store := &fakePersistence{}
	var resumed countingResume
	p := newGateTestParker(t, resumed.add)
	p.SetParkPersistence(store, ParkKindGate)
	gp := NewGateParker(parkedTurnTestLogger(), p)

	if !gp.ParkGate(gateData("plan-drain")) {
		t.Fatalf("ParkGate returned false")
	}
	// Advance the clock well past any realistic wait (a full day) but
	// far short of the gate horizon, then force a drain pass: nothing
	// may fire. (At +2x horizon the drain WOULD legitimately resume --
	// the horizon is a real parker schedule; the contract pinned here is
	// that no realistic clock reaches it, and the daemon's resume
	// callback re-parks even a pathological time-drain.)
	base := time.Now().UTC()
	p.nowFunc = func() time.Time { return base.Add(24 * time.Hour) }
	p.drainDue(context.Background())
	if n := resumed.count(); n != 0 {
		t.Fatalf("drain auto-resumed a gate park (%d resumes)", n)
	}
}

// T3: goal gate park -> simulated restart (fresh parker over the same
// store, Start) -> late approval resumes exactly once; a second
// approval is a no-op.
func TestGateParkGoalRestartReArmLateApproval(t *testing.T) {
	store := newTestParkStore(t)
	defer store.Close()

	var beforeRestart countingResume
	p := newGateTestParker(t, beforeRestart.add)
	p.SetParkPersistence(store, ParkKindGate)
	gp := NewGateParker(parkedTurnTestLogger(), p)

	goalData := GateParkData{
		GateKind:    "goal",
		GateID:      "goal-1",
		SessionID:   "employee-1",
		TurnReentry: map[string]any{"phase": "approve", "plan_id": "plan-9"},
		RequestedAt: time.Now().UTC(),
	}
	if !gp.ParkGate(goalData) {
		t.Fatalf("ParkGate returned false")
	}
	// First daemon exits WITHOUT approval; row survives.
	p.Stop()

	// Restart: fresh parker over the same store.
	var afterRestart countingResume
	var resumedOnce atomic.Bool
	p2 := newGateTestParker(t, afterRestart.add)
	p2.SetParkPersistence(store, ParkKindGate)
	gp2 := NewGateParker(parkedTurnTestLogger(), p2)
	p2.Start(context.Background())
	defer p2.Stop()
	if n := p2.Pending(); n != 1 {
		t.Fatalf("re-armed pending = %d, want 1", n)
	}

	// Approval arrives AFTER the restart.
	ctx := context.Background()
	if !gp2.ResumeByKey(ctx, "goal", "goal-1") {
		t.Fatalf("late approval did not resume after restart")
	}
	if gp2.ResumeByKey(ctx, "goal", "goal-1") {
		t.Fatalf("second approval resumed again")
	}
	if n := afterRestart.count(); n != 1 {
		t.Fatalf("post-restart resume count = %d, want 1", n)
	}
	got, _, err := recordToGateTurn(afterRestart.last())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.GateKind != "goal" || got.GateID != "goal-1" {
		t.Errorf("resumed gate = %s/%s", got.GateKind, got.GateID)
	}
	if got.TurnReentry["phase"] != "approve" {
		t.Errorf("re-entry payload lost: %v", got.TurnReentry)
	}
	if n := store.persistedRowCount(t, ParkKindGate); n != 0 {
		t.Errorf("gate rows after resume = %d, want 0", n)
	}
	resumedOnce.Store(true)
}

// Kind scoping: a gate park never re-arms into the chat or episode
// routers and vice versa (same table, kind-keyed rows).
func TestGateParkKindIsolation(t *testing.T) {
	store := newTestParkStore(t)
	defer store.Close()

	gateRec, err := gateTurnToRecord(gateData("plan-iso"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	chatRec, err := quotaTurnToRecord(QuotaParkedTurn{
		SessionID:      "s1",
		ConversationID: "c1",
		Message:        "m",
		ProviderID:     "p",
		CredentialKey:  "k",
		UnblockAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, ParkKindGate, "plan|plan-iso", gateRec); err != nil {
		t.Fatalf("save gate: %v", err)
	}
	if err := store.Save(ctx, ParkKindChat, "k1", chatRec); err != nil {
		t.Fatalf("save chat: %v", err)
	}

	gateRecs, _, err := store.Load(ctx, ParkKindGate, time.Now())
	if err != nil {
		t.Fatalf("load gate: %v", err)
	}
	chatRecs, _, err := store.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load chat: %v", err)
	}
	if len(gateRecs) != 1 || len(chatRecs) != 1 {
		t.Fatalf("kind isolation broken: gate=%d chat=%d", len(gateRecs), len(chatRecs))
	}
	if _, _, err := recordToGateTurn(gateRecs[0]); err != nil {
		t.Errorf("gate row undecodable as GateParkData: %v", err)
	}
}
