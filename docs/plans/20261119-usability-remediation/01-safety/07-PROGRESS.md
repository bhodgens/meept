# 07-replan-storm-plans-panic - PROGRESS

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Leaf: docs/plans/20261119-usability-remediation/01-safety/07-replan-storm-plans-panic.md

## Checklist

- [x] Read leaf doc + gate tests
- [x] Nil-registry panic fix: registry.go Get + GetForTask nil-receiver guards (actionable error, no deref)
- [x] T1: RalphLoopConfig.MaxReplanExtensions (default 2, <0 clamped 0) + extensionsGranted hard budget + failedLatched terminal latch; CheckCompletion + TriggerReplan both consult budget/latch; failTaskAtCap sets latch after successful Update; Reset clears both maps; TriggerReplan also refuses store-failed tasks
- [x] T1b: orchestrator.go ralph branch refuses replan for terminal-failed TaskOutcome
- [x] T3: handler.go claimFailureRelay dedupe (failureRelayMu/failureRelayed) -- task.failed relayed ONCE per task, no fresh-turn re-relay
- [x] T2 services: plan_service.go isNilPlanStore reflect-safe guard in SetEvolverSink + Get + List + ListBySession
- [x] T2 daemon: nilSafeEvolverPlanStore now returns plan.PlanStore interface; nil Components / nil concrete -> true nil; live store identity preserved
- [x] T2 config: OrchestratorConfig.MaxReplanExtensions (default 2) wired in components.go ralphLoopCfg
- [x] T12 plan.failed: manager.go Synthesize park branch publishes plan.failed{plan_id,session_id,reason} (SynthesisError > retErr > generic); failPlan publishes plan.failed naming failed phase
- [x] gofmt applied to all touched files
- [x] Gate tests GREEN (see REGRESSION-RESOLVED checkpoint below)
- [x] 07-REPORT.md
- [x] NO commit (honored)

## REGRESSION-RESOLVED checkpoint (finishing child, 2026-11-20)

Prior child died on upstream 401 after 32 writes; its work was on disk.
This child closed the three remaining jobs:

1. chain_stability round-5 regression vs MaxReplanExtensions ceiling:
   RESOLVED. TestChainStability_RalphLoopProgress drives 6 progress
   rounds (needs 4 extension grants); the F-NEW-10 default ceiling (2)
   capped it. Reconciled IN THE TEST SETUP: the progress-pin test now
   sets rl.config.MaxReplanExtensions = 4 with a comment naming the
   ceiling pin's owner (ralph_storm_test.go/TestReplanHardCeiling keeps
   the DEFAULT). No assertion weakened; the stalled negative control
   still terminalizes at the cap (no progress = no grant at any budget).
   Also fixed the T11 gate fixture: its "unrelated evidence" text leaked
   the word "evidence", a key term of its OWN task description, so
   validateEvidence accepted it and TestReplanHardCeiling completed the
   task at cycle 0 instead of storming (now "unrelated gibberish
   matching nothing here" -- assertions untouched).
   T4 collateral (plan_failure_copy_test.go is a tracked sibling gate,
   RED pre-existing at HEAD on my verify regex): the cap-reached
   failTaskAtCap reason leaked internal vocab; the call sites
   now pass capFailureUserCopy(n) = "could not verify the work after N
   attempts - review the task". TestPlanFailureCopyRalphCap GREEN; the
   revision-cycles sibling (TestPlanFailureCopyRevisionExhaustion)
   stays RED -- owned by 06-planning/03, NOT touched here.
2. TestPlansTypedNilGet: RESOLVED. The typed-nil SetEvolverSink guard
   was fine; the miss path was not -- with the sink ignored, the shared
   store's plan.ErrPlanNotFound surfaced wrapped-but-unmapped and failed
   errors.Is(err, services.ErrNotFound). PlanService.Get now maps
   plan.ErrPlanNotFound -> wrapError("plan","Get",ErrNotFound). Full
   `go test ./internal/services/ -count=1` GREEN.
3. 07-REPORT.md written (this leaf's docs dir).

Shared-tree incident: a sibling session stashed + half-popped the whole
tree mid-run (~10:59); internal/agent/ralph_loop.go came back WITHOUT
the MaxReplanExtensions work. Recovered by re-materializing the file
from stash@{0} (which carried the full prior-child version) and
re-applying this child's capFailureUserCopy edits on top. The sibling's
stash entry stash@{0} was left in place (not dropped) -- it is theirs
to pop/drop.

## Verification (all on the live tree, no overlay needed -- ralph_storm.go compiles)

- go test ./internal/agent/ -run 'Ralph|Replan|ChainStability' -count=1 -> ok
- TestReplanHardCeiling / TestFailedTaskNeverReplans / TestFailureRelayedOnce -> PASS
- TestChainStability_RalphLoopProgress / TestChainStability_RalphLoopStalledStillFails -> PASS
- go test ./internal/services/ -count=1 -> ok (typed-nil trio GREEN)
- go test ./internal/plan/ -run 'TestFailureEvent|TestPlan|TestSynthesize' -> ok
- go test ./internal/daemon/ -run Evolver -> ok; ./internal/bus -> ok
- go build ./... -> clean; gofmt -l on all touched files -> clean

## Notes

- Gate contracts (read once): ceiling = MaxIterations + MaxReplanExtensions; latch survives late completed steps; exactly ONE task.failed; SetEvolverSink ignores typed-nil (fallbackStore/fallbackManager stay nil); nilSafeEvolverPlanStore returns interface type; plan.failed payload {plan_id, session_id, reason} on synthesis park AND failPlan.
- failure_terminal_test.go (leaf 05 gate) NOT on disk in this worktree yet -- sibling owns it; nil-registry guard removes the confirmed panic at registry.go:593 regardless.
