package agent

// Plan-vacuity review gate pins (leaf 03, docs/plans/20260918-tool-boundary-hardening/03-plan-vacuity-gate.md).
//
// Regression context (2026-09-18 e2e): a planner step was APPROVED with
// result "I need to create the file 'hello.txt'... I will perform the task
// now... Let me execute the necessary commands" — zero planning artifact —
// via the trivial-task heuristic ("trivial task, non-empty result"). A
// plan-hint step whose result is pure first-person future-intent narration
// is not evidence of planning work.
//
// Production hint values (grep evidence, internal/agent):
//   - intent.go:23  IntentPlan IntentType = "plan"
//   - intent.go:64  IntentArchitect IntentType = "architect"
//   - strategic.go:1211 step.ToolHint = req.Intent (fallback steps carry the
//     request intent, e.g. "plan")
//   - planner_template.go:208-210 the planner prompt advertises "plan" as a
//     valid tool_hint ("plan" → further planning/decomposition), so LLM
//     decomposed steps can carry ToolHint "plan" too.
//   - strategic.go:1250 reviewerStep.ToolHint = string(IntentReview) ("review")
//     for pair:reviewer steps.

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// The verbatim e2e offender text.
const vacuityOffenderText = "I need to create the file 'hello.txt' in the current directory containing the word 'hello'. Since I have no evidence of this being done yet, I will perform the task now and then provide the full path. Let me execute the necessary commands to create the file and verify its contents."

// vacuityStep builds a planner-flavored step fixture with a given hint.
func vacuityStep(hint string) *task.TaskStep {
	return &task.TaskStep{
		ID:       "step-vacuity-" + hint,
		TaskID:   "task-vacuity",
		Sequence: 0,
		State:    task.StepCompleted,
		ToolHint: hint,
	}
}

func TestPlanLooksVacuous_NarrationOnly(t *testing.T) {
	step := vacuityStep("plan")
	step.Result = vacuityOffenderText
	if !planLooksVacuous(step) {
		t.Error("planner-hint step with pure narration result and no evidence must be vacuous")
	}
}

func TestPlanLooksVacuous_WithTaskCreateEvidence(t *testing.T) {
	step := vacuityStep("plan")
	step.Result = vacuityOffenderText
	step.Evidence = []models.Evidence{{
		Type:   models.EvidenceProcessExit,
		Source: "task_create",
	}}
	if planLooksVacuous(step) {
		t.Error("planner-hint step WITH task-management tool evidence must NOT be vacuous")
	}
}

func TestPlanLooksVacuous_StructuredPlanText(t *testing.T) {
	// Numbered list = a structured plan artifact.
	step := vacuityStep("plan")
	step.Result = "1. Create hello.txt\n2. Report the path"
	if planLooksVacuous(step) {
		t.Error("planner-hint step with a numbered-list plan must NOT be vacuous")
	}

	// JSON object start = structured artifact.
	stepJSON := vacuityStep("plan")
	stepJSON.Result = `{"steps": ["create hello.txt", "verify"]}`
	if planLooksVacuous(stepJSON) {
		t.Error("planner-hint step with a JSON plan artifact must NOT be vacuous")
	}

	// 2+ bullet lines = structured artifact (review checklist: narration +
	// one bullet must not defeat detection — one bullet alone is not enough).
	stepBullets := vacuityStep("plan")
	stepBullets.Result = "I will plan this out:\n- step one\n- step two"
	if planLooksVacuous(stepBullets) {
		t.Error("planner-hint step with a 2+ bullet plan must NOT be vacuous")
	}

	// A single bullet line with narration is still vacuous (cannot be
	// defeated by narration + one bullet).
	stepOneBullet := vacuityStep("plan")
	stepOneBullet.Result = "I will plan this out:\n- step one"
	if !planLooksVacuous(stepOneBullet) {
		t.Error("narration plus a single bullet must remain vacuous")
	}
}

func TestPlanLooksVacuous_NonPlannerHintUnaffected(t *testing.T) {
	step := vacuityStep("code")
	step.Result = vacuityOffenderText
	if planLooksVacuous(step) {
		t.Error("non-planner hints must be unaffected (returns false)")
	}
}

func TestPlanLooksVacuous_EmptyResultNotVacuous(t *testing.T) {
	// Empty is a different failure mode, already handled by the
	// non-empty heuristic check — it must not be classified vacuous.
	step := vacuityStep("plan")
	step.Result = ""
	if planLooksVacuous(step) {
		t.Error("empty result must NOT be vacuous (different failure, handled elsewhere)")
	}
}

func TestPlanLooksVacuous_ReviewHintPlannerPair(t *testing.T) {
	// pair:reviewer steps carry ToolHint "review" (strategic.go:1250:
	// reviewerStep.ToolHint = string(IntentReview)). They are planner-side
	// steps and must be covered by the gate.
	step := vacuityStep("review")
	step.Result = "Let me review the plan and get back to you with the results."
	if !planLooksVacuous(step) {
		t.Error("review-hint (pair:reviewer) step with narration-only result must be vacuous")
	}

	// "architect" is the other planning intent (intent.go:64), pairs with
	// "plan" in spec_plan mode (intent.go:118).
	stepArch := vacuityStep("architect")
	stepArch.Result = vacuityOffenderText
	if !planLooksVacuous(stepArch) {
		t.Error("architect-hint step with pure narration result must be vacuous")
	}
}

func TestPlanLooksVacuous_NarrationMarkerBoundary400(t *testing.T) {
	// Marker inside the first 400 chars: vacuous.
	prefix := make([]byte, 300)
	for i := range prefix {
		prefix[i] = 'x'
	}
	step := vacuityStep("plan")
	step.Result = string(prefix) + " I will now begin the work."
	if !planLooksVacuous(step) {
		t.Error("narration marker within the first 400 chars must be detected")
	}

	// Marker BEYOND 400 chars: not detected (scan window boundary).
	farPrefix := make([]byte, 450)
	for i := range farPrefix {
		farPrefix[i] = 'x'
	}
	stepFar := vacuityStep("plan")
	stepFar.Result = string(farPrefix) + " I will now begin the work."
	if planLooksVacuous(stepFar) {
		t.Error("narration marker beyond the first 400 chars must NOT be detected")
	}
}

func TestPlanLooksVacuous_CaseInsensitiveMarkers(t *testing.T) {
	for _, marker := range []string{
		"i will create the plan now",
		"I WILL create the plan now",
		"I'LL draft the steps",
		"i'll draft the steps",
		"I NEED TO decompose this task",
		"LET ME break this down",
		"i am going to write the plan",
	} {
		step := vacuityStep("plan")
		step.Result = marker
		if !planLooksVacuous(step) {
			t.Errorf("narration marker %q (case-insensitive) must be detected as vacuous", marker)
		}
	}
}
