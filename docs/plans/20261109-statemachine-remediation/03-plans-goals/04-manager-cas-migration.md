# Plan State Transition Table + Manager Migration - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** M-7 manager half, as a TABLE-DRIVEN transition design (design amendment 2026-11-09): one declared from->to table drives all plan transitions through one guarded entry point. Replaces the original "migrate 6 call sites individually" shape.
- **Dependencies:** 03-plan-cas.md (UpdatePlanStateWhere must exist)
- **Estimated Context:** 55K
- **Concurrency Group:** B
- **Audit references:** M-7, plan F2

## Goal

An illegal plan transition becomes unrepresentable instead of unguarded:
every transition is one table row; every state change flows through one
function that reads the table and calls UpdatePlanStateWhere.

## Context

- internal/plan/manager.go transition sites (re-derive from-state sets from current guards at dispatch):
  - Submit: [draft] -> pending_approval
  - Approve: [pending_approval] -> approved (already CAS - its guard folds into the table)
  - Cancel: any non-terminal -> cancelled
  - Reject: [pending_approval] -> cancelled
  - Revise: [pending_approval, cancelled, failed] -> planning (failed added per audit F7 - leaf 06 T1 depends on this row; add it here, leaf 06 only tests it)
  - Confirm: [completed, failed, cancelled] -> confirmed
  - complete: [executing] -> completed
- internal/plan/store_sqlite.go:343 SetPlanState (unconditional - retain for CreatePlan and reclaimer internals only), :251 UpdatePlan (full-row; lease-bearing rows must move to targeted updates)

## Verified Anchors

Re-grep at dispatch; the from-state sets above must be re-derived from
current code before writing. If current guards disagree with the audit,
prefer the code's intent and note the delta in the report.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Transition table (package-level, plan.go or new transitions.go):
var planTransitions = map[PlanState][]PlanState{
    StatePlanning:        {},                                    // terminal entry (created here)
    StateDraft:           {StatePendingApproval, StatePlanning, StateCancelled},
    StatePendingApproval: {StateApproved, StateCancelled, StatePlanning},
    StateApproved:        {StateSynthesizing, StateCancelled},
    StateSynthesizing:    {StateExecuting, StateDraft, StateFailed, StateApproved},
    StateExecuting:       {StateCompleted, StateCancelled, StateFailed},
    StateCompleted:       {StateConfirmed},
    StateFailed:          {StateConfirmed, StatePlanning},
    StateCancelled:       {StateConfirmed, StatePlanning},
    StateConfirmed:       {},                                    // terminal
}
// Single guarded entry point:
func (m *PlanManager) transition(ctx context.Context, planID string, to PlanState) error
//   reads plan state once, checks table membership, calls UpdatePlanStateWhere(from=current, to)
//   lost race / non-member -> existing typed not-modifiable error shape
// Manager transition methods (Submit/Cancel/Reject/Revise/Confirm/complete/Approve) become thin wrappers over transition().
```

### What This Leaf Consumes

- C-P1 UpdatePlanStateWhere (leaf 03).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Transition table + guard | Create: internal/plan/transitions.go; Modify: plan.go (expose table for tests) | gate TestManagerTransitionsUseCAS GREEN | hand-rolled guards, no table | GREEN: table membership drives every transition | Given plan in draft; When transition(to=executing); Then refused (not in table). When transition(to=pending_approval); Then succeeds |
| T2 | Manager methods -> wrappers | Modify: internal/plan/manager.go (6 sites + fold Approve's existing CAS guard) | gate test GREEN | sites use read-check-write | GREEN; concurrent cancel+confirm on one completed plan -> exactly one wins | - |
| T3 | Table-driven test | Test: internal/plan/transitions_test.go | `go test -p 2 ./internal/plan/ -run TestPlanTransitionTable` (new) | new | GREEN: every enum state has a table row; terminal states have empty to-sets; every audited transition is a member (machine == table) | - |
| T4 | Lease-bearing rows: targeted updates | Modify: manager.go full-row UpdatePlan uses on synthesizing/executing rows | `go test -p 2 ./internal/plan/` | - | GREEN: ConfirmPlan cannot clobber lease/task_id columns | Given plan with live lease; When ConfirmPlan; Then lease/task_id columns intact |
| T5 | Caller error-shape parity | Verify: `go test -p 2 ./internal/plan/... ./internal/intake/... ./internal/agent/...` | - | - | callers' existing error handling unchanged | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] grep SetPlanState in manager.go reduced to CreatePlan + reclaimer internals (documented)
- [ ] Table and audited state machine match exactly (T3 is that proof)
- [ ] All plan package tests green

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations; any from-set deltas vs audit.

## Review Checklist (For Review Agent)

- [ ] Table is the single source: no transition path bypasses transition()
- [ ] Terminal states (confirmed) have empty to-sets
- [ ] T4 proves no lease clobber

## Notes

DESIGN AMEND 2026-11-09 (user-approved direction): this leaf originally
migrated call sites one-by-one; it now lands the table directly. Leaf 06
T1 (revise-accepts-failed) becomes a test-only change - the table row here
implements it. Do NOT migrate goal transitions in this leaf (goal fence is
leaf 05's store-level design; a goal transition table is a candidate
follow-up, note it in the report if the shape generalizes cleanly).
