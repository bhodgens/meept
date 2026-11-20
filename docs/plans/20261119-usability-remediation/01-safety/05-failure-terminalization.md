# Failure Terminalization - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T8) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.
> LARGE LEAF (5 seams): if wall-time runs short, finish T1-T3 first, report
> T4-T5 PENDING with disk state - orchestrator re-dispatches the remainder.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Every silent failure path ends in an honest user-visible terminal
- **Dependencies:** 00T-gate.md, 01-reject-honesty.md (handler.go relay region)
- **Estimated Context:** 95K
- **Concurrency Group:** B (after 01 REVIEWED - handler.go overlap)
- **Audit references:** W7, W24, W25, W27; queue-gates #1/#2/#3/#6/#8;
  classifier-routing F6-1/F6-2; audit-routing top-10 #2/#5/#8/#9

## Goal

Close the no-backstop failure paths: pair failures, plan-request errors,
plan synthesis failure, crash-stranded jobs, parked-turn empty replies,
closed-loop turns, hung Close, panic recovery + terminal replay query,
goal-gate park stranding.

## Context

- internal/agent/tactical.go:2479-2503 - OnJobFailed pair branch returns
  before task.error publish; no-live-steps finalize block at ~2735-2774.
- internal/agent/pair_manager.go:354-402 - finalizeTask writes state,
  publishes NOTHING; RunRound error paths ~155-211.
- internal/agent/orchestrator.go:369-375 - handlePlanRequest logs+returns on
  Plan error; strategic.failTaskWithReason (strategic.go:1905-1929) exists.
- internal/plan/manager.go:634-645 - Synthesize error stored, no event;
  failPlan ~1629 deliberately silent; publishPlanState ~1876.
- internal/queue/store.go:1910-1974 - SweepAbandonedTerminalStates moveToDead
  publishes nothing.
- internal/agent/handler.go:1427-1432 - park detection requires err!=nil;
  loop.go:3795-3802 park returns "",nil; handler.go:1254 WithChatTurnID only
  direct mode; loop_park.go:750-761 no-TurnID resume skips delivery.
- internal/agent/loop.go:4442-4451 Close (wg.Wait); manager.go:131-135
  synchronous staleLoop.Close on dispatch path; shadow-capture goroutines
  context.Background() no timeout (~5903-5915, 6571-6585).
- internal/agent/handler.go:749 handleRequest - no recover.
- internal/daemon/gate_park_wiring.go:266-273 - release pump subscribes
  plan.rejected/plan.cancelled ONLY; goal_loop.go:1536 goal rejection.

## Verified Anchors

- internal/agent/tactical.go:2479 - pair branch entry (re-grep at dispatch)
- internal/agent/pair_manager.go:354 - finalizeTask
- internal/agent/orchestrator.go:369 - handlePlanRequest error branch
- internal/plan/manager.go:634 - Synthesize error path
- internal/agent/handler.go:1427 - parked detection
- internal/daemon/gate_park_wiring.go:266 - release pump subscriptions

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C6: plan.failed{plan_id,session_id,reason} event (blocking publish).
- W25 recovery: turn.result RPC {turn_id} -> {status,reply,error,task_id}
  from a persisted terminal outbox (sqlite, bounded retention).

### What This Leaf Consumes
- C1 (terminal keys), C2 (rejected relay pattern to mirror), C6 (blocking
  publish list), C7 (exempt states - do not re-ap them).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Pair failure terminalization | Modify: internal/agent/tactical.go pair branch; internal/agent/pair_manager.go finalizeTask+RunRound | `go test ./internal/agent/ -run TestPairFailureTerminal -count=1` | gate T8 RED (silent) | GREEN | Given pair step fails/exhausts; Then task.failed published with jobErr, turn terminal failed; converged pairs relay completed |
| T2 | Plan-request + synthesis failure | Modify: internal/agent/orchestrator.go:369 region; internal/plan/manager.go Synthesize/failPlan | `go test ./internal/agent/ ./internal/plan/ -run 'TestPlanRequestErrorTerminal|TestSynthesisFailureEvent' -count=1` | gate T8 RED (log-only) | GREEN | Given Plan() error; Then failTaskWithReason -> task.failed -> terminal; synthesis park publishes plan.failed{reason} session-scoped |
| T3 | Sweeper + parked-turn honesty | Modify: internal/queue/store.go sweeper; internal/agent/handler.go park detection + WithChatTurnID routed path; loop_park.go resume delivery | `go test ./internal/agent/ ./internal/queue/ -run 'TestSweeperPublishes|TestParkEmptyReply' -count=1` | gate T8 RED (empty completed) | GREEN | Given stranded dead job; Then queue.job.failed published; fresh throttle park terminalizes parked (not completed+empty); routed park resume delivers under originating turn id |
| T4 | Closed-loop guard + bounded Close + goal release | Modify: internal/agent/loop.go (ErrLoopClosed + RunOnce guard + shadow-capture timeouts); internal/agent/manager.go (async stale Close); internal/daemon/gate_park_wiring.go (goal.rejected/goal.cancelled subs) | `go test ./internal/agent/ ./internal/daemon/ -run 'TestClosedLoopGuard|TestGoalRelease' -count=1` | no ErrLoopClosed; Close unbounded; goal subs absent | GREEN | Given closed loop; When turn submitted; Then ErrLoopClosed failed terminal; hung shadow capture cannot block dispatch >30s; goal rejection releases park |
| T5 | Panic recovery + terminal outbox | Modify: internal/agent/handler.go handleRequest (recover->failed terminal); Create: internal/agent/turn_outbox.go + RPC turn.result; wire outbox write in publishTurnTerminal | `go test ./internal/agent/ ./internal/rpc/ -run 'TestPanicRecovery|TestTurnResultQuery' -count=1` | gate T8 RED (no recover/outbox) | GREEN | Given panic mid-turn; Then failed terminal emitted, daemon alive; lost-event client queries turn.result and gets the outcome |

## Self-Verification Checklist

- [ ] Gate T8 GREEN; gate diff empty
- [ ] C1/C6 exact; plan.failed declared in ws schema if WS-relayed
- [ ] Live-gate: rig injects pair failure + plan error (fake provider), terminals observed; panic test via fault-injection hook
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output per task. 3. Live-gate evidence. 4. Deviations/PENDING halves.

## Review Checklist (For Review Agent)

- [ ] Every former silent path now emits exactly ONE terminal (no doubles)
- [ ] Outbox bounded (retention cap), write never blocks the funnel >5s
- [ ] recover() does not swallow programming errors silently (logs Error + stack)
- [ ] Shadow-capture timeout does not lose evidence on healthy paths
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- F-ORCH-28 semaphore lease leak is NOT here (02-routing/04 instruments first).
- If T4/T5 timeout: report disk state; orchestrator re-dispatches remainder
  as 05b (pitfall 25).
