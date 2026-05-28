package plan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const testPlanContent = `# Plan: Add OAuth2 Token Refresh

## Meta
- plan_id: plan-a1b2c3d4
- project: my-app
- created: 2026-05-28T10:30:00Z
- status: planning

## Summary
Implement automatic OAuth2 token refresh for the API gateway.

## Phase 1: Design [pending]
1. Analyze current auth flow [pending]
2. Design token refresh scheme [pending]
3. Document API contract changes [pending]

## Phase 2: Implementation [pending]
4. Update auth middleware [pending] (depends: 2)
5. Add refresh endpoint [pending] (depends: 2)
6. Implement client-side retry [pending] (depends: 4, 5)

## Phase 3: Testing [pending]
7. Write unit tests [pending] (depends: 5)
8. Integration test [pending] (depends: 6, 7)

## Notes
- Phase 2 and 3 can partially overlap
`

func TestParseFullPlan(t *testing.T) {
	parsed, err := ParsePlanContent(testPlanContent)
	if err != nil {
		t.Fatalf("ParsePlanContent returned error: %v", err)
	}

	// Verify top-level fields.
	if parsed.Title != "Add OAuth2 Token Refresh" {
		t.Errorf("Title = %q, want %q", parsed.Title, "Add OAuth2 Token Refresh")
	}
	if parsed.PlanID != "plan-a1b2c3d4" {
		t.Errorf("PlanID = %q, want %q", parsed.PlanID, "plan-a1b2c3d4")
	}
	if parsed.Project != "my-app" {
		t.Errorf("Project = %q, want %q", parsed.Project, "my-app")
	}
	if parsed.Status != "planning" {
		t.Errorf("Status = %q, want %q", parsed.Status, "planning")
	}
	if parsed.Summary != "Implement automatic OAuth2 token refresh for the API gateway." {
		t.Errorf("Summary = %q, want %q", parsed.Summary, "Implement automatic OAuth2 token refresh for the API gateway.")
	}

	// Verify 3 phases.
	if len(parsed.Phases) != 3 {
		t.Fatalf("len(Phases) = %d, want 3", len(parsed.Phases))
	}

	// Phase 1: Design.
	ph1 := parsed.Phases[0]
	if ph1.Name != "Design" {
		t.Errorf("Phase 1 Name = %q, want %q", ph1.Name, "Design")
	}
	if ph1.Sequence != 1 {
		t.Errorf("Phase 1 Sequence = %d, want 1", ph1.Sequence)
	}
	if ph1.State != PhasePending {
		t.Errorf("Phase 1 State = %q, want %q", ph1.State, PhasePending)
	}
	if len(ph1.Steps) != 3 {
		t.Fatalf("Phase 1 Steps count = %d, want 3", len(ph1.Steps))
	}

	// Phase 2: Implementation.
	ph2 := parsed.Phases[1]
	if ph2.Name != "Implementation" {
		t.Errorf("Phase 2 Name = %q, want %q", ph2.Name, "Implementation")
	}
	if ph2.Sequence != 2 {
		t.Errorf("Phase 2 Sequence = %d, want 2", ph2.Sequence)
	}
	if len(ph2.Steps) != 3 {
		t.Fatalf("Phase 2 Steps count = %d, want 3", len(ph2.Steps))
	}

	// Step 4 depends on [2].
	step4 := ph2.Steps[0]
	if step4.Number != 4 {
		t.Errorf("Step 4 Number = %d, want 4", step4.Number)
	}
	if !reflect.DeepEqual(step4.DependsOn, []int{2}) {
		t.Errorf("Step 4 DependsOn = %v, want [2]", step4.DependsOn)
	}

	// Step 5 depends on [2].
	step5 := ph2.Steps[1]
	if !reflect.DeepEqual(step5.DependsOn, []int{2}) {
		t.Errorf("Step 5 DependsOn = %v, want [2]", step5.DependsOn)
	}

	// Step 6 depends on [4, 5].
	step6 := ph2.Steps[2]
	if !reflect.DeepEqual(step6.DependsOn, []int{4, 5}) {
		t.Errorf("Step 6 DependsOn = %v, want [4, 5]", step6.DependsOn)
	}

	// Phase 3: Testing.
	ph3 := parsed.Phases[2]
	if ph3.Name != "Testing" {
		t.Errorf("Phase 3 Name = %q, want %q", ph3.Name, "Testing")
	}
	if ph3.Sequence != 3 {
		t.Errorf("Phase 3 Sequence = %d, want 3", ph3.Sequence)
	}
	if len(ph3.Steps) != 2 {
		t.Fatalf("Phase 3 Steps count = %d, want 2", len(ph3.Steps))
	}

	// Step 7 depends on [5].
	step7 := ph3.Steps[0]
	if !reflect.DeepEqual(step7.DependsOn, []int{5}) {
		t.Errorf("Step 7 DependsOn = %v, want [5]", step7.DependsOn)
	}

	// Step 8 depends on [6, 7].
	step8 := ph3.Steps[1]
	if !reflect.DeepEqual(step8.DependsOn, []int{6, 7}) {
		t.Errorf("Step 8 DependsOn = %v, want [6, 7]", step8.DependsOn)
	}

	// Notes.
	if len(parsed.Notes) != 1 {
		t.Fatalf("Notes count = %d, want 1", len(parsed.Notes))
	}
	if parsed.Notes[0] != "Phase 2 and 3 can partially overlap" {
		t.Errorf("Note[0] = %q, want %q", parsed.Notes[0], "Phase 2 and 3 can partially overlap")
	}
}

func TestParseMinimalPlan(t *testing.T) {
	content := `# Plan: Minimal Plan

## Meta
- plan_id: plan-minimal
- project: test-project
- status: draft

## Summary
A minimal plan for testing.
`
	parsed, err := ParsePlanContent(content)
	if err != nil {
		t.Fatalf("ParsePlanContent returned error: %v", err)
	}

	if parsed.Title != "Minimal Plan" {
		t.Errorf("Title = %q, want %q", parsed.Title, "Minimal Plan")
	}
	if parsed.PlanID != "plan-minimal" {
		t.Errorf("PlanID = %q, want %q", parsed.PlanID, "plan-minimal")
	}
	if parsed.Project != "test-project" {
		t.Errorf("Project = %q, want %q", parsed.Project, "test-project")
	}
	if parsed.Status != "draft" {
		t.Errorf("Status = %q, want %q", parsed.Status, "draft")
	}
	if parsed.Summary != "A minimal plan for testing." {
		t.Errorf("Summary = %q, want %q", parsed.Summary, "A minimal plan for testing.")
	}
	if len(parsed.Phases) != 0 {
		t.Errorf("Phases count = %d, want 0", len(parsed.Phases))
	}
	if len(parsed.Notes) != 0 {
		t.Errorf("Notes count = %d, want 0", len(parsed.Notes))
	}
}

func TestParseCompletedSteps(t *testing.T) {
	content := `# Plan: Test Completed Steps

## Meta
- plan_id: plan-completed
- project: test
- status: executing

## Summary
Testing completed step parsing.

## Phase 1: Build [in_progress]
1. ~~Setup environment~~ [completed]
2. ~~Install dependencies~~ [completed]
3. Run build [pending]
`
	parsed, err := ParsePlanContent(content)
	if err != nil {
		t.Fatalf("ParsePlanContent returned error: %v", err)
	}

	if len(parsed.Phases) != 1 {
		t.Fatalf("Phases count = %d, want 1", len(parsed.Phases))
	}
	steps := parsed.Phases[0].Steps
	if len(steps) != 3 {
		t.Fatalf("Steps count = %d, want 3", len(steps))
	}

	// Step 1: completed with strikethrough.
	if steps[0].State != StepStatusCompleted {
		t.Errorf("Step 1 State = %q, want %q", steps[0].State, StepStatusCompleted)
	}
	if steps[0].Description != "Setup environment" {
		t.Errorf("Step 1 Description = %q, want %q", steps[0].Description, "Setup environment")
	}

	// Step 2: completed with strikethrough.
	if steps[1].State != StepStatusCompleted {
		t.Errorf("Step 2 State = %q, want %q", steps[1].State, StepStatusCompleted)
	}
	if steps[1].Description != "Install dependencies" {
		t.Errorf("Step 2 Description = %q, want %q", steps[1].Description, "Install dependencies")
	}

	// Step 3: pending (no strikethrough).
	if steps[2].State != StepStatusPending {
		t.Errorf("Step 3 State = %q, want %q", steps[2].State, StepStatusPending)
	}
	if steps[2].Description != "Run build" {
		t.Errorf("Step 3 Description = %q, want %q", steps[2].Description, "Run build")
	}
}

func TestParseDependencies(t *testing.T) {
	content := `# Plan: Dep Test

## Meta
- plan_id: plan-deps
- project: test
- status: planning

## Summary
Testing dependency parsing.

## Phase 1: Setup [pending]
1. Initialize project [pending]
2. Configure CI [pending]

## Phase 2: Deploy [pending]
3. Deploy to staging [pending] (depends: 1, 2)
4. Run smoke tests [pending] (depends: 3)
`
	parsed, err := ParsePlanContent(content)
	if err != nil {
		t.Fatalf("ParsePlanContent returned error: %v", err)
	}

	if len(parsed.Phases) != 2 {
		t.Fatalf("Phases count = %d, want 2", len(parsed.Phases))
	}

	// Phase 1: no dependencies.
	if len(parsed.Phases[0].Steps[0].DependsOn) != 0 {
		t.Errorf("Step 1 DependsOn = %v, want empty", parsed.Phases[0].Steps[0].DependsOn)
	}
	if len(parsed.Phases[0].Steps[1].DependsOn) != 0 {
		t.Errorf("Step 2 DependsOn = %v, want empty", parsed.Phases[0].Steps[1].DependsOn)
	}

	// Phase 2: step 3 depends on [1, 2], step 4 depends on [3].
	step3 := parsed.Phases[1].Steps[0]
	if !reflect.DeepEqual(step3.DependsOn, []int{1, 2}) {
		t.Errorf("Step 3 DependsOn = %v, want [1, 2]", step3.DependsOn)
	}
	step4 := parsed.Phases[1].Steps[1]
	if !reflect.DeepEqual(step4.DependsOn, []int{3}) {
		t.Errorf("Step 4 DependsOn = %v, want [3]", step4.DependsOn)
	}
}

func TestParsePlanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-plan.md")

	if err := os.WriteFile(path, []byte(testPlanContent), 0o644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	parsed, err := ParsePlan(path)
	if err != nil {
		t.Fatalf("ParsePlan returned error: %v", err)
	}

	// Verify same top-level fields as TestParseFullPlan.
	if parsed.Title != "Add OAuth2 Token Refresh" {
		t.Errorf("Title = %q, want %q", parsed.Title, "Add OAuth2 Token Refresh")
	}
	if parsed.PlanID != "plan-a1b2c3d4" {
		t.Errorf("PlanID = %q, want %q", parsed.PlanID, "plan-a1b2c3d4")
	}
	if parsed.Project != "my-app" {
		t.Errorf("Project = %q, want %q", parsed.Project, "my-app")
	}
	if parsed.Status != "planning" {
		t.Errorf("Status = %q, want %q", parsed.Status, "planning")
	}
	if len(parsed.Phases) != 3 {
		t.Errorf("Phases count = %d, want 3", len(parsed.Phases))
	}

	// Verify dependencies still parsed correctly from file.
	ph2 := parsed.Phases[1]
	step6 := ph2.Steps[2]
	if !reflect.DeepEqual(step6.DependsOn, []int{4, 5}) {
		t.Errorf("Step 6 DependsOn = %v, want [4, 5]", step6.DependsOn)
	}
}

// newTestPlanForWriter creates a Plan suitable for writer tests.
// (store_test.go already defines newTestPlan with a different signature.)
func newTestPlanForWriter() *Plan {
	return NewPlan("Test Plan", "A test plan description", "test-project", "", "sess-test")
}
