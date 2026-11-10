# Queue-Backed Park Storage - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED (internal/agent/park_queue_gate_test.go — frozen). Make them GREEN. Do NOT commit. No read_file on existing sources. Write once. gofmt+vet.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/agent: new park_backend_queue.go (+ small TurnParker/config plumbing); internal/config: parking.queue_backend key; internal/daemon/components.go: backend selection at wiring.
- **Dependencies:** 01-backend-seam.md (interface must exist); 00T gate
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Audit references:** C1, M-4-park (give-up semantics)

## Goal

queueParkBackend implements parkBackend over internal/queue.Store: parked
turns become type `parked_turn` jobs; owner = claimed_by naming space
(park:quota-watcher etc.); wake = next_retry_at; give-up routes to the
TurnParker giveUp callback, NEVER dead_letter.

## Context

- C-A2 shape in master; queue store: AddJob/next_retry_at claim filter (verify claim honors it; if not, the backend uses a due-row poll over Store.List + Requeue — pick per T1 survey and note)
- drainDue/delete-before-callback stays in TurnParker unchanged; the backend only replaces storage+re-arm

## Verified Anchors

Re-grep at dispatch: queue store.go Add signature; claim next_retry_at predicate (grep '<' near next_retry_at in claim SQL); park_store_sqlite.go ParkOwner consts.

## Interface Contracts (From Parent)

### What This Leaf Exposes
```
type queueParkBackend struct{ q *queue.Store; owner string; kind ParkKind }
func newQueueParkBackend(...) parkBackend   // Arm(): per-owner due-job claim loop -> resumeFunc; exhausted-attempt -> giveUpFunc + job delete
TurnParker.SetBackend wiring: components.go selects by cfg.Parking.QueueBackend; false = nil (default sqlite) — byte-identical when off
```

### What This Leaf Consumes
- C-A1 interface (leaf 01); queue.Store public API only (NO queue package edits — if claim ignores next_retry_at, backend-side due poll within ParkKind jobs instead; document).

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS | Given/When/Then |
|---------|-----------|-------|--------|------------|------|-----------------|
| T1 | Claim/due survey | read-only grep | report | - | decision recorded | Given claim SQL; When next_retry_at honored; Then claim path else due-poll |
| T2 | queueParkBackend | Create: internal/agent/park_backend_queue.go | gates TestQueueBackendSaveLoadDelete GREEN | undefined | GREEN | - |
| T3 | Give-up routing | same | TestQueueBackendGiveUpRoutesToCallback GREEN | undefined | GREEN; assert zero dead_letter rows | - |
| T4 | Config key | Modify: internal/config (+ template json5 if pattern exists) | TestParkingQueueBackendDefaultFalse GREEN | key missing | default false | - |
| T5 | Backend selection wiring | Modify: internal/daemon/components.go (park store init site) | build + daemon park tests | - | flag=false unchanged path | - |

## Self-Verification Checklist

- [ ] All four queue gates GREEN
- [ ] Full internal/agent suite: existing 12 park gates + pre-existing green
- [ ] go build ./... green; -race clean

**DO NOT COMMIT.**

## Completion Report Format

Files; T1 decision; scoreboard; deviations.

## Review Checklist (For Review Agent)

- [ ] Byte-identical when flag off (no new goroutine started)
- [ ] No queue package edits; no dead-letter leakage; no new bus topics (queue.job.* existing events fine — flag if topology changes)

## Notes

payload = full ParkedTurnRecord JSON; job priority default; interactive flag
NOT inherited (parking is not claim-ordered like user jobs) — note in code.
