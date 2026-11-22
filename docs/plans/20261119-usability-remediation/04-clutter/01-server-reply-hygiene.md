# Server Reply Hygiene - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1-T4) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Daemon-side replies are honest, user-shaped, and quiet
- **Dependencies:** 00T-gate.md, 01-safety/05 (handler.go region serialization)
- **Estimated Context:** 85K
- **Concurrency Group:** B
- **Audit references:** W3, W15; U1/U3/U4/U5/U8; F-ORCH-4/6/15/21/25;
  F-NEW-8 (firewall truncation double-logged + silent, 648 WARNs)

## Goal

Never ship empty completed replies; strip claims/evidence envelopes on the
relay path; keep review feedback out of chat; silence side-job completion
noise; apply scaffolding strip on task-detail render feeds.

## Context

- internal/agent/handler.go:2248-2294 formatTaskCompletedMessage;
  bestSummaryResult ~2481-2501; route_to_agent completion (~1254 region)
  ships loop finalResponse even when empty.
- internal/agent/loop.go RunOnceWithParts ~3321 - tool-only turns end with
  empty finalResponse; tool ledger available (session_tool_calls writer
  handler.go:1586-1590; turnToolCalls :1684).
- internal/agent - StripClaimsEvidenceOrProse / StripPromptScaffolding
  (existing strippers; extend, never fork).
- internal/agent/handler.go - review feedback chat push ("## review:");
  internal/agent/orchestrator.go - "failed to find step for job" ERROR on
  empty-step_id side jobs.
- internal/task/store.go - task_steps descriptions carry planner scaffolding
  (task-detail render feed).

## Verified Anchors

- internal/agent/handler.go:2248 - formatTaskCompletedMessage
- internal/agent/handler.go:2481 - bestSummaryResult
- internal/agent/handler.go:1684 - turnToolCalls extraction
- internal/agent/loop.go:3321 - RunOnceWithParts (re-grep)

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Reply hygiene: completed replies non-empty (ledger synthesis or one
  bounded summary re-prompt); relay envelopes stripped; review feedback
  task-detail-only; side-job completions quiet; C1 reply_source set
  ("synthesized" when ledger-derived).

### What This Leaf Consumes
- C1 (reply_source key), C6 (relay publishes blocking - already landed).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Empty-reply synthesis | Modify: internal/agent/handler.go route_to_agent completion; loop.go finalResponse seam (one bounded re-prompt option) | `go test ./internal/agent/ -run TestEmptyReplySynthesis -count=1` | gate T1 RED | GREEN | Given tool-only completed turn; Then reply names effects from ledger (paths written, commands run); reply_source=synthesized; never empty completed |
| T2 | Relay envelope strip | Modify: internal/agent/handler.go bestSummaryResult/formatTaskCompletedMessage (extend StripClaimsEvidenceOrProse) | `go test ./internal/agent/ -run TestRelayEnvelopeStrip -count=1` | gate T2 RED | GREEN | Given accomplished/not_done/issues envelope; Then prose recovered; no raw JSON keys; legacy envelopes still stripped |
| T3 | Review feedback routing | Modify: internal/agent/handler.go review-feedback push site | `go test ./internal/agent/ -run TestReviewFeedbackNotChat -count=1` | gate T3 RED | GREEN | Given review verdict; Then task detail/event carries feedback; chat.response never carries "## review:" |
| T4 | Side-job noise + detail strip + firewall disclosure | Modify: internal/agent/orchestrator.go job-completion handler (empty step_id branch); task-detail feed strip (StripPromptScaffolding on step descriptions for client consumption); internal/llm/context_firewall.go (dedupe double truncation WARN, F-NEW-8; disclose truncation via C1 routing_notice or a brief system note) | `go test ./internal/agent/ ./internal/llm/ -run 'TestSideJobCompletionQuiet|TestFirewallTruncationDisclosed' -count=1` | gate T4 RED; 648 double WARNs today | GREEN | Given memory-capture side job; Then Debug not ERROR, no chat event; task.list_extended step descriptions scaffolding-stripped for display (raw preserved in store); oversized-system truncation logs ONE WARN + user-visible disclosure (never silent) |

## Self-Verification Checklist

- [ ] Gate T1-T4 GREEN; gate diff empty
- [ ] C1 reply_source declared in ws schema
- [ ] Live-gate: rig p1 T1 -> non-empty reply naming hello.txt path; p6 T1 -> no raw envelope; no "## review:" in chat capture
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Synthesis never fabricates effects absent from the ledger
- [ ] One bounded re-prompt max (no unbounded loop); guard caps respected
- [ ] Strippers extended in place (no parallel fork)
- [ ] Raw step descriptions preserved in store (strip is display-only)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Planner JSON passing through the user-reply guard (F-ORCH-5 correlate):
  planner output is internal machinery - ensure the guard applies only to
  USER-facing replies, not plan-task intermediate output (reduces the
  double rewrite-retry churn).
