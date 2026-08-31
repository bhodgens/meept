# Plan: Skill evolution: improve subagent-orchestrator-race-on-nested-completion

## Meta

- plan_id: plan-20260831224744-0042
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.30 (64 positive vs 111 negative out of 214 injections), well below threshold. The skill handles race conditions on nested subagent completion but likely lacks robust synchronization primitives, completion-detection logic, and fallback strategies — leading to frequent timeout or stale-state failures.

Candidate content:
---
name: subagent-orchestrator-race-on-nested-completion
description: |
  Detects and resolves race conditions that occur when an orchestrator agent
  waits for nested subagent completions. Handles early returns, duplicate
  signals, missed notifications, and timeout windows.
metadata:
  version: "2.0"
  author: Sapiens AI
  updated: 2026-08-31
---

# Subagent Orchestrator — Race on Nested Completion

## Problem

When an orchestrator dispatches nested subagents, a race can occur between:
- The subagent finishing and emitting a completion signal.
- The orchestrator polling or waiting for that signal.
- A timeout expiring before the signal is processed.
- Multiple completion signals arriving for the same subagent.

These races produce missing results, duplicate work, or premature timeouts.

## Detection Checklist

Before acting, verify the race condition:

1. **Timing audit** — Is the subagent's reported finish time within the orchestrator's wait window?
2. **Signal deduplication** — Are there multiple completion events for a single subagent ID?
3. **Timeout margin** — Is the configured timeout smaller than the subagent's typical latency p95?
4. **State consistency** — Does the subagent's state transition (PENDING → RUNNING → DONE) follow a valid path without reversals?

## Resolution Strategies

### 1. Idempotent Completion Signal

Accept only the first valid completion signal for each subagent ID. Subsequent signals are logged and ignored.

```
completed_ids = set()

when subagent_signal(id, result):
    if id in completed_ids:
        log("duplicate completion, ignored")
        return
    completed_ids.add(id)
    emit_completion(id, result)
```

### 2. Exponential Backoff with Jitter

Replace fixed polling intervals with exponential backoff capped at a maximum retry count, adding random jitter to prevent thundering-herd effects.

```
poll_interval_ms = min(initial_ms * (2 ^ attempt), max_interval_ms)
actual_delay = poll_interval_ms * (0.5 + random.uniform(0, 1))
await sleep(actual_delay)
```

### 3. Heartbeat-Based Liveness

Subagents emit periodic heartbeat messages. The orchestrator treats a missed heartbeat exceeding N intervals as a hard failure, avoiding indefinite waits.

```
heartbeat_timeout = HEARTBEAT_INTERVAL * MAX_MISSED_COUNTS
if now() - last_heartbeat > heartbeat_timeout:
    mark_as_failed(subagent_id, "heartbeat_timeout")
    escalate()
```

### 4. Deadline Propagation

Compute and propagate absolute deadlines (not durations) to nested subagents so they self-terminate before the parent's timeout expires.

```
parent_deadline = orchestrator_now() + PARENT_TIMEOUT_MS
subagent_deadline = parent_deadline - SLACK_BUFFER_MS
pass(subagent_deadline) to nested subagent
```

### 5. State-Transition Lock

Use an atomic compare-and-swap on subagent state to ensure only one transition path is accepted per ID.

```
if compare_and_swap(state[id], FROM, TO):
    proceed()
else:
    log(f"invalid transition {state[id]} -> {TO} for {id}")
```

## Anti-Patterns to Avoid

- **Polling without backoff**: Starves the event loop and increases false negatives.
- **Global timeouts without per-subagent deadlines**: One slow subagent blocks the entire orchestration.
- **Silently ignoring late signals**: Results may be needed for downstream dependents.
- **Assuming FIFO signal delivery**: Out-of-order completions are normal in distributed systems.

## Recovery Actions

| Symptom | Recovery |
|---|---|
| Timeout before completion received | Retry once with extended deadline; then mark failed |
| Duplicate completion signals | Deduplicate using idempotent set (Strategy 1) |
| State reversal detected | Revert to last known valid state; log anomaly |
| Heartbeat lost | Escalate after N misses; trigger subagent restart if safe |
| Late-arriving result after timeout | Cache result; notify parent asynchronously |

## Implementation Notes

- All strategies above are composable. Start with Strategies 1 and 4 (most impact, lowest complexity).
- Prefer absolute deadlines over relative timeouts for nested structures.
- Log every race-resolution event with subagent ID, timestamp, and strategy applied for debugging.
- Always include a fallback path: if all race-mitigation fails, surface the error to the caller with diagnostic context.


## Notes

