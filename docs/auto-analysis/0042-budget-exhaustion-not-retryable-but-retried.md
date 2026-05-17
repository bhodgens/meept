# 0042: BudgetExceededError Classified as Non-Retryable but Still Retried

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **Medium** |
| Component | `internal/queue/`, `internal/agent/orchestrator.go` |
| Evaluation Dimension | Robustness, Efficiency |
| Reporter | QA Phase 3 |

## Description

When a job fails with `BudgetExceededError`, the job is marked as non-retryable (`NonRetryable() -> true`) and the log says "Non-retryable error - skipping retry". However, the tactical orchestrator still retries the job, creating a loop of repeated failures that consume no tokens but generate excessive log entries and prevent the daemon from processing new requests.

## Reproduction

```bash
# Exhaust the token budget (run complex tasks)
# Then watch the queue retry loop
~/git/meept/bin/meept queue status
# Shows dead_letter piling up
```

## Evidence

```
msg="Job failed" id=job-... state=failed error="...Token budget exceeded..."
msg="Non-retryable error - skipping retry" error="...Token budget exceeded..."
msg="Error processing job" worker=... error="...Token budget exceeded..."
msg="Job failed event received" component=orchestrator
msg="Retryable error detected, retrying job with backoff" component=tactical retry_count=1 reason=transient_error
msg="Job queued for retry with backoff" id=job-... retry_count=1 backoff=2s
```

The same job is retried with increasing backoff (2s, 4s, 8s) until it reaches max retries and goes to dead_letter.

The log shows `NonRetryableError` is correctly detected at the job level, but the tactical orchestrator's `isRetryable` function still returns `true` with `reason=transient_error`.

## Root Cause

Two separate retry mechanisms:
1. **Job-level retry** (`internal/queue/`): Correctly identifies `BudgetExceededError` as non-retryable and skips
2. **Orchestrator tactical retry** (`internal/agent/orchestrator.go`): Has its own retry logic that treats failures differently, classifying budget exhaustion as `transient_error` and retrying with backoff

The orchestrator's `isRetryable()` function doesn't check for `NonRetryableError` interface.

## Impact

- **Medium**: Creates a retry storm on budget exhaustion
- Each retry attempt generates multiple log entries
- Workers are tied up processing doomed retries
- Tasks escalate through multiple levels (3 levels observed), creating more retry loops
- Eventually fills the dead letter queue (49 entries observed)

## Proposed Fix

1. Check for `NonRetryableError` interface in the tactical orchestrator's `isRetryable()` function
2. If the error implements `NonRetryable() bool` and returns true, don't retry
3. Add budget exhaustion to the list of known non-retryable errors in the orchestrator

## Classification

- Type: Bug (inconsistent error handling)
- Regression: No
- Priority: P2 - causes retry storms during budget exhaustion
