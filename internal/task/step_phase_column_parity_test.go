package task

// M11 of the 2026-10-05 bughunt: StepStore.UpdatePhaseSteps re-writes EVERY
// column, but its SET list omitted `suggested_next_hint` while StepStore.Update
// included it. A hint adopted on one path therefore vanished on the phase path
// (internal/agent/orchestrator_phases.go calls UpdatePhaseSteps on every phase
// transition) with no error anywhere — a one-sided 28-placeholder/28-argument
// omission.
//
// These pins are two-sided:
//   - the M11 behavioral pin: a hint survives UpdatePhaseSteps;
//   - a structural parity pin: the two SET lists must name the SAME columns,
//     so the next added column cannot be added to one and forgotten in the
//     other. Drift here is silent by construction — that is the whole defect
//     class.

import (
	"regexp"
	"strings"
	"testing"
)

// setListColumns extracts the assigned column names from an
// "UPDATE task_steps SET a = ?, b = ? ... WHERE" statement.
var setListColumns = regexp.MustCompile(`(?s)UPDATE task_steps\s+SET(.*?)WHERE id = \?`)

func parseSetColumns(t *testing.T, query string) []string {
	t.Helper()
	m := setListColumns.FindStringSubmatch(query)
	if m == nil {
		t.Fatalf("no UPDATE task_steps SET block in:\n%s", query)
	}
	var cols []string
	for part := range strings.SplitSeq(m[1], ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if !ok {
			t.Fatalf("malformed SET entry %q", part)
		}
		cols = append(cols, strings.TrimSpace(name))
	}
	return cols
}

// TestStepStore_UpdateAndUpdatePhaseStepsAgreeOnColumns pins column parity
// between the single-step and batch write paths. The runtime consequence of
// drift is a column silently ignored by one path, so this must be a structural
// equality assertion, not a spot check of the column that drifted today.
func TestStepStore_UpdateAndUpdatePhaseStepsAgreeOnColumns(t *testing.T) {
	updateCols := parseSetColumns(t, stepUpdateQuery)
	phaseCols := parseSetColumns(t, stepPhaseUpdateQuery)

	inUpdate := make(map[string]bool, len(updateCols))
	for _, c := range updateCols {
		inUpdate[c] = true
	}
	inPhase := make(map[string]bool, len(phaseCols))
	for _, c := range phaseCols {
		inPhase[c] = true
	}

	for _, c := range updateCols {
		if !inPhase[c] {
			t.Errorf("column %q is written by Update but NOT by UpdatePhaseSteps — the batch path re-writes every column, so it is silently dropped there", c)
		}
	}
	for _, c := range phaseCols {
		if !inUpdate[c] {
			t.Errorf("column %q is written by UpdatePhaseSteps but NOT by Update", c)
		}
	}

	// The bind order is positional, so equal column SETS are not enough: the
	// placeholder count must equal the argument count or every value lands in
	// the wrong column. The pre-M11 statements were 28/28 (consistent but
	// INCOMPLETE); after adding suggested_next_hint both must be 29/29.
	if got, want := countPlaceholders(stepUpdateQuery), countPlaceholders(stepPhaseUpdateQuery); got != want {
		t.Errorf("placeholder counts differ: Update = %d, UpdatePhaseSteps = %d", got, want)
	}
	// Sanity-pin the post-fix width so a future "trim the fat" edit that drops
	// columns from BOTH statements still trips this test.
	if got := countPlaceholders(stepUpdateQuery); got != 29 {
		t.Errorf("stepUpdateQuery placeholders = %d, want 29 (28 pre-M11 + suggested_next_hint)", got)
	}
	if got := countPlaceholders(stepPhaseUpdateQuery); got != 29 {
		t.Errorf("stepPhaseUpdateQuery placeholders = %d, want 29 (28 pre-M11 + suggested_next_hint)", got)
	}
}

// countPlaceholders counts the `?` bind placeholders in a statement.
func countPlaceholders(query string) int {
	return strings.Count(query, "?")
}

// TestStep_SuggestedNextHintSurvivesUpdatePhaseSteps is the M11 behavioral pin:
// a hint set on a step, then written through the phase-batch path, must still
// be there afterwards. Before the fix this asserted the opposite.
func TestStep_SuggestedNextHintSurvivesUpdatePhaseSteps(t *testing.T) {
	store := newInteractiveStepStore(t)

	step := NewTaskStep("task-hint-phase", "phase step with a hint", 0)
	step.State = StepScheduled
	step.SuggestedNextHint = "debugger"
	step.Phase = "phase-1"
	if err := store.Create(step); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Re-read so the batch write starts from a store-shaped value (same shape
	// orchestrator_phases.go passes in).
	live, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	live.SuggestedNextHint = "reviewer"
	live.ConversationID = "phase-phase-1-" + live.ID
	live.AccumulatedContext = "phase startup context"

	if err := store.UpdatePhaseSteps([]*TaskStep{live}); err != nil {
		t.Fatalf("UpdatePhaseSteps: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get after phase update: %v", err)
	}
	if got.SuggestedNextHint != "reviewer" {
		t.Errorf("SuggestedNextHint after UpdatePhaseSteps = %q, want %q (the batch path dropped it)",
			got.SuggestedNextHint, "reviewer")
	}
	// The neighbouring batch columns must still work (the fix did not shift
	// the bind order).
	if got.ConversationID != live.ConversationID {
		t.Errorf("ConversationID after UpdatePhaseSteps = %q, want %q", got.ConversationID, live.ConversationID)
	}
	if got.AccumulatedContext != live.AccumulatedContext {
		t.Errorf("AccumulatedContext after UpdatePhaseSteps = %q, want %q",
			got.AccumulatedContext, live.AccumulatedContext)
	}

	// And a cleared hint clears (no stale value): the batch path writes the
	// column, so NULL is reachable.
	live.SuggestedNextHint = ""
	if err := store.UpdatePhaseSteps([]*TaskStep{live}); err != nil {
		t.Fatalf("UpdatePhaseSteps (clear hint): %v", err)
	}
	cleared, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get after clearing hint: %v", err)
	}
	if cleared.SuggestedNextHint != "" {
		t.Errorf("SuggestedNextHint after clearing = %q, want empty", cleared.SuggestedNextHint)
	}
}
