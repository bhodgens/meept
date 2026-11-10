# Parked-Queue Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY, fail RED for missing-implementation reasons (undefined target symbols = accepted compile-RED). No production changes, no commits. No read_file on existing sources; grep/cat. Survey existing park test rigs (park_owner_gate_test.go rig helpers, newTestParkStore, scriptedToggleChatter) and reuse.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/agent test files only.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Pin C-A1/C-A2 target shapes: parkBackend seam, queue-backed park round-trip, give-up routing, flag default.

## Verified Anchors

- internal/agent/park_store_sqlite.go ParkOwner* consts; internal/queue/store.go AddJob/ClaimNextForAgent next_retry_at filter (grep); config pattern: grep skills.state default-false in internal/config.

## Interface Contracts (From Parent)

### What This Leaf Exposes
```
internal/agent/park_queue_gate_test.go:
TestParkBackendSeamWrapped         // TurnParker.SetBackend(fakeBackend) routes Save/Delete/LoadOwned (compile-RED: SetBackend/parkBackend undefined)
TestQueueBackendSaveLoadDelete     // queueParkBackend over a real queue.Store: Park saves job type "parked_turn" w/ next_retry_at==ResumeAt, claimed_by==owner; LoadOwned returns owner rows only (compile-RED)
TestQueueBackendGiveUpRoutesToCallback // attempt>=max on due job -> give-up callback, NO dead_letter row (compile-RED)
TestParkingQueueBackendDefaultFalse // config key parking.queue_backend parses default false (behavior-RED: key undefined today)
```

## Tasks

| Task ID | Objective | Files | Verify | RED reason |
|---------|-----------|-------|--------|------------|
| T1 | four gate tests above | Test: internal/agent/park_queue_gate_test.go | `go test -p 2 -count=1 ./internal/agent/ -run 'ParkBackend|QueueBackend|ParkingQueueBackend'` | undefined symbols / missing config key |

## Self-Verification Checklist

- [ ] RED for implementation reasons; pre-existing suites still green apart from these
- [ ] No prod file touched

**DO NOT COMMIT.**

## Completion Report Format

Files; RED tails; rig notes; deviations.

## Review Checklist (For Review Agent)

- [ ] Matrix F1-F3 coverage vs master Test Gate table

## Notes

Queue-backend test uses in-memory queue store helper (survey existing
queue test rig via the package's import surface — internal/agent tests
may need a local fake implementing queue.Add/Claim minimal subset; keep
it in the gate file).
