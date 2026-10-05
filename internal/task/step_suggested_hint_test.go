package task

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStep_SuggestedNextHintRoundTrip pins the successor-hints storage
// contract (agent-routing tree leaf 03): a finished step's advisory
// suggested_next_hint survives Create, Update, and scan. Without
// persistence the OnJobCompleted adoption would evaporate on every step
// reload, making the hint dead for any future reader.
func TestStep_SuggestedNextHintRoundTrip(t *testing.T) {
	store := newInteractiveStepStore(t)

	step := NewTaskStep("task-hint-1", "step with successor hint", 0)
	step.SuggestedNextHint = "debugger"
	if err := store.Create(step); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.SuggestedNextHint != "debugger" {
		t.Errorf("after Create: SuggestedNextHint = %q, want %q", got.SuggestedNextHint, "debugger")
	}

	// Update path (evidence persistence etc.) must not drop the hint, and
	// a new value must persist.
	got.SuggestedNextHint = "reviewer"
	if err := store.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.SuggestedNextHint != "reviewer" {
		t.Errorf("after Update: SuggestedNextHint = %q, want %q", got2.SuggestedNextHint, "reviewer")
	}

	// GetByJobID (the OnJobCompleted lookup path) scans the same columns.
	got2.JobID = "job-hint-1"
	if err := store.Update(got2); err != nil {
		t.Fatalf("update job id: %v", err)
	}
	byJob, err := store.GetByJobID("job-hint-1")
	if err != nil {
		t.Fatalf("get by job id: %v", err)
	}
	if byJob.SuggestedNextHint != "reviewer" {
		t.Errorf("after GetByJobID: SuggestedNextHint = %q, want %q", byJob.SuggestedNextHint, "reviewer")
	}

	// Zero-value stays empty, not stale.
	orphan := NewTaskStep("task-hint-1", "step without hint", 1)
	if orphan.SuggestedNextHint != "" {
		t.Errorf("new step SuggestedNextHint should default to empty, got %q", orphan.SuggestedNextHint)
	}
}

// TestStep_SuggestedNextHintOmitEmpty pins the omitempty json tag: an empty
// hint must not serialize as "suggested_next_hint":"" — advisory channels
// that leak empty keys defeat every "is the hint present" check downstream.
func TestStep_SuggestedNextHintOmitEmpty(t *testing.T) {
	step := NewTaskStep("task-hint-2", "no hint step", 0)
	data, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "suggested_next_hint") {
		t.Errorf("empty SuggestedNextHint serialized into JSON: %s", data)
	}

	step.SuggestedNextHint = "planner"
	data, err = json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal with hint: %v", err)
	}
	if !strings.Contains(string(data), `"suggested_next_hint":"planner"`) {
		t.Errorf("set SuggestedNextHint missing from JSON: %s", data)
	}
}
