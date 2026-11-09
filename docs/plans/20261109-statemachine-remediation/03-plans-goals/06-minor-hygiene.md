# Plan/Goal Minor Hygiene - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** F7 (failed plans revisable), F8 (cancel cascades phases), F9 (dead phase_completed event), F11-adjacent (mapping restore gap), messages nit (queued->read), F6-adjacent (state CHECK constraints).
- **Dependencies:** 03-plan-cas.md (touches same store file; serialize after)
- **Estimated Context:** 35K
- **Concurrency Group:** C
- **Audit references:** F7, F8, F9, F11, F13 (accepted-as-designed part), messages F14 nit, F6

## Goal

Small findings closed in one sweep; no redesign of the mapping stores.

## Context

- internal/plan/manager.go:566 RejectPlan / :489 RevisePlan (from-sets), :440-450 CancelPlan (plan-row only), :987 plan.phase_completed publish, :1168-1180 RestoreMappings (executing-only, limit 100)
- internal/employee/messaging.go:255 MarkRead guard (allows queued->read)
- internal/plan/store_sqlite.go:58 (plans.state), employee goal/message tables - no CHECKs

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// RevisePlan from-set gains StateFailed (failed synthesis-parked plans are revisable).
// CancelPlan cascades: phases in_progress/pending -> cancelled-equivalent state handling per Q2 writer leaf's enum additions (use cancelled plan + phase rollback to 'pending'; live child tasks cancelled via existing task-cancel path).
// plan.phase_completed event REMOVED (zero subscribers; bus topology regen required).
// RestoreMappings covers synthesizing + executing.
// MarkRead requires state='delivered'.
// CHECK constraints added: plans.state, plan_phases.state, agent_messages.state (employee_goals covered by fence leaf's package).
```

### What This Leaf Consumes

- C-P1 for any state writes it adds.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Revise accepts failed (test-only: table row lands in leaf 04) | Modify: internal/plan/manager.go ONLY if leaf 04 has not landed; otherwise test asserts the leaf-04 table behavior | gate TestRevisePlanAcceptsFailed GREEN | RED | GREEN; if leaf 04 landed: NO manager edit, test asserts table row failed->planning | Given failed plan; When RevisePlan; Then state=planning |
| T2 | Cancel cascades phases | Modify: manager.go CancelPlan | gate TestCancelCancelsPhases GREEN | RED | GREEN; live child tasks receive the existing cancellation | Given executing plan with in_progress phase + task; When CancelPlan; Then phases reset, task cancelled |
| T3 | Remove dead event | Modify: manager.go :987 + topology regen (`make graphs`) | `grep -rn plan.phase_completed internal/` empty + `make graphs-check` | still published | removed; bus-topology regenerated | - |
| T4 | RestoreMappings widen | Modify: manager.go :1168-1180 | gate test in 00T T5 set (TestRestoreMappingsCoversSynthesizing if present; else package test) | RED | GREEN: synthesizing plans' child mappings restored | - |
| T5 | MarkRead monotone | Modify: internal/employee/messaging.go :255 | gate TestMarkReadRequiresDelivered GREEN | RED | GREEN: queued->read rejected | - |
| T6 | CHECK constraints | Modify: plan + employee store schemas (mirror effects_ledger pattern) | package tests + fresh-DB garbage INSERT rejects | RED | GREEN | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] `make graphs` + `make graphs-check` pass after T3
- [ ] No consumer of removed event exists (grep before removing)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] F13 mapping redesign NOT attempted (scope discipline)
- [ ] Cancel cascade uses existing task-cancel machinery, not new events

## Notes

T2 depends on leaf 02's phase enum additions landing first (serialized via
orchestrator group ordering); if PhaseFailed exists, cancelled phases go to
'pending' (reset semantics), not 'failed'.
