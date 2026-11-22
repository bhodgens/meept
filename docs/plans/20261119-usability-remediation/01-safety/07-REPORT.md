# 07-replan-storm-plans-panic - REPORT

Leaf: docs/plans/20261119-usability-remediation/01-safety/07-replan-storm-plans-panic.md
Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Status: COMPLETE (no commit, per leaf contract)
Delivered across two children: the prior child (died on upstream 401 with
32 writes on disk) plus this finishing child.

## What shipped

### T1 -- replan storm hard ceiling (F-NEW-10)

- RalphLoopConfig.MaxReplanExtensions (default 2, negative clamped to 0),
  wired through config/schema.go + OrchestratorConfig and daemon
  components.go ralphLoopCfg.
- extensionsGranted map: per-task, NEVER resets mid-task; each
  progress-based cap extension consumes one grant; at the budget the cap
  sticks. Total replans bounded by MaxIterations + MaxReplanExtensions
  regardless of how much the completed-step count grows (the storm ran
  113 replans; each replan grew progress, so the old extension grants
  were unbounded).
- failedLatched map: once failTaskAtCap terminalizes a task, TriggerReplan
  and CheckCompletion refuse it forever (survives replan-created steps
  completing late -- the shape that bypassed TaskIsTerminal in the live
  storm). TriggerReplan additionally refuses any store-failed task.
  Reset(taskID) clears iterations/progressSeen/extensionsGranted/
  failedLatched for genuinely new runs.
- Orchestrator ralph branch refuses replan for a terminal-failed
  TaskOutcome (belt + braces with the latch).
- task.failed publishes via bus.PublishBlocking so a full subscriber
  buffer cannot silently drop the terminal (01-safety/03 seam).

### T3 -- failure relayed exactly once

- handler.go claimFailureRelay dedupe (failureRelayMu/failureRelayed):
  a re-reported task.failed for an already-terminalized task never
  relays a second turn terminal under a fresh turn id (43 re-relays in
  the rerun; now exactly ONE).

### T2 -- GET /api/v1/plans typed-nil panic (F-NEW-1)

- daemon nilSafeEvolverPlanStore returns the plan.PlanStore interface
  (a nil concrete *plan.SQLiteStore boxed into the interface passed the
  old != nil guard and List/Get dereferenced the nil receiver -> panic).
  Nil Components / nil concrete store now yield a true nil interface.
- services PlanService.SetEvolverSink ignores typed-nil values via
  isNilPlanStore (reflect-safe: Ptr/Map/Slice/Interface/Func/Chan
  IsNil), as defense in depth.
- Finishing child: PlanService.Get mapped the shared store's clean miss
  (plan.ErrPlanNotFound) to wrapError("plan","Get",ErrNotFound). With
  the sink ignored the fallback branch never runs, so the store
  sentinel surfaced unmapped and errors.Is(err, services.ErrNotFound)
  failed. Fix: explicit errors.Is mapping before the generic wrap.
  TestPlansTypedNilGet + List + SinkIgnored all GREEN; full
  ./internal/services package GREEN.

### T12 -- plan.failed events

- plan/manager.go Synthesize park branch publishes
  plan.failed{plan_id, session_id, reason} (SynthesisError > retErr >
  generic); failPlan publishes plan.failed naming the failed phase.
  services/plan_service.go + daemon/components.go plumbing updated to
  the interface-typed store.

### Nil-registry panic guard

- registry.go Get + GetForTask nil-receiver guards return an actionable
  error instead of dereferencing (the confirmed panic at
  registry.go:593).

## Regression reconciliation (this child)

- TestChainStability_RalphLoopProgress (tracked pre-existing pin) broke
  because F-NEW-10's default extension budget (2) caps total replans at
  5 while the pin drives 6 progress rounds (7 replans). Reconciled in
  TEST SETUP only: the progress-pin raises
  rl.config.MaxReplanExtensions = 4 with a comment naming
  TestReplanHardCeiling as the default-ceiling owner. The production
  ceiling logic is untouched and NO assertion was weakened; the stalled
  negative control (TestChainStability_RalphLoopStalledStillFails) still
  terminalizes at the cap at any budget, because a grant requires
  observed progress.
- T11 gate fixture bug found while reconciling: ralph_storm_test.go's
  evidence string "unrelated evidence matching none of the key terms"
  contains "evidence", a key term of its own task description, so
  validateEvidence scored the evidence SUFFICIENT and
  TestReplanHardCeiling completed the task at cycle 0 instead of
  storming. Fixed the fixture text (now shares no >3-char word with the
  description); gate assertions untouched. This was a latent defect in
  the gate file itself -- it could never have exercised the ceiling.
- T4 collateral fix (needed because the leaf's verify regex 'Ralph'
  includes the tracked sibling gate TestPlanFailureCopyRalphCap, RED at
  HEAD): the three failTaskAtCap call sites now pass
  capFailureUserCopy(n) = "could not verify the work after N attempts -
  review the task" instead of the internal-vocabulary reasons ("max
  ralph loop iterations reached...", "replan iteration cap reached").
  TestPlanFailureCopyRalphCap GREEN. The sibling gate
  TestPlanFailureCopyRevisionExhaustion (review_manager.go copy) stays
  RED -- owned by 06-planning/03, deliberately not touched here.

## Shared-tree incident (disclosure)

A sibling session stashed the whole tree and half-popped it mid-run
(~10:59 MST): internal/agent/ralph_loop.go came back WITHOUT the
MaxReplanExtensions work while sibling-new edits (PublishBlocking call)
were present. Recovered by re-materializing ralph_loop.go from
stash@{0} (which carried the prior child's complete version, PublishBlocking
included) and re-applying this child's capFailureUserCopy edits on top.
The sibling's stash@{0} entry was left in place -- dropping it is the
owning session's call. All other leaf files survived byte-identical to
the stash (verified with git diff stash@{0} per file).

## Verification evidence (live tree; no overlay -- ralph_storm_test.go compiles with MaxReplanExtensions present)

| Command | Result |
|---|---|
| go test ./internal/agent/ -run 'Ralph\|Replan\|ChainStability' -count=1 | ok |
| TestReplanHardCeiling / TestFailedTaskNeverReplans / TestFailureRelayedOnce (T11 members) | PASS / PASS / PASS |
| TestChainStability_RalphLoopProgress / TestChainStability_RalphLoopStalledStillFails | PASS / PASS |
| TestPlanFailureCopyRalphCap | PASS |
| go test ./internal/services/ -count=1 | ok (typed-nil trio included) |
| go test ./internal/plan/ -run 'TestFailureEvent\|TestPlan\|TestSynthesize' -count=1 | ok |
| go test ./internal/daemon/ -run Evolver / ./internal/bus -count=1 | ok / ok |
| go build ./... | clean |
| gofmt -l on all touched files | clean |

## Files touched (this leaf, both children combined)

- internal/agent/ralph_loop.go (ceiling, latch, budget, user copy)
- internal/agent/orchestrator.go (terminal-failed replan refusal)
- internal/agent/handler.go (failure relay dedupe)
- internal/agent/registry.go (nil-receiver guards)
- internal/config/schema.go + internal/daemon/components.go (MaxReplanExtensions wiring, nil-safe evolver store)
- internal/plan/manager.go (plan.failed events)
- internal/services/plan_service.go (typed-nil guards + ErrNotFound mapping)
- internal/bus/bus.go (PublishBlocking seam, shared with 01-safety/03)
- internal/agent/chain_stability_test.go (setup-only extension-budget raise on the progress pin)
- internal/agent/ralph_storm_test.go (fixture text defect fix; assertions untouched)
- docs/plans/.../01-safety/07-PROGRESS.md, 07-REPORT.md

## Open items for the orchestrator

- No commit made (leaf contract). Tree carries sibling WIP; commit by
  concern with explicit pathspecs per the shared-tree protocol.
- TestPlanFailureCopyRevisionExhaustion remains RED pending
  06-planning/03 (review_manager.go copy) -- not this leaf's scope.
- internal/agent/failure_terminal_test.go (leaf 05 gate) is present and
  mostly green via the leaf-07 handler dedupe + latch work;
  TestPlanRequestErrorTerminal stays RED -- handlePlanRequest
  terminalization is leaf 05's own open task (05-PROGRESS T2), not this
  leaf's scope.
