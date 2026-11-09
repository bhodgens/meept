# Queue Gate Tests - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY. They must fail RED for missing-implementation reasons (undefined functions / wrong behavior). Do NOT implement production code. Do NOT commit. Do NOT use read_file on existing sources - explore with search_files/terminal. Write once, do not read back.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Gate tests for all 01-queue features, committed before implementation dispatches.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** M-1, M-2, M-3, M-4, F6, F7, F8, F9 (audit 2026-11-09)

## Goal

Pin the target behavior of every queue fix as failing tests: token-required MarkProcessing, requeue column parity, stranded-state sweeper, state CHECK, retry budget, operator retry bound.

## Context

- internal/queue/store.go - job state machine store; claim-token CAS pattern already present at Claim/Complete/Fail
- internal/queue/job.go - JobState enum
- Tests use an in-memory/temp sqlite store via the existing test helpers in the package (survey with search_files before writing).

## Verified Anchors

Re-grep at dispatch time (63a7cf90 origin):

- internal/queue/store.go:538-556 - UpdateState - terminal-only guard, no token
- internal/queue/store.go:954, 1019, 1074, 1158 - requeue paths - column-reset sets differ
- internal/queue/store.go:1462-1528 - RecoverFromDeadLetter - no interactive restore
- internal/queue/store.go:97 - jobs schema - no CHECK on state

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test functions in internal/queue/store_gate_test.go (package queue_test or queue per existing convention):
TestMarkProcessingRequiresToken
TestRequeueClearsClusterColumns
TestRetryClearsClusterColumns
TestDeadLetterRecoveryPreservesInteractive
TestSweepAbandonedTerminalStates
TestRetryOperatorRespectsMaxRetries
TestJobsStateCheckRejectsGarbage
```

### What This Leaf Consumes

None (tests only; consumes parent contract C-Q1/C-Q2 shapes as compile targets).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Token-required MarkProcessing test | Test: internal/queue/store_gate_test.go | `go test -p 2 ./internal/queue/ -run TestMarkProcessingRequiresToken` | MarkProcessing has 1-arg signature / accepts stale token | compiles; wrong-token call leaves state claimed; right token sets processing | Given claimed job with token T; When MarkProcessing(id, other-token); Then ErrJobStateLocked or no-op, state still claimed |
| T2 | Requeue parity tests | Test: same file | `go test -p 2 ./internal/queue/ -run 'TestRequeueClearsClusterColumns|TestRetryClearsClusterColumns'` | requeue leaves timeout_at/claimed_by_node set | all requeue paths clear timeout_at, last_heartbeat_at, claimed_by_node | Given claimed clustered job; When Requeue/Retry; Then row pending AND cluster columns NULL |
| T3 | Sweeper test | Test: same file | `go test -p 2 ./internal/queue/ -run TestSweepAbandonedTerminalStates` | SweepAbandonedTerminalStates undefined | failed rows older than N -> dead_letter (if budget exhausted) or counted; dead-in-table rows -> dead_letter; healthy rows untouched | Given 1 stale failed (budget out), 1 stale failed (budget left), 1 dead-in-table; When sweep; Then dead_letter has 2, jobs retains the retryable failed as pending-or-failed per contract C-Q2 |
| T4 | Operator retry bound + recovery parity | Test: same file | `go test -p 2 ./internal/queue/ -run 'TestRetryOperatorRespectsMaxRetries|TestDeadLetterRecoveryPreservesInteractive'` | Retry has no max check / recovery drops interactive | operator Retry past budget errors; recovered job keeps interactive=true | Given failed job with retry_count>=max; When bus Retry; Then error. Given interactive dead-letter row; When RecoverFromDeadLetter; Then interactive preserved |
| T5 | CHECK constraint test | Test: same file | `go test -p 2 ./internal/queue/ -run TestJobsStateCheckRejectsGarbage` | no CHECK in schema | direct INSERT of state='garbage' fails with constraint error | Given fresh schema; When INSERT with invalid state; Then constraint violation |

## Self-Verification Checklist

- [ ] All tests compile against CURRENT code and fail for missing-implementation reasons (not syntax errors)
- [ ] No production file modified
- [ ] Tests table-driven where inputs vary

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify commands + RED output tail; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Tests fail RED for implementation-missing reasons, not compile noise
- [ ] Coverage matches orchestrator Test Gate matrix F1-F5
- [ ] No production changes

## Notes

Survey existing store test helpers first (search_files 'func newTestStore' or similar in internal/queue).
