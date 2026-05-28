package agent

import (
	"testing"
)

func TestNewPairSession(t *testing.T) {
	ps := NewPairSession("task-1", "implement auth module", "coder", "planner", 5)

	if ps.TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", ps.TaskID)
	}
	if ps.ActorAgentID != "coder" {
		t.Errorf("expected actor 'coder', got %q", ps.ActorAgentID)
	}
	if ps.ReviewerAgentID != "planner" {
		t.Errorf("expected reviewer 'planner', got %q", ps.ReviewerAgentID)
	}
	if ps.MaxRounds != 5 {
		t.Errorf("expected max_rounds 5, got %d", ps.MaxRounds)
	}
	if ps.State != PairSessionActive {
		t.Errorf("expected state active, got %q", ps.State)
	}
	if ps.Context.OriginalSpec != "implement auth module" {
		t.Errorf("expected spec 'implement auth module', got %q", ps.Context.OriginalSpec)
	}
	if len(ps.StepIDs) != 0 {
		t.Errorf("expected no step IDs, got %d", len(ps.StepIDs))
	}
}

func TestPairSession_CurrentRound(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)

	if ps.CurrentRound() != 1 {
		t.Errorf("expected round 1, got %d", ps.CurrentRound())
	}

	ps.Context.Attempts = append(ps.Context.Attempts, &Attempt{Round: 1})
	if ps.CurrentRound() != 2 {
		t.Errorf("expected round 2 after 1 attempt, got %d", ps.CurrentRound())
	}
}

func TestPairSession_IsExhausted(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 2)

	if ps.IsExhausted() {
		t.Error("should not be exhausted at round 1")
	}

	ps.Context.Attempts = append(ps.Context.Attempts, &Attempt{Round: 1})
	if ps.IsExhausted() {
		t.Error("should not be exhausted at round 2 (max=2)")
	}

	ps.Context.Attempts = append(ps.Context.Attempts, &Attempt{Round: 2})
	if !ps.IsExhausted() {
		t.Error("should be exhausted after 2 attempts (max=2)")
	}
}

func TestPairSession_SetCriteria(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)
	ps.SetCriteria([]string{"write tests", "handle errors", "add docs"})

	if len(ps.Context.PendingCriteria) != 3 {
		t.Fatalf("expected 3 pending criteria, got %d", len(ps.Context.PendingCriteria))
	}
	if ps.Context.PendingCriteria[0] != "write tests" {
		t.Errorf("expected first criterion 'write tests', got %q", ps.Context.PendingCriteria[0])
	}
	if len(ps.Context.AcceptedCriteria) != 0 {
		t.Errorf("expected 0 accepted criteria, got %d", len(ps.Context.AcceptedCriteria))
	}
}

func TestPairContext_HasConverged(t *testing.T) {
	pc := &PairContext{
		PendingCriteria: []string{"a", "b"},
	}
	if pc.HasConverged() {
		t.Error("should not be converged with pending criteria")
	}

	pc.PendingCriteria = nil
	pc.AcceptedCriteria = []string{"a", "b"}
	if !pc.HasConverged() {
		t.Error("should be converged when no pending criteria remain")
	}
}

func TestPairContext_ActorPrompt(t *testing.T) {
	pc := &PairContext{
		OriginalSpec:     "implement login",
		PendingCriteria:  []string{"validate input"},
		AcceptedCriteria: []string{"create route"},
		Attempts: []*Attempt{
			{
				Round:       1,
				ActorOutput: "created route /login",
				Review: &ReviewResult{
					Status:   ReviewRejected,
					Feedback: "missing input validation",
					Issues:   []string{"no email validation"},
				},
			},
		},
	}

	prompt := pc.ActorPrompt()
	if prompt == "" {
		t.Fatal("actor prompt should not be empty")
	}
	// Should contain spec
	if !contains(prompt, "implement login") {
		t.Error("actor prompt should contain original spec")
	}
	// Should contain accepted criteria
	if !contains(prompt, "create route") {
		t.Error("actor prompt should contain accepted criteria")
	}
	// Should contain pending criteria
	if !contains(prompt, "validate input") {
		t.Error("actor prompt should contain pending criteria")
	}
	// Should contain reviewer feedback
	if !contains(prompt, "missing input validation") {
		t.Error("actor prompt should contain reviewer feedback")
	}
}

func TestPairContext_ReviewerPrompt(t *testing.T) {
	pc := &PairContext{
		OriginalSpec:    "implement login",
		PendingCriteria: []string{"validate input"},
	}

	prompt := pc.ReviewerPrompt("here is the login handler code")
	if prompt == "" {
		t.Fatal("reviewer prompt should not be empty")
	}
	if !contains(prompt, "here is the login handler code") {
		t.Error("reviewer prompt should contain actor output")
	}
	if !contains(prompt, "validate input") {
		t.Error("reviewer prompt should contain pending criteria")
	}
}

func TestAttempt_Satisfied(t *testing.T) {
	a := &Attempt{Review: &ReviewResult{Status: ReviewApproved}}
	if !a.Satisfied() {
		t.Error("approved attempt should be satisfied")
	}

	b := &Attempt{Review: &ReviewResult{Status: ReviewRejected}}
	if b.Satisfied() {
		t.Error("rejected attempt should not be satisfied")
	}

	c := &Attempt{Review: nil}
	if c.Satisfied() {
		t.Error("attempt without review should not be satisfied")
	}
}

func TestPairSession_OwnsStep(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)

	if ps.OwnsStep("step-1") {
		t.Error("should not own step before it is added")
	}

	ps.AddStepID("step-1")
	if !ps.OwnsStep("step-1") {
		t.Error("should own step after adding")
	}
	if ps.OwnsStep("step-2") {
		t.Error("should not own unadded step")
	}
}

func TestPairSession_StateTransitions(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)

	if ps.State.IsTerminal() {
		t.Error("active session should not be terminal")
	}

	ps.MarkConverged()
	if ps.State != PairSessionConverged {
		t.Errorf("expected converged, got %q", ps.State)
	}
	if !ps.State.IsTerminal() {
		t.Error("converged should be terminal")
	}

	ps2 := NewPairSession("task-2", "spec", "coder", "planner", 3)
	ps2.MarkExhausted()
	if ps2.State != PairSessionExhausted {
		t.Errorf("expected exhausted, got %q", ps2.State)
	}

	ps3 := NewPairSession("task-3", "spec", "coder", "planner", 3)
	ps3.MarkFailed()
	if ps3.State != PairSessionFailed {
		t.Errorf("expected failed, got %q", ps3.State)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
