# Scheduler Delivery Remediation - Implementation Orchestrator

> **For the executing agent:** orchestrator for this branch. Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 test-gate leaf + 1 implementation leaf
- **Scope:** Close M-5 (audit scheduler F5): crash between tick-claim and delivery loses occurrences; receipt recovery reads only LastDelivery.

## Goal

Make scheduled-occurrence delivery crash-safe: no tick claimed whose
delivery can be silently lost, and receipt recovery re-delivers every
in-flight receipt, not only the newest.

## Architecture

Two-part fix in internal/scheduler: (1) move `advanceLastWake` to AFTER
successful Execute (or gate it on the delivery receipt reaching at least
in_flight), so a crash before execution leaves the tick catchable by the
post-crash catch-up scan; (2) fix `recoverInFlightDeliveries` to walk
`DeliveryHistory` for ALL pending/in_flight receipts, not `cfg.LastDelivery`
only. Q4 default (move-after-delivery) applies unless overridden in
dispatch context.

## Interface Contracts

### C-S1: Recovery walks full history

```
// Fixed in place: recoverInFlightDeliveries walks DeliveryHistory for ALL
// pending|in_flight receipts (ordered by attempt time, MessageID dedup),
// not cfg.LastDelivery only. Gate test drives this existing entry point;
// keep its signature.
// Delta 2026-11-09: plan named ReceiptStore.RecoverInFlight(ctx); test
// targets the real entry point. Test wins; this contract restated.
// Owner: 01-crash-window.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-scheduler-tests.md | leaf (gate) | none | 25K | A |
| 01 | 01-crash-window.md | leaf | 00T | 50K | A |

## Dispatch Protocol

Phase 0 gate, then single implementation leaf, in-session review, commit.

## Review Checklist

- [ ] advanceLastWake ordering verified by a test that kills the flow between claim and advance
- [ ] Recovery test with TWO interleaved receipts (first stuck in_flight, second delivered) re-delivers the first
- [ ] No receipt dedup regression: same MessageID not delivered twice in one recovery
- [ ] No gate-test modification

Output: APPROVED or gaps.

## Coding Conventions

Repo conventions; `-p 2`; audit-finding citations in fix comments.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | M-5 part 1 (advance-before-execute) | 01 T1-T2 | TestAdvanceLastWakeAfterDelivery |
| R2 | M-5 part 2 (LastDelivery-only recovery) | 01 T3-T4 | TestRecoverInFlightWalksHistory |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-scheduler-tests | TESTS_VALIDATED | 1 | committed 6e73912b; C-S1 restated to real entry point (delta log) |
| 01-crash-window | PENDING | 0 | queued: 2-leaf cap busy with queue wave |

## Integration Test Plan

`go test -p 2 ./internal/scheduler/...`; a full-cycle test: claim -> crash
simulated via injected error -> recovery -> exactly one delivery per
occurrence.

## Test Gate

| # | Feature | Source | Unit tests | E2E | Status |
|---|---------|--------|-----------|-----|--------|
| F1 | advance ordering | 01 T1 | TestAdvanceLastWakeAfterDelivery | n/a | VALIDATED |
| F2 | history-walking recovery | 01 T3 | TestRecoverInFlightWalksHistory | n/a | VALIDATED |

Delta log: 2026-11-09, F1, plan->tests, gate adds an extra sub-case
"successful_execute_advances_watermark" pinning happy-path semantics; kept
(plan silent on it — lesser artifact updated to reflect greater). 2026-11-09,
F2, plan->tests, C-S1 restated to the real entry point recoverInFlightDeliveries
(see contract delta).

## Open Questions

- Q4 default applies: move advanceLastWake after Execute. If the user picks
  the pre-execute receipt shape instead, 01 T1/T2 change shape but not goal.

## Frozen Documents

Leaves freeze at dispatch.

## Notes

Anchors from 63a7cf90: scheduler.go:651 (claim), :671-676 (advance), :707
(Execute); receipts.go:126-132 (recovery reads LastDelivery only).
Re-grep by symbol at dispatch time.
