package agent

// F9 (2026-09-17 bughunt) pins: ReviewStep must NOT auto-approve a step
// whose result claims a specific tool execution with no meaningful tool
// evidence, even when the review policy would skip review for its
// conversational hint. DefaultReviewPolicy lists "chat" in SkipReview, so
// before the fix the policy.NeedsReview shortcut returned ReviewApproved
// before the claim-without-evidence guard (heuristicReviewPasses) ever
// ran — the exact hallucination shape from the 2026-09-15 sweep ("extraction
// ran clean" with zero tool calls routed).
//
// The plain-conversational sibling (TestHeuristicReview_PlainConversationalAnswerStillPasses)
// pins that honest chat answers still auto-approve; it must stay green.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// TestReviewStep_ClaimWithoutEvidenceNotAutoApprovedBySkipHint drives the
// full ReviewStep path: a "chat" step whose structured envelope narrates a
// tool execution with no tool evidence must not come back ReviewApproved
// via the skip-review shortcut.
func TestReviewStep_ClaimWithoutEvidenceNotAutoApprovedBySkipHint(t *testing.T) {
	rm := newTestReviewManager(t)

	// Non-nil registry with no registered reviewer spec: the full-review
	// path (where this step must land) fails in registry.Get with
	// "agent spec not found" — an ERROR, never an approval. With the bug
	// present this method instead returned (ReviewApproved, nil) from
	// the skip-review shortcut without ever touching the registry.
	rm.registry = NewAgentRegistry(RegistryConfig{Logger: testLogger()})

	step := &task.TaskStep{
		ID:       "step-f9-claim",
		TaskID:   "task-f9-claim",
		Sequence: 0,
		State:    task.StepCompleted,
		ToolHint: "chat", // on DefaultReviewPolicy.SkipReview
		Result: `{"evidence":[],"response":"Extraction ran clean on the first pass — no fuss, no drama.",` +
			`"status":"completed","success":true}`,
	}

	res, err := rm.ReviewStep(context.Background(), step, nil)

	// The pin: NOT auto-approved via the skip-review shortcut. With the
	// F9 bug present this method returned (ReviewApproved, "Auto-approved
	// (no review required)", nil) without consulting any guard. With the
	// fix, the step escalates to the reviewer lane (which here errors
	// with "no LLM client configured" — an error, never an approval).
	if err == nil && res != nil && res.Status == ReviewApproved {
		t.Fatalf("claim-no-evidence step with skip-review hint was AUTO-APPROVED: %+v", res)
	}
	if res != nil && res.Feedback == "Auto-approved (no review required)" {
		t.Errorf("skip-review shortcut fired for a guard-flagged step: %+v", res)
	}
}

// heuristicGuardsPass unit pin: the guard helper agrees with
// heuristicReviewPasses on both shapes.
func TestHeuristicGuardsPass_MatchesClaimGuards(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}

	claimStep := &task.TaskStep{
		ToolHint: "chat",
		Result:   `{"response":"Extraction ran clean on the first pass.","success":true}`,
	}
	if rm.heuristicGuardsPass(claimStep) {
		t.Error("tool-execution claim with no evidence must fail the policy-gate guard")
	}

	plainStep := &task.TaskStep{
		ToolHint: "chat",
		Result:   "17 * 23 = 391, because 17 times 23 is the product of the two numbers.",
	}
	if !rm.heuristicGuardsPass(plainStep) {
		t.Error("plain conversational answer must pass the policy-gate guard")
	}
}
