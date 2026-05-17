# Token Budget Exhaustion Causes Total System Lockout

**Date**: 2026-05-15
**Phase**: 3 (multi-agent orchestration)
**Severity**: critical
**Component**: `internal/llm/budget.go`, `internal/agent/` (orchestrator, dispatcher, agent loop)

## Description

The token budget tracker enforces a hard hourly limit (default: 100,000 tokens). When compound-intent decomposition creates multiple parallel agent loops, each loop makes LLM calls that individually cost ~9,000-10,000 tokens. Within seconds, the cumulative token count exhausts the hourly budget, after which EVERY subsequent LLM request fails -- including the dispatcher's intent classifier, which is needed to route even simple chat messages.

Once the budget is exhausted, the system enters a total lockout state where no new requests can be processed until the hourly window resets. The escalation/retry mechanism then compounds the problem by repeatedly trying to re-plan failed tasks, each attempt hitting the same budget wall.

## Reproduction

1. Start the daemon with `zai/glm-4.7` as default model, hourly budget = 100,000
2. Send a message that triggers compound intent (e.g., "hello, what can you do?")
3. The dispatcher detects 3 intents, creates 4 steps, spawns 3+ parallel agents
4. Each agent loop burns ~9,000-10,000 tokens
5. After ~10 agent iterations total, hourly budget exceeds 100,000
6. ALL subsequent requests fail with "Token budget exceeded - request blocked"
7. System is unusable until the hourly window resets

## Evidence

Budget accumulation over ~90 seconds:
```
hourly=230     (dispatcher classification)
hourly=9777    (first agent loop iteration - glm-4.7 response)
hourly=10006   (second agent loop iteration)
hourly=10236   (third classification)
hourly=19938   (agent response)
hourly=30158   (planner decomposition)
hourly=40503   (subtask 1 execution)
hourly=49754   (subtask 2 execution)
hourly=59046   (subtask 3 execution)
hourly=68332   (review agent)
hourly=77536   (review retry with rate limit)
hourly=87032   (escalation re-plan)
hourly=96635   (escalation re-plan)
>> Token budget exceeded - every request after this fails <<
```

210 occurrences of "Token budget exceeded" logged over the next 2 minutes.

The system attempted to process 4 new chat requests after budget exhaustion. Each one:
1. Dispatcher LLM classifier fails
2. Falls back to keyword classifier (low confidence)
3. Dispatches to agent
4. Agent LLM call fails
5. Returns error to user

## Root Cause

Multiple compounding issues:

1. **No budget awareness in task decomposition**: The planner/decomposer does not check remaining token budget before creating subtasks. A simple "hello" message spawns 4 steps across 3 agents.

2. **Budget is global, not per-task or per-session**: The 100,000 token hourly budget covers ALL requests system-wide. Background tasks from prior sessions count against the same budget.

3. **Review agents consume budget unnecessarily**: After a task completes, a code-reviewer agent spins up and makes its own LLM call (~9,000 tokens), even for trivial tasks like storing a user preference.

4. **Escalation/retry loops waste budget**: When a step fails due to budget exhaustion, the escalation manager retries with backoff, but the retry also hits the same budget wall, creating a futile loop.

5. **No budget reset mechanism**: There is no way to manually reset the budget or gracefully degrade (e.g., switch to a cheaper model for classification only).

## Proposed Fix

1. **Per-task and per-session budget caps**: Add configurable `max_tokens_per_task` and `max_tokens_per_session` limits that prevent any single task from consuming more than X% of the hourly budget.

2. **Budget-aware task decomposition**: Before creating subtasks, check remaining budget and either:
   - Reduce the number of parallel agents
   - Switch to fast-path (single agent) mode
   - Return an error immediately rather than starting tasks that will fail

3. **Separate budget for classification vs execution**: Reserve a portion of the hourly budget (e.g., 10%) exclusively for intent classification. This ensures the dispatcher can always route requests even when execution budget is exhausted.

4. **Skip review for trivial tasks**: Don't spin up a review agent for simple memory_store operations or other low-risk tool calls.

5. **Budget-aware retry**: When a retry is triggered, check if the failure was budget-related and stop retrying immediately rather than wasting additional attempts.

6. **Manual budget reset CLI command**: Add `meept budget reset` or similar for operator intervention.

7. **Budget exhaustion graceful degradation**: When budget is exceeded, return a clear "budget exceeded, retry in N minutes" message to the user instead of a cryptic error chain.

## Resolution
**Partially Fixed** (2026-05-16)

Phase 1 completed -- core budget infrastructure for per-task/per-session caps:
1. Added `PerTaskTokenLimit` (default: 50,000) and `PerSessionTokenLimit` (default: 100,000) to `BudgetConfig` in both `internal/config/schema.go` and `internal/llm/budget.go`.
2. New `Budget.CheckBudgetWithScope(taskID, sessionID)` method validates per-task and per-session caps alongside hourly/daily budgets.
3. New `Budget.RecordTaskUsage()` and `Budget.RecordSessionUsage()` methods track granularity.
4. New `Budget.RecordUsageWithScope()` records usage with task/session scoping.
5. `Budget.GetStatus()` now reports per-task and per-session usage in its `Status` struct with `TaskBudgetExhausted` and `SessionBudgetExhausted` flags.
6. Daemon wiring in `internal/daemon/components.go` passes these fields from config to the budget tracker.
7. Startup logging augmented to display per-task and per-session budget limits.

Remaining (for agent-layer integration):
- Steps 2-7 in "Proposed Fix" require changes in `internal/agent/` (orchestrator, planner, dispatcher) to call `CheckBudgetWithScope()` and `RecordTaskUsage()` around each task execution. These are agent-layer changes and are tracked as follow-up work.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
