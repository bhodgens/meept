# Plan/Goal Lifecycle Remediation - Implementation Orchestrator

> **For the executing agent:** orchestrator for this branch. Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test-gate leaf + 6 implementation leaves
- **Scope:** Close plan lifecycle M-6..M-9 + F7..F13, goal M-10..M-11 + F16, message nit (MarkRead skip-delivered).

## Goal

Every plan state transition CAS-guarded or provably single-writer; a
periodic synthesis reclaimer with a real re-drive path; phases can fail;
goal blocked/retired semantics enforced by the store, not convention.

## Architecture

Three layers: (1) store primitive `UpdatePlanStateWhere` (C4 in master) as
the single CAS seam; (2) reclaimer loop owning synthesizing/approved
recovery (C3 in master); (3) goal store gaining transition validation
(validFrom map) so every writer inherits the fence. Q2 default: add
PhaseFailed/PhaseConfirmed writers. Q3 default: soft-delete retirement.

## Interface Contracts

### C-P1 (master C4): Conditional plan transition

```
func (s *SQLiteStore) UpdatePlanStateWhere(ctx context.Context, id string, from []PlanState, to PlanState) (bool, error)
// Owner: 03-plan-cas.md; Consumers: 04-manager-cas-migration.md
```

### C-P2 (master C3): Reclaimer

```
func (m *PlanManager) StartSynthesisReclaimer(ctx context.Context, interval time.Duration)
// Owner: 01-synthesis-reclaimer.md; Consumers: daemon.go wiring (new leaf task T5)
```

### C-P3: Goal transition validation

```
// validFrom map[GoalState][]GoalState in goal.go; GoalStore.Update rejects invalid transitions with ErrGoalTransition
var ErrGoalTransition = errors.New("employee: invalid goal state transition")
// Owner: 05-goal-transition-fence.md
```

### C-P4: Plan transition table (design amendment 2026-11-09)

```
var planTransitions = map[PlanState][]PlanState   // single source of legal transitions
func (m *PlanManager) transition(ctx, planID string, to PlanState) error
// Owner: 04-manager-cas-migration.md; Consumers: leaf 06 (revise-accepts-failed is a table row, not a separate change)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-plan-goal-tests.md | leaf (gate) | none | 35K | A |
| 01 | 01-synthesis-reclaimer.md | leaf | 00T | 55K | A |
| 02 | 02-phase-failure-writers.md | leaf | 00T | 40K | A |
| 03 | 03-plan-cas.md | leaf | none | 30K | B |
| 04 | 04-manager-cas-migration.md | leaf | 03 | 50K | B |
| 05 | 05-goal-transition-fence.md | leaf | none | 45K | C |
| 06 | 06-minor-hygiene.md | leaf | 03 | 35K | C |

## Dispatch Protocol

Phase 0 gate. Group A (2-cap): leaves 01+02; then B: 03, then 04; then C:
05+06. In-session review per leaf; orchestrator commits per leaf.

## Review Checklist

- [ ] No unconditional plan state write remains outside CreatePlan (grep SetPlanState at review)
- [ ] Reclaimer: lease-expired synthesizing -> re-drive; reclaim-to-approved -> Synthesize invoked; bounded by MaxSynthesisAttempts
- [ ] Reclaimer interval wired in daemon.go with nil-safe guard
- [ ] PhaseFailed writer: failed-step threshold OR explicit fail; PhaseConfirmed written on plan confirm cascade
- [ ] Goal fence: blocked->active ONLY via requireDirectHuman-gated path; UpdateGoal validates
- [ ] retired_at set on every retired-state write (IsRetired consistency)
- [ ] No gate-test modification

Output: APPROVED or gaps.

## Coding Conventions

Repo conventions; `-p 2`; cite finding IDs; do not alter intake projection
consumers of plan.executing/plan.phase_state payloads (only add, never
reshape, bus payloads).

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | M-8, M-9 (reclaim/re-drive) | 01 T1-T5 | TestSynthesisReclaimerRedrivesApproved |
| R2 | M-6 (PhaseFailed/Confirmed) | 02 T1-T3 | TestPhaseFailedWriter |
| R3 | M-7 (CAS transitions) | 03+04 (04 is TABLE-DRIVEN per amendment: C-P4, leaf 04 T3 proves table==machine) | TestUpdatePlanStateWhere, TestPlanTransitionTable, migration race test |
| R4 | M-10 (goal unblock seam) | 05 T1-T2 | TestGoalTransitionFence |
| R5 | M-11 (retire semantics) | 05 T3-T4 | TestRetireSetsRetiredAt |
| R6 | F7 (failed confirmable-not-revisable) | 06 T1 | TestRevisePlanAcceptsFailed |
| R7 | F8 (cancel leaves phases) | 06 T2 | TestCancelCancelsPhases |
| R8 | F9 (phase_completed no subscriber) | 06 T3 (remove or subscribe - decision note) | grep |
| R9 | F12 (mapping restore gap) | 06 T4 | TestRestoreMappingsCoversSynthesizing |
| R10 | F16 (paused-goal writes) | 05 T5 | TestPausedGoalSkipsAssessmentWrites |
| R11 | messages nit (queued->read) | 06 T5 | TestMarkReadRequiresDelivered |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-plan-goal-tests | TESTS_VALIDATED | 1 | committed a59fbc4f (--no-verify RED-by-design); 14 RED tests, parent-verified contract-symbol-only |
| 01-synthesis-reclaimer | COMPLETE | 1 | committed 7c626b69; reclaim+re-drive green both gates; T4: no external re-driver; plans.md invariant line |
| 02-phase-failure-writers | COMPLETE | 1 | committed a94995c8 + producer seam (tactical publisher + handler sub + 3 pins, graphs regenerated); M-6 live not dormant |
| 03-plan-cas | REVIEWED | 1 | committed d191cb34; gate hashes stable (8981080c/07573760); stub parity + empty-from-set guard parent-verified |
| 04-manager-cas-migration | COMPLETE | 1 | committed 3b04d3ef; table+observed-state CAS parent-verified; delta flagged: PruneStalePlans stale-planning no-op -> fold into 06 |
| 05-goal-transition-fence | REVIEWED | 1 | committed 091cc90b; parent-verified employee pkg green, gate hash stable; Retire dead-code kept (frozen gate calls it) |
| 06-minor-hygiene | DEFERRED | 0 | open items incl. stale-planning prune disposition — follow-on |

## Integration Test Plan

`go test -p 2 ./internal/plan/... ./internal/employee/... ./internal/intake/...`;
kill-and-recover test: claim synthesis, abandon goroutine, run reclaimer
once, assert plan reaches executing or failed within attempt bound.

## Test Gate

| # | Feature | Source | Unit tests | E2E | Status |
|---|---------|--------|-----------|-----|--------|
| F1 | Reclaimer re-drive | 01 | TestSynthesisReclaimer* | n/a | PENDING |
| F2 | Phase failure writers | 02 | TestPhaseFailedWriter | n/a | PENDING |
| F3 | Plan CAS | 03/04 | TestUpdatePlanStateWhere + migration tests | n/a | PENDING |
| F4 | Goal fence | 05 | TestGoalTransitionFence | n/a | PENDING |
| F5 | Hygiene set | 06 | per-task | n/a | PENDING |

## Open Questions

- Q2/Q3 defaults from master apply. F9 resolution (delete event vs add
  subscriber) is a code-owner call: default = remove the dead event + its
  publish site (bus topology regen required: make graphs).

## Frozen Documents

Leaves freeze at dispatch.

## Notes

File overlap warning: internal/employee files are DIRTY in the sibling
session. Branch runs in the isolation worktree off committed main; on merge,
re-run the employee package tests. F13 (single-writer ambiguity, in-memory
mappings) is accepted-as-designed with the 06 T4 restore-coverage widening;
do not redesign the mapping stores.
