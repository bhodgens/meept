# Phase Failure/Confirm Writers - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Give PhaseFailed and PhaseConfirmed real writers (M-6 / plan F1); make failed phases block plan completion correctly.
- **Dependencies:** 00T-plan-goal-tests.md
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** M-6 (PhaseFailed/PhaseConfirmed zero writers), plan F1

## Goal

A phase that fails reaches PhaseFailed; the plan-level completion check
accounts for it; confirmed plans cascade PhaseConfirmed to their phases.

## Context

- internal/plan/plan.go:38-49 (PhaseState enum + IsTerminal)
- internal/plan/manager.go:888 (SetPhaseState in_progress), :958/:983 (completed via OnStepCompleted/OnTaskCompleted), :1008 (all-phases-complete check looks only for PhaseCompleted), :532-560 ConfirmPlan
- internal/plan/store_sqlite.go:602-630 (UpdatePhase/SetPhaseState/IncrementPhaseProgress)
- Q2 default: ADD writers (not delete enum values) - confirmed in master Open Questions.

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Behavioral:
// OnStepFailed / failed-step threshold path sets PhaseFailed (via SetPhaseState) when a phase's failure count makes it unrecoverable:
//   threshold: failed_steps >= total_steps  OR an explicit phase-fail signal from the executor (pick the minimal existing signal; do not invent new bus events).
// completePlan's completion check: all phases terminal (completed OR failed); any failed phase -> plan StateFailed instead of StateCompleted.
// ConfirmPlan cascades PhaseConfirmed to all terminal phases.
```

### What This Leaf Consumes

- Existing OnStepCompleted/OnTaskCompleted wiring.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Failure detection path | Modify: internal/plan/manager.go (step-failure handling near OnStepCompleted counterparts) | gate TestPhaseFailedWriter GREEN | RED | GREEN: phase with all steps failed -> PhaseFailed persisted | Given phase total=3, 3 failed steps; When step-failure recorded; Then state=failed |
| T2 | Completion check honesty | Modify: manager.go :1008 region + completePlan | `go test -p 2 ./internal/plan/ -run TestPhaseFailedWriter` | - | GREEN: plan with 1 failed phase completes as StateFailed not StateCompleted | Given phases [completed, failed]; When completion evaluated; Then plan failed |
| T3 | Confirm cascade | Modify: manager.go ConfirmPlan | gate TestPhaseConfirmedWriter GREEN | RED | GREEN: confirming a plan sets terminal phases to confirmed | Given completed plan with 2 completed phases; When ConfirmPlan; Then phases confirmed |
| T4 | Intake consumers unaffected | Verify: `go test -p 2 ./internal/intake/...` | - | - | intake projection still green (phase_state payload unchanged) | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] No new bus events; payload shapes unchanged
- [ ] IsTerminal now reachable for phases (grep-able writers exist)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Threshold uses existing failure accounting (IncrementPhaseProgress failed_steps) - no parallel counter
- [ ] Plan-failed outcome matches repo's errored-step semantics (never passes review silently)

## Notes

If the executor has NO per-step failure signal reaching the plan manager
today, stop and report: the leaf then implements only the completed-side
accounting + confirm cascade, and the failure half escalates back to the
orchestrator (missing upstream seam, out of scope to invent).
