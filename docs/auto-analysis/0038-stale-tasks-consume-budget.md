# 0038: Stale Queue Jobs Consume Budget on Daemon Restart

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **High** |
| Component | `internal/daemon/daemon.go`, `internal/task/`, `internal/queue/` |
| Evaluation Dimension | Robustness, Efficiency |
| Reporter | QA Phase 3 |

## Description

When the daemon restarts, it recovers stale tasks from the persistent SQLite queue database and immediately starts processing them. These stale tasks (from previous sessions) consume the entire hourly token budget before any new user requests can be processed. New user requests then fail with "Token budget exceeded - request blocked".

## Reproduction

```bash
# 1. Run some tasks that create queue jobs
~/git/meept/bin/meept chat "fix the bug in ~/git/meept-playground/buggy-app/main.go"
# Let it create planning/review/execution jobs

# 2. Stop and restart daemon
~/git/meept/bin/meept daemon stop
~/git/meept/bin/meept daemon start

# 3. Immediately try a new chat
~/git/meept/bin/meept chat "hello"
# Result: "Token budget exceeded - request blocked"
```

## Evidence

Daemon startup log:
```
msg="Recovered stale tasks" count=9
msg="daemon: recovered stale tasks from previous run" count=9
msg="ASSIGN job claimed" worker_id=worker-... job_id=job-... agent_id=planner
# Immediately starts processing stale planner jobs
msg="LLM call failed" agent=planner error="Token budget exceeded - request blocked"
# Budget already consumed by stale jobs from previous session
```

The stale task retry loop creates an exponential cascade:
1. Stale jobs fail (budget exceeded)
2. Retry with backoff
3. Each retry attempt also checks budget and fails
4. Escalation manager creates new re-plan steps
5. Those also fail, creating more retries
6. Budget is consumed by the overhead of failing repeatedly

## Root Cause

1. Task/queue state persists in SQLite across daemon restarts
2. The daemon eagerly recovers and processes stale tasks on startup
3. No budget reservation for new user requests vs. recovered tasks
4. The retry mechanism treats budget exhaustion as "transient_error" and retries indefinitely
5. Escalation on failure creates new planning steps that also fail, amplifying the problem

## Impact

- **High**: Daemon becomes unusable after restart if stale tasks exist
- Budget exhaustion creates a death spiral of retries
- No way to process new user requests until the hourly budget resets
- Requires manual database cleanup to recover

## Proposed Fix

1. On startup, mark all pending/processing jobs as "cancelled" or "stale" rather than resuming them
2. Add a budget reservation system: reserve 30% of budget for new user requests
3. Classify budget exhaustion as non-retryable (it's already classified but retry logic ignores this)
4. Add a startup flag to clear the queue: `meept-daemon --clean-start`
5. Add a CLI command to clear the queue: `meept queue clear`

## Classification

- Type: Bug (resource management)
- Regression: No
- Priority: P1 - causes daemon lockout after restart
