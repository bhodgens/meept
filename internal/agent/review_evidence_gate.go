package agent

// 06-evidence-review (plan 20261119-usability-remediation, W4 / F-NEW-2/4/13):
// the review-side half of the fabricated-success defect. A file-shaped step
// whose tool ledger shows zero successful file-tool executions can never
// finalize as completed:
//
//   - zero file-tool ToolEvidence  -> forced evidence review: rejected with
//     an actionable reason naming the missing operation and the task; ONE
//     bounded revision retry, then a failed terminal (never auto-approved).
//   - only ERRORED file tools      -> failed terminal with an actionable
//     error (F-NEW-13: the empty-anchor file_edit error shipped as an
//     apology-as-result). Narration/apology is never a result.
//   - only security-DENIED writes  -> rejected once with workdir-relative
//     retry guidance (F-NEW-4); a second denial is a failed terminal.
//   - real file-tool ToolEvidence  -> gate silent; the normal approval path
//     is preserved (no over-gating).
//
// The gate judges ONLY tool-issued evidence (step.Evidence, fed from
// tool.execution.complete bus events by tactical.go) -- never the model's
// Claims or result narration. Non-file steps (chat/analysis) are untouched.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// maxFileEvidenceRetries bounds the zero-evidence / denial revision lane:
// one retry, then a failed terminal. No unbounded review loop.
const maxFileEvidenceRetries = 1

// fileArtifactNameRe matches a file-artifact token (name + extension) in a
// step description -- the second file-shape signal beside the tool hint.
var fileArtifactNameRe = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_./-]*\.[A-Za-z0-9]{1,8}\b`)

// stepIsFileShaped reports whether a step is expected to produce a file
// effect: its tool hint is a file-editing hint, or its description names a
// file artifact. Conversational/analysis steps are not file-shaped.
func stepIsFileShaped(step *task.TaskStep) bool {
	switch strings.ToLower(strings.TrimSpace(step.ToolHint)) {
	case "code", "file_edit", "file_write":
		return true
	}
	return fileArtifactNameRe.MatchString(step.Description)
}

// isFileToolEvidence reports whether an evidence entry was issued by a
// file tool (file_read/file_write/file_edit/file_append/... sources, or a
// file_exists/file_hash type). Structurally empty entries are not evidence.
func isFileToolEvidence(e models.Evidence) bool {
	if e.Type == "" && e.Subject == "" && e.Value == "" && e.Source == "" {
		return false
	}
	switch e.Type {
	case models.EvidenceFileExists, models.EvidenceFileHash:
		return true
	}
	return strings.HasPrefix(strings.ToLower(e.Source), "file_")
}

// fileEvidenceOutcome classifies a file-tool evidence entry's value:
// "ok", "errored", or "denied". Tool ledgers record failures as value
// prose (error:/denied: prefixes); a successful entry carries a hash,
// byte count, or confirmation instead.
func fileEvidenceOutcome(e models.Evidence) string {
	lower := strings.ToLower(e.Value + " " + e.Subject)
	switch {
	case strings.Contains(lower, "denied"),
		strings.Contains(lower, "permission"),
		strings.Contains(lower, "outside the working directory"),
		strings.Contains(lower, "outside the session"):
		return "denied"
	case strings.Contains(lower, "error"),
		strings.Contains(lower, "failed"),
		strings.Contains(lower, "not found"):
		return "errored"
	}
	return "ok"
}

// hasSuccessfulFileEvidence reports whether the ledger carries at least
// one successful file-tool entry (the positive-path predicate shared by
// the gate and ReviewStep's policy-shortcut suppression).
func hasSuccessfulFileEvidence(evs []models.Evidence) bool {
	for _, e := range evs {
		if isFileToolEvidence(e) && fileEvidenceOutcome(e) == "ok" {
			return true
		}
	}
	return false
}

// fileEvidenceGate enforces the file-shaped evidence contract. It returns
// nil when the gate is silent (non-file step, or real successful file-tool
// evidence present) so the normal policy/approval paths run unchanged.
// A non-nil verdict is always fail-CLOSED: rejected or failed, never
// approved. Runs in ReviewStep before every policy path.
func (rm *ReviewManager) fileEvidenceGate(step *task.TaskStep, policy *ReviewPolicy) *ReviewResult {
	if step == nil || !stepIsFileShaped(step) {
		return nil
	}

	var fileEntries []models.Evidence
	for _, e := range step.Evidence {
		if isFileToolEvidence(e) {
			fileEntries = append(fileEntries, e)
		}
	}

	// Positive path: at least one successful file-tool execution backs the
	// step. The gate stays silent -- normal approval, no over-gating (T2).
	errored, denied := 0, 0
	for _, e := range fileEntries {
		switch fileEvidenceOutcome(e) {
		case "ok":
			return nil
		case "denied":
			denied++
		case "errored":
			errored++
		}
	}

	taskLabel := strings.TrimSpace(step.Description)
	if taskLabel == "" {
		taskLabel = step.TaskID
	}

	// T1b (F-NEW-13): the only file tool ERRORED (e.g. empty-anchor
	// file_edit). An apology is not a result: failed terminal, actionable
	// error. No revision -- re-running the same broken edit cannot help.
	if errored > 0 {
		feedback := fmt.Sprintf(
			"Rejected: the file operation for this step failed and no file was changed. "+
				"Task: %s. The file tool reported an error (for file_edit, the anchor text must match the file exactly -- "+
				"read the file first and retry with the exact existing text, or use file_write to replace the whole file). "+
				"Do not report completion until a file tool succeeds.", taskLabel)
		return rm.failFileEvidenceStep(step, feedback, "errored file tool")
	}

	// T1b (F-NEW-4): the write was security-DENIED. One bounded retry with
	// workdir-relative guidance; a second denial is a failed terminal.
	if denied > 0 {
		if step.RevisionCount >= maxFileEvidenceRetries {
			feedback := fmt.Sprintf(
				"Rejected: the file write for this step was denied by the security policy again after a retry. "+
					"Task: %s. No file was changed; the step cannot be completed. "+
					"Ask the user to adjust the working directory or permissions if this path is genuinely required.", taskLabel)
			return rm.failFileEvidenceStep(step, feedback, "denied file write after retry")
		}
		feedback := fmt.Sprintf(
			"Rejected: the file write for this step was denied by the security policy, so no file was changed. "+
				"Task: %s. Retry the write ONCE with a path relative to the session working directory "+
				"(for example hello.txt instead of an absolute path outside it). "+
				"Do not report completion until the write succeeds.", taskLabel)
		rm.logger.Warn("File-evidence gate: security-denied file write; one workdir-relative retry allowed",
			"step_id", step.ID, "task_id", step.TaskID, "revision_count", step.RevisionCount)
		return &ReviewResult{
			Status:     ReviewRejected,
			Feedback:   feedback,
			Confidence: 1.0,
		}
	}

	// T1/T3: zero file-tool ToolEvidence. The step narrates a file effect
	// no tool ever performed (the p6-positive-control shape). Forced
	// evidence review: never auto-approved via any policy shortcut or the
	// trivial-task heuristic. When a full reviewer lane can resolve, the
	// step falls through to it (nil return) with the gate re-checked on
	// the verdict below -- the reviewer may still approve with
	// justification. When no reviewer can resolve (or the retry budget is
	// exhausted), reject deterministically with copy naming the missing
	// file operation and the task in user language -- never
	// blank/"No explicit feedback provided".
	reviewerAvailable := false
	if rm.registry != nil && step.RevisionCount < maxFileEvidenceRetries {
		if _, err := rm.registry.GetForTask(policy.SelectReviewer(step), step.TaskID); err == nil {
			reviewerAvailable = true
		}
	}
	if reviewerAvailable {
		rm.logger.Warn("File-evidence gate: file-shaped step with zero file-tool evidence; forcing full reviewer",
			"step_id", step.ID, "task_id", step.TaskID, "tool_hint", step.ToolHint)
		return nil
	}
	if step.RevisionCount >= maxFileEvidenceRetries {
		feedback := fmt.Sprintf(
			"Rejected: no file operation was executed for this step, even after a retry. "+
				"Task: %s. The file is unchanged; the step cannot be completed. "+
				"Run the file edit/write tool and verify the change on disk before reporting completion.", taskLabel)
		return rm.failFileEvidenceStep(step, feedback, "zero file evidence after retry")
	}
	feedback := fmt.Sprintf(
		"Rejected: no file operation was executed for this step -- the tool ledger shows zero file-tool executions, "+
			"so the requested file change did not happen. Task: %s. "+
			"Execute the file edit or write (for example file_edit/file_write on the named file), "+
			"then verify the file actually changed before reporting completion.", taskLabel)
	rm.logger.Warn("File-evidence gate: file-shaped step with zero file-tool evidence; forced evidence review",
		"step_id", step.ID, "task_id", step.TaskID, "tool_hint", step.ToolHint)
	return &ReviewResult{
		Status:     ReviewRejected,
		Feedback:   feedback,
		Confidence: 1.0,
	}
}

// failFileEvidenceStep marks the step failed (terminal) and returns the
// rejection verdict. HandleReviewResult keeps a failed step failed (no
// revision is created for it), so the task finalizes honestly as failed.
func (rm *ReviewManager) failFileEvidenceStep(step *task.TaskStep, feedback, reason string) *ReviewResult {
	rm.logger.Warn("File-evidence gate: failing step terminal",
		"step_id", step.ID, "task_id", step.TaskID, "reason", reason)
	if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
		rm.logger.Error("Failed to set file-evidence-gated step to failed", "error", err)
	}
	step.State = task.StepFailed
	return &ReviewResult{
		Status:     ReviewRejected,
		Feedback:   feedback,
		Confidence: 1.0,
	}
}
