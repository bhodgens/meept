# Evidence Review Gate - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T9) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** A file-shaped step that ran zero file tools cannot pass review as "completed"
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Audit references:** W4; F-ORCH-20; F-NEW-2/4/13 (fabricated success on
  errored file_edit + security-denied write; F-NEW-13 empty-anchor
  file_edit error shipped as apology-as-result); R1 (review half);
  reviewer-agent architecture

## Goal

Force evidence review when a step's tool_hint/description is file-shaped but
its tool ledger shows zero file-tool executions; block "completed" with zero
effects. This is the review-side half of the fabricated-success defect (the
routing half is 02-routing/01). The committer ran 3 steps, zero file tools,
all approved, file unchanged - review must catch that.

## Context

- internal/agent/review_manager.go - ReviewStep gates on stepHasError before
  policy paths; claim-vs-evidence marking; ToolEvidence (tool-issued, from
  tool.execution.complete bus events) vs Claims (model narration).
- internal/agent/tactical.go - execResult ToolEvidence/Claims extraction
  (~1510-1530); "Unbacked file side-effect claims with zero tool executions"
  nudge exists in loop.go (terminatesOnAnnouncedAction / turnExecutedAnyTools).
- internal/session - session_tool_calls ledger (turnToolCalls handler.go:1684).
- config/agents/committer/AGENT.md - committer tools (git-shaped); a file-edit
  step routed here has no file tools in its ledger.
- Frozen evidence: probes/p6-positive-control.json turn2 (3 steps, zero file
  tools, approved, hello.txt unchanged); daemon.log "Step approved ... No
  explicit feedback provided".

## Verified Anchors

- internal/agent/review_manager.go - ReviewStep + stepHasError gate (re-grep)
- internal/agent/tactical.go:1510 - ToolEvidence/Claims extraction region
- internal/agent/loop.go - turnExecutedAnyTools / unbacked-claim nudge

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Review gate: file-shaped step (tool_hint in {code,file_edit,file_write} or
  description names a file artifact) with zero file-tool ToolEvidence ->
  forced evidence review; cannot auto-approve; verdict rejected with
  actionable reason if no evidence after bounded retry.

### What This Leaf Consumes
- C1 (terminal honesty), ToolEvidence ledger (existing).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | File-shaped zero-evidence gate | Modify: internal/agent/review_manager.go ReviewStep (force evidence review when file-shaped + zero file ToolEvidence) | `go test ./internal/agent/ -run TestFileStepZeroEvidenceGate -count=1` | gate T9 RED (auto-approves today) | GREEN | Given file-edit step, ledger has zero file tools; When ReviewStep; Then NOT auto-approved; forced evidence review; rejected with "no file operation was executed" if still empty |
| T1b | Errored-tool + denial gate (F-NEW-4/13) | Modify: internal/agent/review_manager.go (file-shaped step whose only file tool ERRORED - empty-anchor file_edit - or was security-DENIED cannot finalize completed) | `go test ./internal/agent/ -run 'TestErroredFileToolGate|TestDeniedWriteGate' -count=1` (add) | apology-as-result + denial-prose completed today | GREEN | Given file_edit errored (missing anchor) or write security-denied; Then step NOT completed; failed terminal with actionable error (retry workdir-relative once for denial); narration/apology is never a result |
| T2 | Ledger-backed approval preserved | Modify: review_manager.go (positive path) | `go test ./internal/agent/ -run TestFileStepWithEvidenceApproves -count=1` (add) | - | GREEN | Given file-edit step WITH file_write ToolEvidence; Then normal approval path unchanged (no over-gating) |
| T3 | Actionable rejection copy | Modify: review_manager.go rejection reason | `go test ./internal/agent/ -run TestZeroEvidenceReason -count=1` (add) | "No explicit feedback provided" today | GREEN | Given zero-evidence rejection; Then reason names the missing operation + task, user-language (never blank feedback) |

## Self-Verification Checklist

- [ ] Gate T9 GREEN; gate diff empty
- [ ] Positive path (real file evidence) NOT over-gated
- [ ] Live-gate: rig committer file-edit task with zero tools -> rejected terminal, file unchanged honestly reported
- [ ] No scope creep / artifacts / corruption; gofmt clean

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Gate uses ToolEvidence (tool-issued), never model Claims alone
- [ ] Bounded retry (no unbounded review loop); caps respected
- [ ] Non-file steps unaffected (chat/analysis steps still auto-approve)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- This leaf is the review half of W4/R1; the routing half (veto widening)
  is 02-routing/01. Both must land for F-ORCH-20 to stop reproducing.
- Mutation discipline: T1 must show the CURRENT auto-approval of the
  zero-evidence committer step before the fix.
