# 06-evidence-review PROGRESS

## T1 - file-shaped zero-evidence gate (written)
- Files changed:
  - internal/agent/review_manager.go: ReviewStep now calls rm.fileEvidenceGate(step) after the stepHasError gate, BEFORE every policy path (skip-review shortcut, auto-approve, trivial-task heuristic).
  - internal/agent/review_evidence_gate.go (NEW): fileEvidenceGate + stepIsFileShaped (tool_hint code/file_edit/file_write OR description names a file artifact via extension regex) + isFileToolEvidence (file_* source or file_exists/file_hash type; tool-issued step.Evidence only, never Claims) + bounded retry (maxFileEvidenceRetries=1; exhausted -> StepFailed terminal via failFileEvidenceStep).
- Verify: pending overlay run (sibling test files reference undefined seams; package test build RED expected).

## T1b - errored/denied file tool gate (written)
- review_evidence_gate.go: fileEvidenceOutcome classifies evidence value prose (denied/errored/ok). Errored-only file ledger -> failed terminal, actionable copy (anchor-match guidance). Denied write -> ONE rejection with workdir-relative retry guidance; second denial (RevisionCount>=1) -> failed terminal. Narration never counts as a result.
- Verify: pending.

## T2 - positive path preserved (written)
- Gate returns nil when >=1 successful file-tool evidence entry exists; normal policy/approval flow unchanged. TestFileStepWithEvidenceApproves (existing gate test) pins it.
- Verify: pending.

## T3 - actionable rejection copy (written)
- Rejection feedback names "no file operation was executed", the task description, and the concrete next action; never blank/"No explicit feedback provided". TestFileStepZeroEvidenceReason (existing gate test) pins it.
- Verify: pending.
