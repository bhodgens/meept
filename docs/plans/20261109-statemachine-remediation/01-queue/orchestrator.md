# Job Queue Remediation - Implementation Orchestrator

> **For the executing agent:** orchestrator for this branch. Dispatch, review, re-dispatch, track. Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 5 leaves + 1 test-gate leaf
- **Scope:** Close all internal/queue findings: M-1 (MarkProcessing token-less), M-2 (failed-row stranding), M-3 (dead-row stranding), M-4 (reset-subset drift), F6 (BEGIN IMMEDIATE drift), F7 (missing CHECK), F8 (legacy token-less Complete - decision-gated), F9 (operator Retry budget bypass).

## Goal

Make every job state write token/CAS-guarded, give every non-terminal state
a revisiting path, align requeue column resets, and pin the state enum in
the DB.

## Architecture

Smallest-seam fixes to the existing claim-token CAS design. No new tables.
The failed/dead sweeper extends the existing startup-reset pattern into a
periodic one. Keep legacy surfaces byte-compatible except where a finding
names them.

## Interface Contracts

### C-Q1: MarkProcessing gains a token predicate

```
func (s *Store) MarkProcessing(jobID, claimToken string) error
// UPDATE jobs SET state='processing' WHERE id=? AND claim_token=? AND state IN ('claimed','processing')
// Owner: 01-mark-processing-token.md; Consumers: internal/worker (worker.go:319 call site)
```

### C-Q2: Sweeper entry point

```
func (s *Store) SweepAbandonedTerminalStates(ctx context.Context, olderThan time.Duration) (int, error)
// failed rows with no retry path + dead rows stranded in jobs -> dead_letter (or explicit error counter)
// Owner: 03-stranded-sweeper.md; Consumers: internal/queue sweeper loop wiring + daemon
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-queue-tests.md | leaf (gate) | none | 30K | A |
| 01 | 01-mark-processing-token.md | leaf | 00T | 45K | A |
| 02 | 02-requeue-column-parity.md | leaf | none | 35K | A |
| 03 | 03-stranded-sweeper.md | leaf | 00T | 50K | B |
| 04 | 04-state-check-and-immediate.md | leaf | none | 30K | B |

## Dispatch Protocol

Phase 0: dispatch 00T, validate RED tests in-session against the matrix,
commit, TESTS_VALIDATED. Phase 1: group A parallel (2-cap), review each,
commit per leaf. Phase 2: group B. Phase 3: integration
(`go test -p 2 ./internal/queue/... ./internal/worker/...`), AGENTS.md
summary update if worker contract changed.

## Review Checklist

- [ ] Every `UPDATE jobs SET state` has a token or state predicate (grep at review)
- [ ] New sweeper is idempotent and bounded (single tx, WHERE guards)
- [ ] F6: comment says BEGIN IMMEDIATE and code uses BeginTx with immediate
- [ ] F7: migration adds CHECK + fresh-schema parity
- [ ] No gate-test modification

Output: APPROVED or gaps.

## Coding Conventions

Repo root AGENTS.md conventions apply; tests table-driven; `-p 2`; fix
comments cite audit finding IDs (audit 2026-11-09, tree 20261109-statemachine-remediation).

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | M-1 | 01 T1-T3 | gate test mark-processing-token |
| R2 | M-4 | 02 T1-T2 | gate test requeue-parity |
| R3 | M-2, M-3 | 03 T1-T4 | gate test stranded-sweeper |
| R4 | F6, F7 | 04 T1-T2 | gate test state-check |
| R5 | F8, F9 | 03 T5 / 02 T3 (decision-gated Q1) | gate test retry-budget |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-queue-tests | TESTS_VALIDATED | 1 | committed 6e73912b (--no-verify: RED-by-design bypasses U1000/compile gates per tests-first protocol) |
| 01-mark-processing-token | IN_PROGRESS | 0 | dispatched 2026-11-09 (serialized: all queue leaves touch store.go) |
| 02-requeue-column-parity | PENDING | 0 | first dispatch STOPPED (store.go collision with leaf 01); re-dispatch after 01 commits |
| 03-stranded-sweeper | PENDING | 0 | blocked on 00T |
| 04-state-check-and-immediate | PENDING | 0 | |

## Integration Test Plan

`go build ./...` then `go test -p 2 ./internal/queue/... ./internal/worker/... ./internal/daemon/...` (daemon for startup-reset interplay).
Dead-letter round trip: fail a job to exhaustion, moveToDead, RecoverFromDeadLetter, complete - all in one store test.

## Test Gate

Test leaf: 00T-queue-tests.md.

| # | Feature | Source | Unit tests | E2E | Status |
|---|---------|--------|-----------|-----|--------|
| F1 | MarkProcessing token CAS | C-Q1 | TestMarkProcessingRequiresToken | n/a | VALIDATED |
| F2 | Requeue column parity | 02 | TestRequeueClearsClusterColumns | n/a | VALIDATED |
| F3 | Sweeper revisits failed/dead | C-Q2 | TestSweepAbandonedTerminalStates | n/a | VALIDATED |
| F4 | CHECK + IMMEDIATE | 04 | TestJobsStateCheckRejectsGarbage (plus legacy-migration + moveToDead-duplicate tests) | n/a | VALIDATED |
| F5 | Operator retry budget | 02 T3 | TestRetryOperatorRespectsMaxRetries | n/a | VALIDATED |

## Frozen Documents

Leaves freeze at dispatch; corrections as dated addenda.

## Notes

Anchor drift risk: the audit's store.go line numbers were from 63a7cf90.
Re-grep anchors (symbol-based) before dispatch. The sibling session's dirty
tree does not touch internal/queue (verified 2026-11-09), so this branch can
run in the isolation worktree off current main.
