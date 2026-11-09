# Parked-Turn Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY, fail RED for missing-implementation reasons. Do NOT implement production code. Do NOT commit.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Gate tests for parked-turn branch (C1 CRITICAL, M-12, M-13, M-4-park, F5-F9).
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** C1, M-12, M-13, F4, F5, F6, F8, F9

## Goal

Pin: owner-scoped park rows, throttle resume delivery, gate release on
rejection, re-park bounds, budget persistence.

## Context

- internal/agent/parked_turn.go, park_store_sqlite.go, gate_park.go, loop_park.go, quota_resume.go, budget_resume.go
- Survey test helpers in internal/agent (parker tests exist for C2 fix).

## Verified Anchors

Re-grep at dispatch: park_store_sqlite.go:205-209 (kind-scoped Load), quota_resume.go:310-317, components.go:4286-4318 (two parkers, one kind), gate_park.go:55 (+10y), :168-192 (re-raise append), loop_park.go:418-477 (resume, no delivery), handler.go:2720-2746 (quota resume delivery = parity target).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test functions (internal/agent):
TestLoadReturnsOnlyOwnedRows
TestRestartRearmSingleOwner (integration-flavored: two parkers, one store)
TestThrottleResumeDeliversResult
TestRejectReleasesGatePark
TestQuotaReparkAttemptCap
TestBudgetParkPersists
TestBudgetParkEmitsEvent
TestThrottleStashPerTurn
TestReRaiseReplacesNotAppends
TestParkedDetectionWithoutStateMachine
```

### What This Leaf Consumes

Parent contracts C-K1..C-K3 (compile targets).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Owner scoping tests | Test: internal/agent/park_owner_gate_test.go | `go test -p 2 ./internal/agent/ -run 'TestLoadReturnsOnlyOwnedRows|TestRestartRearmSingleOwner'` | owner column/filter absent -> both parkers load both rows | GREEN | Given throttle row + quota row in store; When each parker loads kind=chat; Then each sees ONLY its own |
| T2 | Delivery/bounds/gate tests | Test: same + adjacent files | `-run 'TestThrottleResumeDeliversResult|TestRejectReleasesGatePark|TestQuotaReparkAttemptCap'` | resume discards result; no ReleaseByKey; no attempt cap | GREEN | Given parked throttle turn; When resumed; Then exchange persisted + client pushed + terminal emitted. Given gate park + plan rejection; When reject; Then park released. Given N quota re-parks; When cap exceeded; Then give-up terminal |
| T3 | Budget + minors tests | Test: same | `-run 'TestBudgetPark|TestThrottleStashPerTurn|TestReRaiseReplaces|TestParkedDetectionWithoutStateMachine'` | budget memory-only; stash shared; re-raise appends | GREEN | - |

## Self-Verification Checklist

- [ ] RED for implementation reasons; no prod changes

**DO NOT COMMIT.**

## Completion Report Format

Files; RED tails; deviations.

## Review Checklist (For Review Agent)

- [ ] C1-killing test present (T1 restart shape)
- [ ] Matrix F1-F4 covered

## Notes

Reuse the existing parker test rig (C2 audit left restart tests); extend, don't fork.
