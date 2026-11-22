# Follow-up Continuation + Boot Recovery - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T4/T5) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Refinements reach the parked draft; boot restores follow-ups; thread resolved once
- **Dependencies:** 00T-gate.md, 01-safety/02 (C3), 02-dispatch-evidence.md (dispatcher.go serialization)
- **Estimated Context:** 85K
- **Concurrency Group:** C
- **Audit references:** W13; R7/A9; session-steering A6/B7; F-ORCH-27 correlate

## Goal

Close the plan-compiler follow-up gap (refinement text routes to SaveDraft
on the SAME parked task, not a new task); boot recovery re-enqueues
persisted follow-ups instead of deleting them; thread topic resolved once
per turn and carried on DispatchResult.

## Context

- internal/agent/dispatcher.go:1449/2592 - new-task creation path;
  shouldCreateTask gate ~2527; no planning-session awareness except
  session_state_gate.go quickplan upgrade.
- internal/agent/plan_draft.go:115 SaveDraft (state guard: planning only),
  :483 seedDraftFromRequest; rpc/plan_seal.go:224-227 SaveDraft RPC seam.
- internal/agent/queue_recovery.go:54/82-114 - loadAndClear SELECT+DELETE,
  count event only; daemon.go:2578 wiring.
- internal/agent/dispatcher.go:953-967 vs 3067-3078 - double thread
  resolution (full input vs Intent.Summary); extractSummary :4206.
- internal/session/thread_resolve.go:95 - unwrap helper.

## Verified Anchors

- internal/agent/dispatcher.go:1449 - new task creation (re-grep at dispatch)
- internal/agent/plan_draft.go:115 - SaveDraft
- internal/agent/queue_recovery.go:109 - DELETE site
- internal/agent/dispatcher.go:955 - first thread resolution

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Continuation route: session with StatePlanning task + plan_draft metadata
  + non-command input -> SaveDraft(same task) + reply with updated draft
  status (no new task). DispatchResult.ThreadConversationID single source.
- Boot inbox: recovered follow-ups re-enqueued to the session's next turn.

### What This Leaf Consumes
- C3 (queue lifecycle/resolution), C5 (trace records continuation handler_case).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Draft refinement route | Modify: internal/agent/dispatcher.go (pre-task-creation branch); internal/agent/plan_draft.go (refinement entry if needed) | `go test ./internal/agent/ -run TestDraftRefinementRoute -count=1` | gate T4 RED (new task today) | GREEN | Given parked planning task with draft; When "make it five languages instead"; Then SaveDraft on SAME task, reply names draft version, NO new task; new-work commands still create tasks |
| T2 | Boot follow-up re-enqueue | Modify: internal/agent/queue_recovery.go; internal/daemon/daemon.go wiring region | `go test ./internal/agent/ -run TestBootFollowupReenqueue -count=1` | gate T5 RED (deletes content) | GREEN | Given persisted follow-ups; When boot; Then content re-enqueued to durable session inbox, rows deleted only after injection; count event reflects reality |
| T3 | Single thread resolution | Modify: internal/agent/dispatcher.go (resolve once in ClassifyAndRoute, carry on DispatchResult; RouteToAgent reuses) | `go test ./internal/agent/ -run TestThreadResolveOnce -count=1` | gate T4 RED (double resolve) | GREEN | Given topic keyword past char 100; Then memory key == queue key for the turn |

## Self-Verification Checklist

- [ ] Gate T4/T5 GREEN; gate diff empty
- [ ] Live-gate: rig p4-style sequence - plan request, refinement follow-up -> same task draft updated; daemon restart with pending follow-up -> message delivered next turn
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Refinement vs new-work discrimination pinned both directions
- [ ] SaveDraft state guard respected (never writes to non-planning task)
- [ ] Recovery idempotent (crash between inject and delete never duplicates)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- The draft CONTENT refinement (LLM brainstorm loop filling the scaffold)
  is 06-planning/03; this leaf only routes the text to the draft seam.
