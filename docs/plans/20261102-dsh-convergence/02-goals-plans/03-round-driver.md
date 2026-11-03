# 03-round-driver - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate —
> the failing tests ALREADY EXIST (00T-tests.md, goals-plans e2e
> `round_driver_test.go` G3) and are committed — your job is to make them pass;
> do NOT delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** The autonomous round driver: when a goal is active + armed and the
  agent is settled/idle, queue round N+1 with ZERO human turns re-entering the
  GoalLoop; persist round admission (CAS on revision); wait on C1 children
  (01-delegation); auto-block at the round cap. Reuses the existing
  scheduler/queue deferred-turn + Manager.Trigger path — no new loop.
- **Dependencies:** 01-goal-model.md (C4a — armed/rounds/maxRounds/blocked);
  parent 01-delegation (C1 children — nil-safe seam, not a hard dep)
- **Estimated Context:** 40K
- **Concurrency Group:** C (after 01 REVIEWED and parent 01-delegation REVIEWED)
- **Audit references:** /tmp/dsh-audit/goal-workflow-plan.md Feature 1 (round
  driver: drive() :146-221, admission fences :364-445)

## Goal

Tier-3 plan execution already runs without a signoff gate (decideTier3). What
is missing is the CONTINUATION driver: after a round settles at idle with a
still-active, still-armed goal, meept currently waits for the next
cron/webhook/user trigger. This leaf makes the goal fully autonomous — it
queues the next round itself, with zero human turns, until the goal completes,
blocks, or exhausts its round budget, and it never double-starts or replays a
stale round (admission fences).

## Context

- **Trigger path (reused, verified):** `employee.Manager.Trigger(ctx, id,
  payload)` (manager.go:1482) -> `triggerViaGoalLoop` (manager.go:1540) ->
  `loop.Decide(ctx, TriggerEvent{...})` (goal_loop.go:1376). The driver does
  NOT call Decide directly; it schedules a deferred turn that re-enters this
  path with a `source:"goal"` payload carrying `round`, `goal_id`, `revision`.
- **Deferred-turn reuse:** `scheduler.Scheduler.ScheduleConfig(JobConfig)` for
  a one-shot interval/agent job; `JobTypeInterval` placeholder re-registered by
  the employee scheduler adapter (jobs.go:140-161); `AgentJob.Execute`
  publishes `source:"scheduler"` on `chat.request` (jobs.go:212-247). The
  driver schedules a ONE-SHOT job whose payload routes back to
  Manager.Trigger for that employee with the goal round identity.
- **Idle/settled signal:** the driver must know the agent reached quiescence.
  Follow the existing AgentLoop idle/settled + deferred-turn seam used by the
  01-delegation branch (master C1/C2); if a direct "idle" callback is not
  exposed, use the settle signal the branch 01 child-wait already consumes.
- **Round-cap default:** `DefaultMaxGoalRounds = 256` (leaf 01). Auto-block
  message mirrors dsh: code `round-limit`, "Goal reached its configured limit
  of N rounds."
- **C1 child wait:** before reserving round N+1, wait for in-flight
  01-delegation children of this employee to settle (`subagent_send_message`
  resume path). This is a NIL-SAFE seam: when 01-delegation is not yet wired,
  the wait is a no-op (the driver still functions; the e2e G4 asserts this).

## Verified Anchors

- internal/employee/manager.go:1482-1534 — `Trigger(...)` entry
- internal/employee/manager.go:1540-1609 — `triggerViaGoalLoop(...)` + per-employee invoke mutex
- internal/employee/goal_loop.go:1376-1404 — `Decide(...)` tier dispatch
- internal/employee/goal.go — `Activate/Disarm/IsArmed`, `RoundsStarted`, `MaxRounds`, `BlockedReason` (leaf 01)
- internal/scheduler/scheduler.go — `ScheduleConfig`; jobs.go:140-247 — Interval/Agent job + chat.request publish
- 01-delegation branch — C1 child registry (parent tree; verify it exists before dispatch)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/employee — name/shape is the contract, verbatim from C4c:
type GoalRoundDriver struct { /* per-employee serialized state */ }
func NewGoalRoundDriver(mgr *Manager, sched *scheduler.Scheduler, settleSignal <-chan struct{}) *GoalRoundDriver
func (d *GoalRoundDriver) Start()                // install per-employee driver hooks
func (d *GoalRoundDriver) Stop()
// Called on each round settle / trigger return:
func (d *GoalRoundDriver) OnRoundSettled(employeeID string)
// Admission before the queued round enters the loop; must reject stale:
func ValidGoalRound(goal *Goal, round, revision int64) bool
// Sets the autonomous-round flag leaf 02 reads (thread-hop):
func MarkAutonomousRound(ctx context.Context, on bool)
// Auto-block edge (shared with leaf 02):
func BlockGoal(ctx context.Context, mgr *Manager, goal *Goal, code, message string) error
```

### What This Leaf Consumes

- C4a (leaf 01) — `IsArmed`, `RoundsStarted`, `MaxRounds`, `BlockedReason`, `state`.
- `employee.Manager.Trigger` (existing).
- `scheduler.Scheduler.ScheduleConfig` for the one-shot deferred turn (existing).
- C1 children (parent 01-delegation) — wait, nil-safe.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement the round-admission predicate `ValidGoalRound` (reject stale revision / non-sequential round) | Create: `internal/employee/goal_round_driver.go` | `go test -p 2 ./internal/employee/ -run ValidGoalRound -v` (impl-local unit, not gate) | FAIL: function absent → won't compile | PASS: exact `revision` + `round==RoundsStarted+1` accepted; anything else rejected | Given a goal at revision 2, roundsStarted 1; When round=2 rev=2; Then valid; round=3 or rev=1 invalid |
| T2 | Implement the driver's `OnRoundSettled`: if goal active+armed+settled and `RoundsStarted < MaxRounds`, reserve round N+1 (CAS increment RoundsStarted), wait on C1 children (nil-safe), schedule the one-shot deferred turn re-entering Trigger with `source:"goal", round, goal_id, revision`; else auto-block at cap | Modify: `internal/employee/goal_round_driver.go` | `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -run RoundDriver -v` (gate G3) | FAIL: round N+1 never queued on a settled armed goal | PASS: a MaxRounds=2 goal advances to round 2 with zero human turns; the deferred turn re-enters the loop with the reserved identity | Given an armed active goal, MaxRounds=2, agent idle; When OnRoundSettled fires; Then round 2 is queued and runs unattended |
| T3 | Auto-block on round-limit: when `RoundsStarted >= MaxRounds`, set State=GoalBlocked + BlockedReason{code:`round-limit`} and Disarm | Modify: `internal/employee/goal_round_driver.go` | gate G3 same | FAIL: no block at cap | PASS: after the final round the goal phase is `blocked`, blockedReason.code is `round-limit`, and it is disarmed | Given MaxRounds=2 goal after round 2; When OnRoundSettled fires again; Then goal blocks and no round 3 starts |
| T4 | Disarm-on-error + provider-wait safety: on a Decide error / provider wait / max-tokens outcome, disarm instead of looping; race fences prevent a stale round entering a changed goal | Modify: `internal/employee/goal_round_driver.go` + integration in manager trigger path | gate G3 same | FAIL: driver retries on error instead of disarming | PASS: an errored/settled round disarms the goal; a stale reserved round is rejected by T1 before entering Decide | Given a round that errors; When the loop settles; Then the goal is disarmed and no further rounds queue |
| T5 | Wire `MarkAutonomousRound` to leaf 02 so complete/blocked are legal during continuation; set it true only on the reserved round's step, false otherwise | Modify: `internal/employee/goal_round_driver.go` + leaf-02 accessor | `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -v` (G3 + G2 combined) | FAIL: autonomous tools legal only during a continuation turn | PASS: inside a reserved goal round, `goal_update complete/blocked` works; outside, direct-human is required | Given a continuation round in flight; When the model completes the goal; Then it succeeds and the goal is done |

## Self-Verification Checklist

- [ ] All tasks implemented and gate G3 passing (RED -> GREEN)
- [ ] Contract C4c satisfied exactly (type names, method signatures, auto-block code/message)
- [ ] Zero human turns verified — the continuation is a scheduled trigger, not a user prompt
- [ ] Nil-safe C1 child wait: driver works with 01-delegation absent (G4 e2e asserts)
- [ ] No new queue/scheduler primitives; only ScheduleConfig reuse
- [ ] No scope creep — no plan-mode, no direct Decide bypass, no config surface

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path
2. **Verify command + output:** `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -run RoundDriver -v` last 20 lines; `gofmt -l` clean
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate G3 untouched (git diff empty)
- [ ] Contract C4c matches exactly; round-limit block code/message verbatim
- [ ] No direct Decide call — always through Manager.Trigger / deferred turn
- [ ] Conventions: no ignored errors, predictable-ID, no mutex across I/O, typed-nil guards on the C1 wait seam
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh driver to re-read: `/tmp/deepseek-harness/packages/goal/goal-round-driver/src/index.ts`
  — especially `drive()` (:146-221), the round-limit auto-block (:182-188), the
  user-message source `{kind:'goal', goalId, revision, round}` (:190-195), and
  the `validReservation` race fences (:364-378). Port the FENCES (stale-round
  rejection) faithfully; the meept deferred-turn mechanics replace dsh's
  `agent.followup` but the ADMISSION contract is the same.
- The orchestrator must confirm parent 01-delegation is REVIEWED before
  dispatching this leaf (set in the orchestrator's Dispatch Protocol). If it is
  not, the C1 wait ships as a documented nil-safe no-op and 03 is still
  dispatchable — but the orchestrator decides at dispatch time.
- signature risk: the `settleSignal` channel and `ScheduleConfig` call shape
  must be read from the CURRENT internal/scheduler source before coding — the
  anchors above are the re-read point; update to the live signature if it
  drifted.
