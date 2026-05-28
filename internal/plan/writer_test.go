package plan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWritePlanMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-plan.md")

	plan := newTestPlanForWriter()
	phases := []ParsedPhase{
		{
			Name:     "Design",
			Sequence: 1,
			State:    PhasePending,
			Steps: []ParsedStep{
				{Number: 1, Description: "Analyze requirements", State: StepStatusPending},
				{Number: 2, Description: "Create design doc", State: StepStatusPending},
			},
		},
		{
			Name:     "Implementation",
			Sequence: 2,
			State:    PhasePending,
			Steps: []ParsedStep{
				{Number: 3, Description: "Write code", State: StepStatusPending},
				{Number: 4, Description: "Write tests", State: StepStatusPending, DependsOn: []int{3}},
			},
		},
	}

	if err := WritePlanMarkdown(path, plan, phases); err != nil {
		t.Fatalf("WritePlanMarkdown returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}
	content := string(data)

	// Verify markdown structure.
	if !strings.Contains(content, "# Plan: Test Plan") {
		t.Error("Missing plan title heading")
	}
	if !strings.Contains(content, "## Meta") {
		t.Error("Missing Meta heading")
	}
	if !strings.Contains(content, "- plan_id: "+plan.ID) {
		t.Error("Missing plan_id in meta")
	}
	if !strings.Contains(content, "- project: test-project") {
		t.Error("Missing project in meta")
	}
	if !strings.Contains(content, "- status: planning") {
		t.Error("Missing status in meta")
	}
	if !strings.Contains(content, "## Summary") {
		t.Error("Missing Summary heading")
	}
	if !strings.Contains(content, "A test plan description") {
		t.Error("Missing summary content")
	}
	if !strings.Contains(content, "## Phase 1: Design [pending]") {
		t.Error("Missing Phase 1 heading")
	}
	if !strings.Contains(content, "## Phase 2: Implementation [pending]") {
		t.Error("Missing Phase 2 heading")
	}
	if !strings.Contains(content, "1. Analyze requirements [pending]") {
		t.Error("Missing step 1")
	}
	if !strings.Contains(content, "4. Write tests [pending] (depends: 3)") {
		t.Error("Missing step 4 with dependency")
	}
	if !strings.Contains(content, "## Notes") {
		t.Error("Missing Notes heading")
	}
}

func TestUpdatePlanStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-plan.md")

	// Write initial plan with status "planning".
	plan := newTestPlanForWriter()
	phases := []ParsedPhase{
		{
			Name:     "Design",
			Sequence: 1,
			State:    PhasePending,
			Steps: []ParsedStep{
				{Number: 1, Description: "Analyze requirements", State: StepStatusPending},
				{Number: 2, Description: "Create design doc", State: StepStatusPending},
				{Number: 3, Description: "Review design", State: StepStatusPending},
			},
		},
		{
			Name:     "Build",
			Sequence: 2,
			State:    PhasePending,
			Steps: []ParsedStep{
				{Number: 4, Description: "Write code", State: StepStatusPending},
				{Number: 5, Description: "Write tests", State: StepStatusPending},
			},
		},
	}

	if err := WritePlanMarkdown(path, plan, phases); err != nil {
		t.Fatalf("WritePlanMarkdown returned error: %v", err)
	}

	// Update to executing with phase 1 in-progress (2 of 3 steps done).
	planPhases := []PlanPhase{
		{Sequence: 1, TotalSteps: 3, CompletedSteps: 2, State: PhaseInProgress},
		{Sequence: 2, TotalSteps: 2, CompletedSteps: 0, State: PhasePending},
	}

	if err := UpdatePlanStatus(path, StateExecuting, planPhases); err != nil {
		t.Fatalf("UpdatePlanStatus returned error: %v", err)
	}

	// Read back and verify status changed.
	parsed, err := ParsePlan(path)
	if err != nil {
		t.Fatalf("ParsePlan returned error: %v", err)
	}

	if parsed.Status != "executing" {
		t.Errorf("Status = %q, want %q", parsed.Status, "executing")
	}

	if len(parsed.Phases) != 2 {
		t.Fatalf("Phases count = %d, want 2", len(parsed.Phases))
	}

	// Phase 1 should be in_progress.
	ph1 := parsed.Phases[0]
	if ph1.State != PhaseInProgress {
		t.Errorf("Phase 1 State = %q, want %q", ph1.State, PhaseInProgress)
	}

	// Steps 1 and 2 completed (Number <= CompletedSteps), step 3 pending.
	if ph1.Steps[0].State != StepStatusCompleted {
		t.Errorf("Step 1 State = %q, want %q", ph1.Steps[0].State, StepStatusCompleted)
	}
	if ph1.Steps[1].State != StepStatusCompleted {
		t.Errorf("Step 2 State = %q, want %q", ph1.Steps[1].State, StepStatusCompleted)
	}
	if ph1.Steps[2].State != StepStatusPending {
		t.Errorf("Step 3 State = %q, want %q", ph1.Steps[2].State, StepStatusPending)
	}

	// Phase 2 should remain pending with all steps pending.
	ph2 := parsed.Phases[1]
	if ph2.State != PhasePending {
		t.Errorf("Phase 2 State = %q, want %q", ph2.State, PhasePending)
	}
	for i, step := range ph2.Steps {
		if step.State != StepStatusPending {
			t.Errorf("Phase 2 Step %d State = %q, want %q", i, step.State, StepStatusPending)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip-plan.md")

	plan := newTestPlanForWriter()
	originalPhases := []ParsedPhase{
		{
			Name:     "Setup",
			Sequence: 1,
			State:    PhasePending,
			Steps: []ParsedStep{
				{Number: 1, Description: "Initialize repo", State: StepStatusPending},
				{Number: 2, Description: "Configure CI", State: StepStatusPending},
			},
		},
		{
			Name:     "Deploy",
			Sequence: 2,
			State:    PhasePending,
			Steps: []ParsedStep{
				{Number: 3, Description: "Deploy to staging", State: StepStatusPending, DependsOn: []int{1, 2}},
				{Number: 4, Description: "Run smoke tests", State: StepStatusPending, DependsOn: []int{3}},
			},
		},
	}

	// Write via WritePlanMarkdown.
	if err := WritePlanMarkdown(path, plan, originalPhases); err != nil {
		t.Fatalf("WritePlanMarkdown returned error: %v", err)
	}

	// Parse back via ParsePlanContent.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}
	parsed, err := ParsePlanContent(string(data))
	if err != nil {
		t.Fatalf("ParsePlanContent returned error: %v", err)
	}

	// Verify top-level fields preserved.
	if parsed.Title != plan.Title {
		t.Errorf("Title = %q, want %q", parsed.Title, plan.Title)
	}
	if parsed.PlanID != plan.ID {
		t.Errorf("PlanID = %q, want %q", parsed.PlanID, plan.ID)
	}
	if parsed.Project != plan.ProjectID {
		t.Errorf("Project = %q, want %q", parsed.Project, plan.ProjectID)
	}
	if parsed.Status != string(plan.State) {
		t.Errorf("Status = %q, want %q", parsed.Status, string(plan.State))
	}
	if parsed.Summary != plan.Description {
		t.Errorf("Summary = %q, want %q", parsed.Summary, plan.Description)
	}

	// Verify phases and steps preserved.
	if len(parsed.Phases) != len(originalPhases) {
		t.Fatalf("Phases count = %d, want %d", len(parsed.Phases), len(originalPhases))
	}
	for i, ph := range parsed.Phases {
		orig := originalPhases[i]
		if ph.Name != orig.Name {
			t.Errorf("Phase %d Name = %q, want %q", i, ph.Name, orig.Name)
		}
		if ph.Sequence != orig.Sequence {
			t.Errorf("Phase %d Sequence = %d, want %d", i, ph.Sequence, orig.Sequence)
		}
		if ph.State != orig.State {
			t.Errorf("Phase %d State = %q, want %q", i, ph.State, orig.State)
		}
		if len(ph.Steps) != len(orig.Steps) {
			t.Fatalf("Phase %d Steps count = %d, want %d", i, len(ph.Steps), len(orig.Steps))
		}
		for j, step := range ph.Steps {
			origStep := orig.Steps[j]
			if step.Number != origStep.Number {
				t.Errorf("Phase %d Step %d Number = %d, want %d", i, j, step.Number, origStep.Number)
			}
			if step.Description != origStep.Description {
				t.Errorf("Phase %d Step %d Description = %q, want %q", i, j, step.Description, origStep.Description)
			}
			if step.State != origStep.State {
				t.Errorf("Phase %d Step %d State = %q, want %q", i, j, step.State, origStep.State)
			}
			if !reflect.DeepEqual(step.DependsOn, origStep.DependsOn) {
				t.Errorf("Phase %d Step %d DependsOn = %v, want %v", i, j, step.DependsOn, origStep.DependsOn)
			}
		}
	}
}

func TestWriteFromParsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "parsed-plan.md")

	original := &ParsedPlan{
		Title:   "Parsed Plan Test",
		PlanID:  "plan-parsed-001",
		Project: "parsed-project",
		Status:  "planning",
		Summary: "A plan written from a ParsedPlan struct.",
		Phases: []ParsedPhase{
			{
				Name:     "Design",
				Sequence: 1,
				State:    PhasePending,
				Steps: []ParsedStep{
					{Number: 1, Description: "Gather requirements", State: StepStatusPending},
					{Number: 2, Description: "Create mockups", State: StepStatusCompleted},
				},
			},
		},
		Notes: []string{
			"This is a test note.",
			"Another note for verification.",
		},
	}

	// Write via WritePlanFromParsed.
	if err := WritePlanFromParsed(path, original); err != nil {
		t.Fatalf("WritePlanFromParsed returned error: %v", err)
	}

	// Parse back.
	parsed, err := ParsePlan(path)
	if err != nil {
		t.Fatalf("ParsePlan returned error: %v", err)
	}

	// Verify all fields preserved.
	if parsed.Title != original.Title {
		t.Errorf("Title = %q, want %q", parsed.Title, original.Title)
	}
	if parsed.PlanID != original.PlanID {
		t.Errorf("PlanID = %q, want %q", parsed.PlanID, original.PlanID)
	}
	if parsed.Project != original.Project {
		t.Errorf("Project = %q, want %q", parsed.Project, original.Project)
	}
	if parsed.Status != original.Status {
		t.Errorf("Status = %q, want %q", parsed.Status, original.Status)
	}
	if parsed.Summary != original.Summary {
		t.Errorf("Summary = %q, want %q", parsed.Summary, original.Summary)
	}

	// Verify phase.
	if len(parsed.Phases) != len(original.Phases) {
		t.Fatalf("Phases count = %d, want %d", len(parsed.Phases), len(original.Phases))
	}
	ph := parsed.Phases[0]
	origPh := original.Phases[0]
	if ph.Name != origPh.Name {
		t.Errorf("Phase Name = %q, want %q", ph.Name, origPh.Name)
	}
	if ph.Sequence != origPh.Sequence {
		t.Errorf("Phase Sequence = %d, want %d", ph.Sequence, origPh.Sequence)
	}
	if len(ph.Steps) != len(origPh.Steps) {
		t.Fatalf("Steps count = %d, want %d", len(ph.Steps), len(origPh.Steps))
	}

	// Step 1: pending.
	if ph.Steps[0].State != StepStatusPending {
		t.Errorf("Step 1 State = %q, want %q", ph.Steps[0].State, StepStatusPending)
	}
	if ph.Steps[0].Description != "Gather requirements" {
		t.Errorf("Step 1 Description = %q, want %q", ph.Steps[0].Description, "Gather requirements")
	}

	// Step 2: completed (written as strikethrough, parsed back).
	if ph.Steps[1].State != StepStatusCompleted {
		t.Errorf("Step 2 State = %q, want %q", ph.Steps[1].State, StepStatusCompleted)
	}
	if ph.Steps[1].Description != "Create mockups" {
		t.Errorf("Step 2 Description = %q, want %q", ph.Steps[1].Description, "Create mockups")
	}

	// Notes preserved.
	if !reflect.DeepEqual(parsed.Notes, original.Notes) {
		t.Errorf("Notes = %v, want %v", parsed.Notes, original.Notes)
	}
}
