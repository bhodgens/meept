# Stranded-State Sweeper - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Periodic sweeper that revisits non-terminal failed rows and dead-in-table rows (M-2, M-3); moveToDead failure made recoverable.
- **Dependencies:** 00T-queue-tests.md
- **Estimated Context:** 50K
- **Concurrency Group:** B
- **Audit references:** M-2, M-3, audit 2026-11-09

## Goal

No row rests in failed (budget exhausted) or dead-in-jobs without a
machinery-driven revisit. moveToDead failures self-heal on the next sweep.

## Context

- internal/queue/store.go:851 FailAttempt (writes failed/dead), :907-911 moveToDead (log-only failure), :1431 moveToDead body (INSERT dead_letter + DELETE jobs)
- internal/queue/queue.go - existing sweeper/ticker patterns; daemon.go:2489 startup reset pattern as wiring reference

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func (s *Store) SweepAbandonedTerminalStates(ctx context.Context, olderThan time.Duration) (int, error)
// Contract C-Q2:
// - failed rows, updated_at < cutoff, retry_count >= max_retries -> moveToDead (idempotent; tolerate duplicate dead_letter by DELETE-ing the jobs row)
// - dead rows stranded in jobs (any age, updated_at < cutoff) -> moveToDead completion (INSERT-or-ignore + DELETE)
// - failed rows with budget remaining are NOT swept (Retry/worker path owns them)
// Returns count moved.
func (s *Store) StartSweeper(ctx context.Context, interval, olderThan time.Duration) // optional ticker wrapper; wiring in daemon
```

### What This Leaf Consumes

- Existing moveToDead; refactor its body so INSERT and DELETE compose under ONE transaction (BEGIN IMMEDIATE) and a duplicate dead_letter id downgrades to "ensure deleted".

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | moveToDead transactional + duplicate-safe | Modify: internal/queue/store.go (:1431 region) | `go test -p 2 ./internal/queue/ -run TestMoveToDeadDuplicateSafe` (new) | new test | GREEN: simulate duplicate dead_letter row -> jobs row still removed, no error surfaced | Given dead_letter already contains id X; When moveToDead(X); Then jobs row gone, nil error |
| T2 | Sweeper method | Modify: internal/queue/store.go; Test: gate test T3 | `go test -p 2 ./internal/queue/ -run TestSweepAbandonedTerminalStates` | undefined | GREEN per contract | Given the 3-row fixture; When sweep; Then dead_letter grew by exactly the 2 exhausted/stranded rows |
| T3 | Sweeper wiring | Modify: internal/daemon/daemon.go (near :2489 startup reset) | `go build ./...` | - | daemon starts sweeper ticker (config-gated off by default? NO - on by default at 15m interval; olderThan 1h) | Given daemon boot; When ticker fires; Then sweep runs, count logged at debug |
| T4 | AGENTS.md + config note | Modify: internal/queue/AGENTS.md (if exists) or root AGENTS.md summary line | grep | - | sweeper documented | - |

## Self-Verification Checklist

- [ ] Gate test GREEN, untouched
- [ ] Sweep idempotent: run twice, second run moves 0
- [ ] Bounded: single tx per row batch, WHERE-guarded

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Contract C-Q2 semantics exact
- [ ] moveToDead duplicate path proven by test
- [ ] Daemon wiring nil-safe + config-documentable

## Notes

Sweeper interval constant belongs beside the existing retry/queue config in
internal/queue; expose in config only if the repo already has a queue config
section - otherwise package constants (config surface creep is scope creep).
