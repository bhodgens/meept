# Plan/Goal Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY, fail RED for missing-implementation reasons. Do NOT implement production code. Do NOT commit.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Gate tests for plan/goal branch features (M-6..M-11, F7, F8, F11, F16).
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** M-6..M-11, F7, F8, F11, F16

## Goal

Pin: reclaimer re-drive, phase failure writers, CAS transitions, goal fence, retire consistency, hygiene items.

## Context

- internal/plan/manager.go + store_sqlite.go; internal/employee/goal.go + goal_tools.go
- Survey existing package test helpers first.

## Verified Anchors

Re-grep at dispatch: plan.go:43-44 (dead enum values); manager.go:188 (RecoverStrandedSynthesis), :315/341/445/566 (SetPlanState sites), :705 (ClaimPlanSynthesis caller); store_sqlite.go:343/361; goal.go:920/1023 (Retire dead code); goal_tools.go:459-476.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test functions:
TestSynthesisReclaimerRedrivesApproved
TestSynthesisReclaimerPeriodic (ticker-level)
TestPhaseFailedWriter
TestPhaseConfirmedWriter
TestUpdatePlanStateWhere
TestManagerTransitionsUseCAS (submit/cancel/confirm race shapes)
TestGoalTransitionFence
TestRetireSetsRetiredAt
TestPausedGoalSkipsAssessmentWrites
TestRevisePlanAcceptsFailed
TestCancelCancelsPhases
TestMarkReadRequiresDelivered
```

### What This Leaf Consumes

Parent contracts C-P1..C-P3 (compile targets).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Reclaimer tests | Test: internal/plan/reclaimer_gate_test.go | `go test -p 2 ./internal/plan/ -run TestSynthesisReclaimer` | StartSynthesisReclaimer undefined; reclaim-to-approved leaves plan stuck | GREEN | Given synthesizing plan with expired lease; When reclaimer runs; Then plan re-synthesized or parked failed within attempt bound. Given reclaim-to-approved; When reclaimer runs; Then Synthesize re-invoked |
| T2 | Phase writer tests | Test: same dir | `-run 'TestPhaseFailedWriter|TestPhaseConfirmedWriter'` | no writers exist | GREEN: failed-step threshold -> PhaseFailed; plan confirm cascades PhaseConfirmed | - |
| T3 | CAS tests | Test: same dir | `-run 'TestUpdatePlanStateWhere|TestManagerTransitionsUseCAS'` | primitives undefined | GREEN: from-state mismatch -> (false, nil); cancel-vs-confirm race resolves single-winner | - |
| T4 | Goal tests | Test: internal/employee/goal_fence_gate_test.go | `go test -p 2 ./internal/employee/ -run 'TestGoalTransitionFence|TestRetireSetsRetiredAt|TestPausedGoalSkipsAssessmentWrites'` | fence/ErrGoalTransition undefined | GREEN: blocked->active via plain Update rejected; tool resume path allowed; retired always has retired_at; paused goal rows skip assessment writes | - |
| T5 | Hygiene tests | Test: respective packages | `-run 'TestRevisePlanAcceptsFailed|TestCancelCancelsPhases|TestMarkReadRequiresDelivered'` | behaviors absent | GREEN | - |

## Self-Verification Checklist

- [ ] All RED for implementation reasons; no prod changes

**DO NOT COMMIT.**

## Completion Report Format

Files; RED tails; deviations.

## Review Checklist (For Review Agent)

- [ ] Matrix coverage F1-F5 of branch orchestrator

## Notes

Plan store tests need the sqlite test fixture pattern used by store_sqlite_test.go.
