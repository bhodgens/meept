package agent

// 01-safety TEST GATE T9 (00T-gate.md): evidence review gate (F5b).
//
// A file-shaped step (tool_hint code/file_edit/file_write) whose tool
// ledger shows ZERO file-tool executions auto-approves today when its
// result is plain narration that trips none of the claim guards -- the
// frozen p6-positive-control shape: committer ran 3 steps, zero file
// tools, all approved, hello.txt unchanged (W4, F-NEW-2/13).
//
// 06-evidence-review contract: file-shaped + zero file ToolEvidence ->
// forced evidence review, NEVER auto-approved; with real file
// ToolEvidence the normal approval path is preserved; the rejection
// reason names the missing operation (never "No explicit feedback
// provided").
//
// RED today: the zero-evidence file step is auto-approved by the
// trivial-task heuristic.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// gateFileStep builds a file-edit step with the given evidence ledger.
// The result is benign narration (no artifact/tool-run claim markers),
// exactly the shape that sails through today's heuristic guards.
func gateFileStep(t *testing.T, rm *ReviewManager, id, taskID string, evidence []models.Evidence) *task.TaskStep {
	t.Helper()
	step := &task.TaskStep{
		ID:          id,
		TaskID:      taskID,
		Sequence:    0,
		State:       task.StepCompleted,
		ToolHint:    "file_edit",
		Description: "edit hello.txt to add the greeting line",
		Result:      "The greeting line is now in place and the document reads cleanly from top to bottom.",
		Evidence:    evidence,
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	return step
}

// TestFileStepZeroEvidenceGate is the load-bearing pin: a file-edit step
// with ZERO file-tool evidence must NOT auto-approve.
func TestFileStepZeroEvidenceGate(t *testing.T) {
	rm := newTestReviewManager(t)
	step := gateFileStep(t, rm, "step-zero-ev", "task-zero-ev", nil)

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		// A forced-review escalation that cannot resolve a reviewer is
		// acceptable fail-CLOSED behavior (never an approval).
		if strings.Contains(err.Error(), "reviewer") {
			return
		}
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status == ReviewApproved {
		t.Fatalf("CURRENT DEFECT: file-edit step with ZERO file-tool evidence was AUTO-APPROVED (%q); the fabricated-success gate must force evidence review (W4/F-NEW-2)", res.Feedback)
	}
}

// TestFileStepZeroEvidenceReason pins the actionable rejection copy: the
// zero-evidence verdict names the missing file operation and the task --
// never blank/"No explicit feedback provided".
func TestFileStepZeroEvidenceReason(t *testing.T) {
	rm := newTestReviewManager(t)
	step := gateFileStep(t, rm, "step-zero-reason", "task-zero-reason", nil)

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		if strings.Contains(err.Error(), "reviewer") {
			return // fail-closed escalation; no verdict copy to check
		}
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status == ReviewApproved {
		t.Fatalf("CURRENT DEFECT: zero-evidence file step approved (%q)", res.Feedback)
	}
	if res.Status == ReviewRejected {
		feedback := strings.ToLower(res.Feedback)
		if strings.Contains(feedback, "no explicit feedback") || strings.TrimSpace(res.Feedback) == "" {
			t.Errorf("CURRENT DEFECT: rejection copy is blank feedback (%q); it must name the missing file operation", res.Feedback)
		}
		if !strings.Contains(feedback, "file") {
			t.Errorf("rejection reason %q must name the missing file operation (user-language, actionable)", res.Feedback)
		}
	}
}

// TestFileStepWithEvidenceApproves pins the positive path: a file-edit
// step WITH real file-tool ToolEvidence keeps the normal approval flow
// (no over-gating).
func TestFileStepWithEvidenceApproves(t *testing.T) {
	rm := newTestReviewManager(t)
	evidence := []models.Evidence{
		models.NewEvidence(models.EvidenceFileExists, "hello.txt", "sha256:abc123", "file_edit"),
	}
	step := gateFileStep(t, rm, "step-real-ev", "task-real-ev", evidence)

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep with real file evidence: %v", err)
	}
	if res.Status != ReviewApproved {
		t.Errorf("file-edit step WITH file-tool evidence got %q (%q); the normal approval path must be preserved (no over-gating)", res.Status, res.Feedback)
	}
}

// TestZeroEvidenceGateNonFileUnaffected pins scope: a conversational
// step with no evidence still auto-approves (chat/analysis products ARE
// the report; the gate is file-shaped steps only).
func TestZeroEvidenceGateNonFileUnaffected(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{
		ID:       "step-chat-noev",
		TaskID:   "task-chat-noev",
		Sequence: 0,
		State:    task.StepCompleted,
		ToolHint: "chat",
		Result:   "Here is the explanation you asked for, covering all three points in order.",
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewApproved {
		t.Errorf("conversational step without evidence got %q; the file-evidence gate must not over-gate non-file steps", res.Status)
	}
}
