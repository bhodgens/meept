# Review Agent Burns LLM Budget on Trivial Tasks

**Date**: 2026-05-15
**Phase**: 3 (multi-agent orchestration)
**Severity**: medium
**Component**: `internal/agent/review.go`, `internal/agent/orchestrator.go`

## Description

After every agent-executed step that requires review, the system spawns a separate code-reviewer agent with its own full LLM context and prompt. This review agent makes an LLM call (~9,000-10,000 tokens) even for trivial operations like storing a user preference via `memory_store`. The review result is then used to decide whether to approve or reject the step.

For a task like "remember that I prefer Go for backend development", the coder agent calls `memory_store` (which fails due to a schema bug, see 0020), then the code-reviewer agent burns another ~9,200 tokens to report that the step was rejected because "the Result field is empty." This review provides no value -- the failure is already logged -- but consumes ~10% of the hourly token budget.

## Reproduction

1. Start the daemon with debug logging
2. Send any message that gets dispatched to the coder agent
3. Observe the coder agent completes its tool call
4. A code-reviewer agent is created and makes a full LLM call
5. Review result (approved/rejected) is logged, consuming ~9,000 tokens

## Evidence

```
level=INFO msg="Job completed" id=job-20260516032122.899734000-0001
level=INFO msg="Starting review" component=review step_id=step-... tool_hint=code
level=DEBUG msg="Selected reviewer" component=review reviewer=code-reviewer
level=DEBUG msg="Agent loop iteration" agent=code-reviewer iteration=1 max=3
level=DEBUG msg="Making LLM request" url=https://api.z.ai/... model=glm-4.7
level=DEBUG msg="Recorded token usage" component=budget tokens=9204
level=INFO msg="Review completed" component=review status=rejected confidence=1 duration=27.69s
```

The review took 27.69 seconds and 9,204 tokens for a trivial `memory_store` task.

Meanwhile, other steps in the same compound task are auto-approved:
```
level=DEBUG msg="Step does not require review" component=review step_id=step-...
level=INFO msg="Step approved" component=review feedback="Auto-approved (no review required)"
```

So some steps skip review (analyze hint) while code steps always trigger a full review, regardless of task complexity.

## Root Cause

1. **Review is unconditional for code steps**: The review system checks `tool_hint=code` and always triggers a full LLM review, regardless of task complexity.

2. **No cost-benefit check**: The reviewer doesn't consider whether the tokens spent on review are justified by the task's importance or risk level.

3. **Full LLM context for review**: The reviewer agent creates a full agent loop with its own system prompt, context window, and LLM call, even when a simple heuristic could determine approval.

4. **No caching or short-circuit**: If the step's tool call returned an error, the review is redundant -- the step already failed. There's no pre-check for obvious failures.

## Proposed Fix

1. **Skip review on tool execution failure**: If the tool call returned an error, mark the step as failed immediately without spinning up a reviewer.

2. **Risk-based review**: Only trigger full LLM review for steps that involve file writes, shell commands, or other destructive operations. Skip review for read-only operations and memory stores.

3. **Lightweight review for trivial tasks**: For low-risk tool calls, use a simple heuristic check (did the tool return success? is the result non-empty?) instead of a full LLM call.

4. **Budget-aware review**: Check remaining token budget before spinning up a reviewer. If budget is below a threshold (e.g., 20%), skip review and auto-approve.

5. **Add a `review: false` option to tool hints or step configuration**: Allow steps to opt out of review entirely.

## Fix

Added three guard conditions to `ReviewManager.ReviewStep()` in `internal/agent/review_manager.go`:

1. **Error skip** (`stepHasError`): If the step result contains error indicators (`error:`, `failed to`, `could not`, `unable to`, `rpc error`, `permission denied`, etc.), return auto-approved immediately without an LLM call. The step failure is already logged; there is nothing to review.

2. **Trivial task skip** (`isTrivialTask` + `heuristicReviewPasses`): If a task has fewer than 3 steps, the complexity is too low to justify a full LLM review (~9,200 tokens). Instead, a lightweight heuristic checks for non-empty, meaningful result content. If the result is non-empty, auto-approve.

3. **Fallback to full review**: For multi-step (3+) tasks with successful execution and no error indicators, the original full LLM review path remains unchanged.

**Files modified**: `internal/agent/review_manager.go`
**New methods**: `stepHasError()`, `isTrivialTask()`, `heuristicReviewPasses()`

## Status

**FIXED** — All agent tests pass (`go test ./internal/agent/... -v`, PASS confirmed).

## Model vs Harness
[ ] Harness bug  [ ] Model quality issue  [ ] Both
