# Replan Storm Cap + Plans REST Typed-Nil Panic - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T11/T12) - make them pass; do NOT delete/skip/weaken
> them. Do NOT commit. Do NOT git add. Explore with search_files/terminal
> cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** A failed task stops replanning at a hard ceiling; the plans REST endpoint never panics on a typed-nil sink store
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Audit references:** F-NEW-10 (replan storm), F-NEW-1 (plans panic);
  REPORT-GUI-RERUN.md sections 3; ralph_loop.go madeForwardProgress;
  internal/daemon/AGENTS.md "Typed-nil wiring" rule

## Goal

Close the two NEW critical findings from the GUI rerun:
(a) F-NEW-10: a failed task replanned 113x over 30+ min because the
"forward progress" cap-extension grants a budget reset on EVERY cycle
(each replan adds completed steps, so completed_steps grows 10->109 and
madeForwardProgress keeps returning true). The MaxIterations cap never
sticks. Add a HARD absolute ceiling on replan extensions per task; a
terminal-failed task must never replan; re-relaying the same failure under
fresh turn_ids must stop.
(b) F-NEW-1: GET /api/v1/plans panics because a nil *plan.SQLiteStore boxed
into the plan.PlanStore interface field defeats the != nil guard
(SetEvolverSink + nilSafeEvolverPlanStore). Apply the typed-nil rule from
internal/daemon/AGENTS.md at the wiring seam AND make the service guard
reflect-safe.

## Context

- internal/agent/ralph_loop.go:407 TriggerReplan; :413-431 cap + progress
  extension (madeForwardProgress resets iterations to MaxIterations-1 each
  time completed-step count grows); :112 madeForwardProgress; :94
  taskProgress (CountByState completed+approved); :541 failTaskAtCap
  (F6: never re-terminalize - returns early if already terminal).
- internal/agent/orchestrator.go:452-478 - OnJobCompleted ralph branch:
  TaskIsTerminal -> only Reset if TaskOutcome complete; else CheckCompletion
  -> TriggerReplan. The storm task was StateFailed but kept replanning:
  the replan path re-runs steps that complete/approve, growing taskProgress,
  so the extension fires endlessly. The terminal guard at :452 is bypassed
  because replan creates NEW non-terminal steps under the same task.
- internal/services/plan_service.go:27-33 SetEvolverSink (plain != nil);
  :113 store nil guard; :126-127 fallbackStore != nil guard (typed-nil
  passes); :98-100 GetPlan fallback same class.
- internal/daemon/daemon.go:1215 FallbackPlanStore: nilSafeEvolverPlanStore;
  :3481-3486 returns *plan.SQLiteStore (concrete) -> boxed nil-as-non-nil.
- internal/services/service.go:92 - PlanStore interface field.
- REPRO: /tmp/usability-rig/repro/plan-nil-panic-repro.go (byte-identical
  isolated repro); daemon.log 18:01:15/32 panics; storm task
  task-20261120011106.011798000-0010 (153 Triggered replan, 146 at
  iteration=3, 144 extension grants, completed_steps 10->109).

## Verified Anchors

- internal/agent/ralph_loop.go:407 - TriggerReplan
- internal/agent/ralph_loop.go:413 - cap + madeForwardProgress extension
- internal/agent/ralph_loop.go:112 - madeForwardProgress
- internal/agent/orchestrator.go:452 - TaskIsTerminal ralph branch
- internal/services/plan_service.go:27 - SetEvolverSink
- internal/services/plan_service.go:126 - fallbackStore guard
- internal/daemon/daemon.go:3481 - nilSafeEvolverPlanStore

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Hard replan ceiling: orchestrator.max_replan_extensions (default 2) per
  task; total replans bounded by MaxIterations + extensions; a task that
  hit failTaskAtCap NEVER replans again (terminal-failed latch).
- Plans service typed-nil safety: SetEvolverSink + List/Get guards reject
  typed-nil interface values (reflect or concrete-nil check at wiring).

### What This Leaf Consumes
- C6 (task.failed blocking publish - failure relayed ONCE), C1 (terminal).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Hard replan ceiling + terminal latch | Modify: internal/agent/ralph_loop.go (extension counter + cap; latch after failTaskAtCap); internal/config/schema.go (max_replan_extensions); orchestrator.go ralph branch (never replan a failed-terminal task) | `go test ./internal/agent/ -run 'TestReplanHardCeiling|TestFailedTaskNeverReplans' -count=1` | gate T11 RED (113 replans today) | GREEN | Given a task failing evidence checks repeatedly with growing step count; Then replans stop at MaxIterations+max_replan_extensions; failTaskAtCap latches; ONE failure terminal, no fresh-turn_id re-relay storm |
| T2 | Plans typed-nil guard | Modify: internal/services/plan_service.go SetEvolverSink + List/Get guards (reflect-safe nil check); internal/daemon/daemon.go nilSafeEvolverPlanStore (return interface nil when concrete nil) | `go test ./internal/services/ ./internal/daemon/ -run 'TestPlansTypedNil|TestNilSafeEvolverStore' -count=1` | gate T12 RED (panic today) | GREEN | Given evolver sink disabled (nil concrete store); When GET /api/v1/plans; Then 200 with shared-store plans (no panic, no typed-nil deref); nil-dependency test through REAL wiring function |
| T3 | Failure relayed once | Modify: internal/agent/orchestrator.go + handler.go (dedupe failure relay per task; never fresh turn_id for an already-terminal task's re-failure) | `go test ./internal/agent/ -run TestFailureRelayedOnce -count=1` (add) | 43 re-relays today | GREEN | Given a task that failed and terminalized; Then subsequent internal cycles do NOT re-push task.failed under new turn_ids; the user sees the failure once |

## Self-Verification Checklist

- [ ] Gate T11/T12 GREEN; gate diff empty
- [ ] Hard ceiling config-gated; default bounds the storm (<=5 total replans)
- [ ] Plans endpoint 200 on default-config daemon (live-gate, no panic)
- [ ] Typed-nil test through REAL wiring (not hand-built struct) per AGENTS.md
- [ ] No scope creep / artifacts / corruption; gofmt clean

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence (plans 200 + storm bounded). 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Extension counter never resets mid-task (the storm root cause)
- [ ] failTaskAtCap latch survives across replan-created steps
- [ ] Typed-nil guard at BOTH wiring seam and service (defense in depth)
- [ ] recover() in HTTP handler logs debug.Stack() (AGENTS.md rule) if added
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- F-NEW-10 is the biggest usability MULTIPLIER (floods bus -> F-NEW-3
  terminal loss -> starves every other probe). Fixing it amplifies C6.
- The storm is LATENT (ralph_loop.go byte-identical between bases), not a
  regression from the 10 new commits - confirmed by the rerun report.
- madeForwardProgress "first observation baseline" logic is correct; the
  defect is the UNBOUNDED extension grant, not the progress detection.
