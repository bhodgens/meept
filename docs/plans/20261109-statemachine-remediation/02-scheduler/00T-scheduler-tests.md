# Scheduler Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY, fail RED for missing-implementation reasons. Do NOT implement production code. Do NOT commit.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Gate tests for scheduler crash-window fix (M-5).
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** M-5 / scheduler F5

## Goal

Pin: lastWake advances only after delivery; recovery walks DeliveryHistory.

## Context

- internal/scheduler/scheduler.go - tick claim -> advanceLastWake -> Execute flow
- internal/scheduler/receipts.go / jobs.go - receipt states pending -> in_flight -> delivered

## Verified Anchors

Re-grep at dispatch: scheduler.go:651 (claim), :671-676 (advance), :707 (Execute); receipts.go:126-132 (LastDelivery-only recovery).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test functions (package scheduler, per existing test layout):
TestAdvanceLastWakeAfterDelivery
TestRecoverInFlightWalksHistory
```

### What This Leaf Consumes

Parent contract C-S1.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Advance ordering test | Test: internal/scheduler/crash_window_gate_test.go | `go test -p 2 ./internal/scheduler/ -run TestAdvanceLastWakeAfterDelivery` | current code advances before Execute -> assertion fails | GREEN | Given Execute that fails; When tick runs; Then lastWake unchanged (retryable next catch-up) |
| T2 | History recovery test | Test: same file | `go test -p 2 ./internal/scheduler/ -run TestRecoverInFlightWalksHistory` | recovery reads LastDelivery only -> stuck in_flight receipt not re-delivered | GREEN | Given receipt A stuck in_flight + later receipt B delivered; When recover; Then A re-delivered once, B untouched |

## Self-Verification Checklist

- [ ] RED for implementation reasons; no prod changes

**DO NOT COMMIT.**

## Completion Report Format

Files; RED output tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Matches orchestrator Test Gate F1/F2

## Notes

Use the package's existing fake-executable-job pattern if present (survey first).
