# Dual-Backend Restart Parity - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Tests ONLY — port the C1 killer restart test to the queue backend and prove flag-off byte-identity. Do NOT commit. No read_file on existing sources. NEVER edit frozen gates.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/agent test files (new file only).
- **Dependencies:** 02-queue-backend.md
- **Estimated Context:** 45K
- **Concurrency Group:** B

## Goal

The tree-20261109 C1 guarantee holds on BOTH backends: after a simulated
restart, each parked job resumes exactly once by its owner; with
parking.queue_backend=false nothing new runs.

## Interface Contracts (From Parent)

### What This Leaf Exposes
```
TestRestartRearmSingleOwnerQueueBackend  // mirrors TestRestartRearmSingleOwner over queueParkBackend (quota + throttle rows, distinct owners, exactly-once per owner)
TestQueueBackendDisabledByteIdentical    // flag false: parker uses sqlite backend; no queue rows created for parks
```

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---------|-----------|-------|--------|------------|------|
| T1 | two tests | Test: internal/agent/park_queue_parity_test.go | `go test -p 2 -count=1 ./internal/agent/ -run 'RestartRearmSingleOwnerQueueBackend|QueueBackendDisabledByteIdentical'` | pre-leaf-02: undefined backend | GREEN post-02 |

## Self-Verification Checklist

- [ ] Reuses the existing restart-rig helpers from park_owner_gate_test.go (do not fork rigs)
- [ ] -race clean

**DO NOT COMMIT.**

## Review Checklist (For Review Agent)

- [ ] Parity assertions match the sqlite-side gate 1:1

## Notes

Run this leaf right AFTER 02 commits (same wave, serialized by its
symbols). If 02's T1 chose due-poll over claim, the restart test drives
the Arm loop with a clock knob — survey what 02 exposed.
