# Async Dispatch Bypasses Budget Gate, Creates Zombie Tasks
**Date**: 2026-05-15
**Phase**: 2
**Severity**: medium
**Component**: `internal/agent/handler.go`, `internal/llm/budget.go`
**Evaluation Dimension**: correctness, robustness

## Description
When a message is classified as a compound task and dispatched asynchronously, the ChatHandler returns a task acknowledgment immediately without checking the token budget. The plan request is published to the orchestrator, which will eventually try to execute the steps via LLM -- at which point the budget check will fail, creating a zombie task that can never complete.

## Reproduction
1. With budget exceeded (or any budget state), send a message classified as compound:
   ```
   meept chat "what can you do?"
   ```
2. Observe: task acknowledgment returned immediately (budget not checked)
3. The orchestrator later tries to execute the plan, fails on budget, task becomes stuck

## Evidence
In `internal/agent/handler.go` (line 479-495):
```go
case h.dispatcher.ShouldDispatchAsync(result) && result.Task != nil:
    // Async dispatch: send ack immediately, let orchestrator handle it
    // ... NO budget check here ...
    reply = h.FormatEnhancedAsyncTaskAck(result, steps, ...)
    h.publishPlanRequest(result, conversationID)
```

The budget is only checked when `RouteToAgent` is called (line 502), which calls `agent.RunOnce` (line 708), which calls the LLM client, which checks the budget. But the async dispatch path at line 479 skips `RouteToAgent` entirely.

## Root Cause
The async dispatch path was designed to decouple request acceptance from execution, but it doesn't validate that execution will actually be possible. There's no pre-flight budget check before accepting the task.

## Impact
- Creates tasks that can never complete (zombie tasks)
- User sees acknowledgment ("starting task... est. 8-13 min") but nothing will happen
- Misleading user feedback suggesting the task is being worked on
- Accumulates failed task records in the database

## Fix Applied (2026-05-16)

Added budget pre-check to async dispatch path in `ChatHandler.handleRequest` (`internal/agent/handler.go`).

### Changes
1. Added `budget *llm.Budget` field to `ChatHandler` with `SetBudget()` setter method.
2. Wired budget from daemon at both ChatHandler creation points in `internal/daemon/components.go`.
3. In the async dispatch branch of `handleRequest`: before sending the ACK, check `h.budget.CheckBudget()`. If budget is exceeded:
   - Set the task state to `failed` via `taskStore.Update()` to mark it as terminal
   - Return a `BudgetExceededError` instead of publishing the plan request
   - The normal error response path sends the budget error back to the user

This prevents zombie tasks from being created when the token budget is exhausted. The task that `ClassifyAndRoute` creates is immediately cancelled, and no plan request is published to the orchestrator.

## Classification
- Design gap in async dispatch path
- Creates misleading user experience
- Related to bug 0034 (budget blocks all chat)
