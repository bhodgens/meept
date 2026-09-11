package metrics

// Outcome-capture store tests (classifier-outcome-loop leaf 03): the six
// spec cases for ResolvePendingOutcome (ok / corrected / window-expired /
// non-classified-source / failed_replan / no-op). Mirrors the NewStore
// construction used by store_test.go (batching disabled so RecordDispatch
// writes land immediately).

import (
	"path/filepath"
	"testing"
	"time"
)

func newOutcomeTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; writes are direct
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// recordOutcomeDispatch inserts one pending dispatch row directly.
func recordOutcomeDispatch(t *testing.T, s *Store, e DispatchEntry) {
	t.Helper()
	s.RecordDispatch(e)
}

// getOutcomeRow fetches the outcome/corrected_agent for a given turn.
func getOutcomeRow(t *testing.T, s *Store, sessionID string, turnNo int) (outcome, corrected string) {
	t.Helper()
	err := s.db.Get(&outcome,
		`SELECT outcome FROM dispatch_log WHERE session_id = ? AND turn_no = ?`,
		sessionID, turnNo)
	if err != nil {
		t.Fatalf("get outcome turn %d: %v", turnNo, err)
	}
	if err := s.db.Get(&corrected,
		`SELECT corrected_agent FROM dispatch_log WHERE session_id = ? AND turn_no = ?`,
		sessionID, turnNo); err != nil {
		t.Fatalf("get corrected_agent turn %d: %v", turnNo, err)
	}
	return outcome, corrected
}

// Case 1: same session, same agent, 1 turn apart -> prior row 'ok',
// corrected_agent ”.
func TestResolvePendingOutcome_SameAgentOk(t *testing.T) {
	store := newOutcomeTestStore(t)

	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-ok", AgentID: "llm/code",
		ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})

	if err := store.ResolvePendingOutcome("sess-ok", 2, "llm/code", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome: %v", err)
	}

	outcome, corrected := getOutcomeRow(t, store, "sess-ok", 1)
	if outcome != "ok" {
		t.Errorf("prior outcome = %q, want ok", outcome)
	}
	if corrected != "" {
		t.Errorf("corrected_agent = %q, want empty", corrected)
	}
}

// Case 2: same session, DIFFERENT agents, 1 turn apart -> prior
// 'corrected' with corrected_agent = second agent.
func TestResolvePendingOutcome_DifferentAgentCorrected(t *testing.T) {
	store := newOutcomeTestStore(t)

	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-corr", AgentID: "llm/code",
		ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})

	if err := store.ResolvePendingOutcome("sess-corr", 2, "llm/review", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome: %v", err)
	}

	outcome, corrected := getOutcomeRow(t, store, "sess-corr", 1)
	if outcome != "corrected" {
		t.Errorf("prior outcome = %q, want corrected", outcome)
	}
	if corrected != "llm/review" {
		t.Errorf("corrected_agent = %q, want llm/review", corrected)
	}
}

// Case 3: different agents but 5 turns apart (window 3) -> prior 'ok'.
func TestResolvePendingOutcome_WindowExpired(t *testing.T) {
	store := newOutcomeTestStore(t)

	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-window", AgentID: "llm/code",
		ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})

	if err := store.ResolvePendingOutcome("sess-window", 6, "llm/review", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome: %v", err)
	}

	outcome, corrected := getOutcomeRow(t, store, "sess-window", 1)
	if outcome != "ok" {
		t.Errorf("prior outcome = %q, want ok (outside window)", outcome)
	}
	if corrected != "" {
		t.Errorf("corrected_agent = %q, want empty", corrected)
	}
}

// Case 4: prior row came from a non-classified path (classifier_method =
// short_message_guard) -> stays 'pending' (non-classified rows are never
// correction sources).
func TestResolvePendingOutcome_NonClassifiedSourceStaysPending(t *testing.T) {
	store := newOutcomeTestStore(t)

	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-guard", AgentID: "chat",
		ClassifierMethod: "", TurnNo: 1, Outcome: "pending", // non-classified path
	})

	if err := store.ResolvePendingOutcome("sess-guard", 2, "llm/code", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome: %v", err)
	}

	outcome, corrected := getOutcomeRow(t, store, "sess-guard", 1)
	if outcome != "pending" {
		t.Errorf("prior outcome = %q, want pending (non-classified source)", outcome)
	}
	if corrected != "" {
		t.Errorf("corrected_agent = %q, want empty", corrected)
	}
}

// Case 5: MarkTaskFailedReplan flips only the matching task_id's pending row.
func TestMarkTaskFailedReplan_TargetsMatchingTask(t *testing.T) {
	store := newOutcomeTestStore(t)

	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-fail", TaskID: "task-A",
		ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})
	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-fail", TaskID: "task-B",
		ClassifierMethod: "llm", TurnNo: 2, Outcome: "pending",
	})
	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-fail", TaskID: "task-C",
		ClassifierMethod: "llm", TurnNo: 3, Outcome: "ok", // already resolved
	})

	if err := store.MarkTaskFailedReplan("task-B"); err != nil {
		t.Fatalf("MarkTaskFailedReplan: %v", err)
	}

	var outcomeA, outcomeB, outcomeC string
	for _, tc := range []struct {
		taskID string
		want   *string
	}{
		{"task-A", &outcomeA}, {"task-B", &outcomeB}, {"task-C", &outcomeC},
	} {
		if err := store.db.Get(tc.want,
			`SELECT outcome FROM dispatch_log WHERE task_id = ?`, tc.taskID); err != nil {
			t.Fatalf("get outcome for %s: %v", tc.taskID, err)
		}
	}
	if outcomeA != "pending" {
		t.Errorf("task-A outcome = %q, want pending (untouched)", outcomeA)
	}
	if outcomeB != "failed_replan" {
		t.Errorf("task-B outcome = %q, want failed_replan", outcomeB)
	}
	if outcomeC != "ok" {
		t.Errorf("task-C outcome = %q, want ok (already resolved, not flipped)", outcomeC)
	}
}

// Case 6: no prior pending row -> ResolvePendingOutcome is a no-op (no error).
func TestResolvePendingOutcome_NoPriorRowNoOp(t *testing.T) {
	store := newOutcomeTestStore(t)

	// Empty session: nothing to resolve.
	if err := store.ResolvePendingOutcome("sess-empty", 1, "llm/code", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome on empty session: %v", err)
	}

	// Same session but the only prior row is already resolved -> no-op.
	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-resolved", AgentID: "llm/code",
		ClassifierMethod: "llm", TurnNo: 1, Outcome: "ok",
	})
	if err := store.ResolvePendingOutcome("sess-resolved", 2, "llm/review", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome with resolved prior: %v", err)
	}

	var count int
	if err := store.db.Get(&count,
		`SELECT COUNT(*) FROM dispatch_log WHERE session_id = 'sess-resolved' AND outcome = 'corrected'`); err != nil {
		t.Fatalf("count corrected rows: %v", err)
	}
	if count != 0 {
		t.Errorf("resolved prior row was re-resolved: %d corrected rows, want 0", count)
	}
}

// Supplement to case 4: a non-classified CURRENT dispatch (empty agentID)
// resolves an existing classified prior row to 'ok' only -- never 'corrected'
// -- because its agent comparison is meaningless.
func TestResolvePendingOutcome_NonClassifiedCurrentResolvesOkOnly(t *testing.T) {
	store := newOutcomeTestStore(t)

	recordOutcomeDispatch(t, store, DispatchEntry{
		SessionID: "sess-nonclass", AgentID: "llm/code",
		ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})

	// The current dispatch came from a non-classified path: agentID "".
	if err := store.ResolvePendingOutcome("sess-nonclass", 2, "", 3); err != nil {
		t.Fatalf("ResolvePendingOutcome: %v", err)
	}

	outcome, corrected := getOutcomeRow(t, store, "sess-nonclass", 1)
	if outcome != "ok" {
		t.Errorf("prior outcome = %q, want ok", outcome)
	}
	if corrected != "" {
		t.Errorf("corrected_agent = %q, want empty", corrected)
	}
}
