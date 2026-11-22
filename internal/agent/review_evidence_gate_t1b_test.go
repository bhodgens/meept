package agent

// 06-evidence-review T1b gate tests (leaf doc marks these "(add)"):
// a file-shaped step whose only file tool ERRORED (F-NEW-13 empty-anchor
// file_edit) or was security-DENIED (F-NEW-4) cannot finalize completed.
// Errored -> failed terminal with actionable error. Denied -> one bounded
// workdir-relative retry, then failed terminal. Narration/apology is never
// a result.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TestErroredFileToolGate: the only file tool errored (empty-anchor
// file_edit); the step's narration is an apology. It must NOT be approved
// and must land failed-terminal with an actionable error.
func TestErroredFileToolGate(t *testing.T) {
	rm := newTestReviewManager(t)
	evidence := []models.Evidence{
		models.NewEvidence(models.EvidenceFileExists, "hello.txt",
			"error: file_edit anchor text not found in file", "file_edit"),
	}
	step := gateFileStep(t, rm, "step-errored-ft", "task-errored-ft", evidence)
	step.Result = "I'm sorry, the edit could not be applied because the anchor was empty. The file should be fine as it was."

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status == ReviewApproved {
		t.Fatalf("F-NEW-13 DEFECT: errored file_edit finalized approved (%q); apology-as-result must fail", res.Feedback)
	}
	if res.Status != ReviewRejected {
		t.Fatalf("status = %q, want rejected", res.Status)
	}
	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Errorf("step state = %q, want failed terminal (no completed-with-error)", persisted.State)
	}
	fb := strings.ToLower(res.Feedback)
	if !strings.Contains(fb, "file") || strings.TrimSpace(res.Feedback) == "" {
		t.Errorf("feedback must name the failed file operation, got %q", res.Feedback)
	}
	if !strings.Contains(fb, "anchor") && !strings.Contains(fb, "retry") && !strings.Contains(fb, "read the file") {
		t.Errorf("feedback must be actionable (anchor/retry guidance), got %q", res.Feedback)
	}
}

// TestDeniedWriteGate: a security-denied file write cannot finalize
// completed. First review: rejected with workdir-relative retry guidance
// (one bounded retry). After the retry is consumed and the write is still
// denied: failed terminal.
func TestDeniedWriteGate(t *testing.T) {
	rm := newTestReviewManager(t)
	denied := []models.Evidence{
		models.NewEvidence(models.EvidenceFileExists, "/etc/passwd",
			"denied: path outside the working directory", "file_write"),
	}
	step := gateFileStep(t, rm, "step-denied-w", "task-denied-w", denied)
	step.Description = "write the greeting into hello.txt"
	step.Result = "The write was blocked, but the greeting is conceptually in place and the task is done."

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status == ReviewApproved {
		t.Fatalf("F-NEW-4 DEFECT: security-denied write finalized approved (%q)", res.Feedback)
	}
	if res.Status != ReviewRejected {
		t.Fatalf("status = %q, want rejected", res.Status)
	}
	fb := strings.ToLower(res.Feedback)
	if !strings.Contains(fb, "denied") || !strings.Contains(fb, "relative") {
		t.Errorf("first denial must give workdir-relative retry guidance, got %q", res.Feedback)
	}

	// Retry consumed, still denied: failed terminal, no further revisions.
	step.RevisionCount = 1
	res2, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep after retry: %v", err)
	}
	if res2.Status == ReviewApproved {
		t.Fatalf("denied write approved after retry: %q", res2.Feedback)
	}
	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Errorf("step state after exhausted denial retry = %q, want failed terminal", persisted.State)
	}
}

// TestZeroEvidenceRetryBounded: the zero-evidence rejection grants exactly
// one revision; a still-empty ledger after the retry is a failed terminal
// (no unbounded review loop).
func TestZeroEvidenceRetryBounded(t *testing.T) {
	rm := newTestReviewManager(t)
	step := gateFileStep(t, rm, "step-zero-retry", "task-zero-retry", nil)

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewRejected {
		t.Fatalf("first zero-evidence review = %q, want rejected", res.Status)
	}
	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State == task.StepFailed {
		t.Fatalf("first rejection must allow one bounded retry, not fail terminal immediately")
	}

	step.RevisionCount = 1
	res2, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep after retry: %v", err)
	}
	if res2.Status == ReviewApproved {
		t.Fatalf("zero-evidence step approved after retry: %q", res2.Feedback)
	}
	persisted2, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted2.State != task.StepFailed {
		t.Errorf("state after exhausted retry = %q, want failed terminal", persisted2.State)
	}
	if !strings.Contains(strings.ToLower(res2.Feedback), "file") {
		t.Errorf("terminal copy must name the missing file operation, got %q", res2.Feedback)
	}
}
