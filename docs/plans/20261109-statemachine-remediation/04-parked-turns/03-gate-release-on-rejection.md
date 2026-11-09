# Gate Release on Rejection - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** M-12 + F9: gate parks release on plan rejection/cancel; re-raise replaces instead of appending; release emits an observable event.
- **Dependencies:** 01-parker-identity.md
- **Estimated Context:** 40K
- **Concurrency Group:** B
- **Audit references:** M-12, F9 (duplicate record), C-K3

## Goal

No gate row outlives its plan's terminal decision; PendingGates is not a
leak; duplicate approvals cannot double-resume.

## Context

- internal/agent/gate_park.go:55 (+10y ResumeAt), :168-192 (ParkGate re-raise APPENDS record; ResumeByKey pops one), :208-267 (resumeByKey delete-before-callback)
- internal/agent/gate_park_wiring.go:116-131 (plan.approved -> ResumeByKey), :151-190 (plan.submitting -> ParkGate); NO rejection listener
- internal/plan/manager.go:445/566 (CancelPlan/RejectPlan sites — wire release via bus, not direct import: plan pkg must not import agent)

## Verified Anchors

Re-grep at dispatch. Wiring direction: gate_park_wiring SUBSCRIBES to plan
rejection/cancel bus topics (check what plan pkg publishes on reject/cancel;
if no event exists, subscribe to the state-topic that exists — plan.state
events — and match on terminal states).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C-K3:
func (g *GateParker) ReleaseByKey(gateID, reason string) (bool, error)
// deletes park row + memory records (ALL copies under the key), emits the resume-event with reason="rejected"|"cancelled"; returns false when nothing parked.
// ParkGate re-raise: REPLACES the queued record (at-most-one per key) instead of appending.
```

### What This Leaf Consumes

- C-K1 owner semantics ("gate" owner).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | ReleaseByKey | Modify: gate_park.go | gate TestRejectReleasesGatePark GREEN | absent | GREEN | Given gate park for plan P; When ReleaseByKey(P, "rejected"); Then row deleted, memory cleared, event emitted; second call returns false |
| T2 | Rejection/cancel subscription | Modify: gate_park_wiring.go | same test (wiring shape) + `go build ./...` | no listener | GREEN: plan rejected/cancelled -> release; re-drive-safety: unknown gate IDs no-op | - |
| T3 | Re-raise replaces | Modify: gate_park.go :168-192 | gate TestReRaiseReplacesNotAppends GREEN | appends | GREEN | Given existing park for key K; When ParkGate(K) again; Then one record remains |
| T4 | Observability | Verify event emission consumed by existing quota_wait topic consumers | `go test -p 2 ./internal/tui/` (compile-level) | - | TUI/GUI compile; no payload reshape | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] plan pkg NOT importing agent (direction check: wiring subscribes)
- [ ] Approval double-resume now impossible (T3 proof)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Release covers BOTH reject and cancel paths (and any other terminal: failed synthesis)
- [ ] No new bus topics (reuse existing resume-event)

## Notes

If plan pkg publishes no terminal-state event the wiring can match, escalate
to orchestrator BEFORE adding a new topic — leaf 03-plans-goals/04's CAS
migration may add one; coordinate via the master contract addendum, not a
local invention.
