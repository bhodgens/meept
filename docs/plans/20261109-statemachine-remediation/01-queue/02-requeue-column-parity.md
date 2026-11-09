# Requeue Column Parity + Operator Retry Bound - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT delete/skip/weaken gate tests. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** All requeue/retry paths clear the full claim column set (M-4); dead-letter recovery preserves interactive; operator-facing Retry gains a max-retries bound (F9).
- **Dependencies:** 00T-queue-tests.md
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** M-4, F9, audit 2026-11-09

## Goal

A row emerging from ANY non-terminal state carries no stale claim fields;
operator retry cannot loop forever outside the budget.

## Context

- internal/queue/store.go:954 (Requeue), :1019 (Retry), :1074 (ResetToPending), :1158 (ResetStaleClaimsAtStartup), store_cluster_claim.go:174 (remote reclaim)
- internal/queue/store.go:1462-1528 RecoverFromDeadLetter (drops interactive)
- queue.go:1102 bus Retry handler; api_handlers.go:1806 HTTP Retry

## Verified Anchors

Re-grep at dispatch; column set to align on: claimed_by, claim_token,
timeout_at, last_heartbeat_at, claimed_by_node (cluster variants where the
column exists).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Store method signatures unchanged. Behavioral contract:
// Every transition to state='pending' clears: claimed_by, claim_token, timeout_at, last_heartbeat_at, claimed_by_node (when present).
// RecoverFromDeadLetter restores interactive from dead_letter row.
// Retry (operator surfaces) returns ErrRetryBudgetExhausted when retry_count >= max_retries.
```

### What This Leaf Consumes

- Parent C-Q2 column-set definition.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Requeue/Retry clear cluster columns | Modify: internal/queue/store.go (954, 1019) | gate tests TestRequeueClearsClusterColumns, TestRetryClearsClusterColumns GREEN | RED | GREEN | Given clustered claimed row with timeout/heartbeat/node set; When Requeue or Retry; Then all cleared, retry semantics unchanged |
| T2 | Dead-letter recovery preserves interactive | Modify: internal/queue/store.go (RecoverFromDeadLetter SELECT + INSERT) | TestDeadLetterRecoveryPreservesInteractive GREEN | RED | GREEN | Given interactive dead-letter row; When recover; Then jobs row interactive=true |
| T3 | Operator retry bound | Modify: internal/queue/store.go (Retry) + queue.go bus handler error mapping | TestRetryOperatorRespectsMaxRetries GREEN | RED | GREEN; bus/HTTP map the sentinel to a 4xx-shaped error | Given retry_count >= max_retries; When operator Retry; Then ErrRetryBudgetExhausted, row unchanged |
| T4 | Keep worker-path semantics | Verify: full package | `go test -p 2 ./internal/queue/` | - | all pre-existing tests still GREEN (provider-wait requeue still preserves retry_count/interactive) | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] No behavioral change to retry_count preservation rules
- [ ] gofmt/vet clean

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] All four requeue paths + remote reclaim share the full column reset
- [ ] interactive preserved through dead-letter round trip
- [ ] budget sentinel surfaced through bus + HTTP handlers

## Notes

Do NOT clear next_retry_at semantics differently per path - keep existing
backoff vs notBefore distinction intact.
