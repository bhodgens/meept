# Crash-Safe Scheduler Delivery - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Move advanceLastWake after delivery; recovery walks full DeliveryHistory (M-5, Q4 default shape).
- **Dependencies:** 00T-scheduler-tests.md
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **Audit references:** M-5

## Goal

A crash anywhere between claim and delivery leaves the occurrence
recoverable; recovery re-delivers every pending/in_flight receipt.

## Context

- internal/scheduler/scheduler.go:651-707 - claim/advance/Execute sequence
- internal/scheduler/receipts.go:126-132 - recoverInFlightDeliveries reads cfg.LastDelivery only
- DeliveryHistory - per-job receipt list (jobs.json store)

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func (r *ReceiptStore) RecoverInFlight(ctx context.Context) (int, error)
// walks DeliveryHistory: every receipt pending|in_flight re-delivered once (MessageID dedup inside recovery run)
```

### What This Leaf Consumes

- C-S1 parent contract.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Reorder advance-after-delivery | Modify: internal/scheduler/scheduler.go (671-676 region) | gate TestAdvanceLastWakeAfterDelivery GREEN | RED | GREEN; Q4 default: advance runs after Execute+receipt>=in_flight; shell/reminder jobs unaffected in happy path | Given failing Execute; When tick; Then lastWake not advanced |
| T2 | Duplicate-advance guard | Verify: full package | `go test -p 2 ./internal/scheduler/` | - | no double-fire for already-claimed ticks (claims PK still gates) | - |
| T3 | History-walking recovery | Modify: internal/scheduler/receipts.go; add RecoverInFlight | gate TestRecoverInFlightWalksHistory GREEN | RED | GREEN; ordered by attempt time; one delivery per stuck receipt | Given 2 stuck + 1 delivered receipts; When RecoverInFlight; Then 2 re-deliveries, delivered untouched |
| T4 | Wire recovery | Modify: startup/recovery call site (grep recoverInFlightDeliveries caller) | `go build ./...` | - | new method replaces LastDelivery-only scan; old path removed or delegated | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] No occurrence delivered twice in one recovery (dedup by MessageID)
- [ ] gofmt/vet clean

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Ordering change does not break multi-occurrence catch-up (lastWake semantics preserved for happy path)
- [ ] Recovery bounded and idempotent

## Notes

If Q4 was overridden by the user to the pre-execute receipt shape, T1
changes to "write pending receipt before advance" - goal identical, confirm
with orchestrator before deviating from the default.
