# Plan CAS Primitive - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Store-level CAS primitive UpdatePlanStateWhere (C-P1 / master C4) — the single guarded transition seam.
- **Dependencies:** 00T (for the test), no leaf deps
- **Estimated Context:** 30K
- **Concurrency Group:** B
- **Audit references:** M-7 (store half)

## Goal

One store method does conditional plan transitions; every manager
transition migrates onto it in leaf 04.

## Context

- internal/plan/store_sqlite.go:343 SetPlanState (unconditional), :361 UpdatePlanStateConditional (existing CAS pattern to mirror), :251 UpdatePlan (full-row)

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func (s *SQLiteStore) UpdatePlanStateWhere(ctx context.Context, id string, from []PlanState, to PlanState) (bool, error)
// UPDATE plans SET state=?, updated_at=? WHERE id=? AND state IN (...)
// false, nil when 0 rows (lost race / already moved). Error only on DB failure.
// Also add PlanStore interface method (internal/plan/store.go) so manager depends on the interface.
```

### What This Leaf Consumes

- UpdatePlanStateConditional's tx/pragma idiom (mirror it exactly).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Primitive | Modify: internal/plan/store_sqlite.go + store.go interface | gate TestUpdatePlanStateWhere GREEN | RED | GREEN | Given plan in pending_approval; When transition from=[pending_approval] to=approved; Then true. When from=[draft]; Then false, state unchanged |
| T2 | Interface + mocks parity | Verify: `go build ./... && go test -p 2 ./internal/plan/` | compile | - | all store implementations (sqlite + any test fakes) implement the method | - |

## Self-Verification Checklist

- [ ] Gate test GREEN, untouched
- [ ] Mirror UpdatePlanStateConditional's transaction idiom exactly

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Signature matches C-P1 exactly
- [ ] 0-rows = (false, nil), never an error

## Notes

Keep SetPlanState for CreatePlan and the reclaimer's internals only; leaf
04 removes manager uses. Do not remove it in this leaf.
