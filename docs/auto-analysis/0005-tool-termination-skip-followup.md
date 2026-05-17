# Tool Termination Skips LLM Follow-up, Causing Review Rejection

**Date**: 2026-05-15
**Phase**: 2 (core agent loop)
**Severity**: high
**Component**: `internal/agent/loop.go`

## Description

When an agent executes tools (e.g., `platform_tools`, `platform_agents`) and all tools return a termination signal, the agent loop skips the LLM follow-up step that would synthesize the tool results into a coherent response. This results in:

1. The agent producing no textual output — only tool call results
2. The review system rejecting the work as "no response provided"
3. Revision steps being created but getting stuck ("No ready steps to schedule")
4. The task never completing

## Reproduction

1. Start daemon with `--debug`
2. Send `./bin/meept chat "hello, what can you do?"`
3. The classifier (tiny model) detects 3 intents → compound task
4. Planner creates 3 subtasks
5. Chat agent runs `platform_tools` and `platform_agents`
6. Agent loop logs: `All tools signal termination, skipping LLM follow-up`
7. Reviewer rejects: "No response/result provided"
8. Revision step created but never scheduled → task stuck

## Evidence

```
level=INFO msg="Executing tool" agent=chat tool=platform_tools
level=INFO msg="Executing tool" agent=chat tool=platform_agents
level=INFO msg="All tools signal termination, skipping LLM follow-up"
level=INFO msg="Review completed" status=rejected confidence=0.95
level=INFO msg="Step rejected" issues="[No response/result provided ...]"
level=DEBUG msg="No ready steps to schedule"
```

## Root Cause

In `internal/agent/loop.go`, when all executed tools signal termination (`ShouldTerminate` returns true), the loop exits without calling the LLM one final time to synthesize the tool results into a response. The agent loop optimizes by treating tool termination as "we're done" but this skips the critical synthesis step.

The correct behavior should be: after tools execute and return results, call the LLM one more time with the tool results in context so it can produce a final text response.

## Proposed Fix

After tool execution, when all tools signal termination:
1. Still do one final LLM call with the accumulated tool results in context
2. The LLM should synthesize the results into a user-facing response
3. Then terminate the loop

Alternatively, if skipping the LLM is intentional (to save tokens), then the review system should not reject responses that consist solely of tool outputs — it should pass the tool results through.

## Resolution

**Status**: FIXED

**Date**: 2026-05-16

**Change**: `internal/agent/loop.go` lines 2331-2337

When `ShouldTerminate()` returns true, the loop now makes a final LLM call (`chatWithFailover`) with the tool results already in the conversation context. Tools are omitted from the call so the LLM produces a text synthesis rather than further tool calls. A wrap-up instruction is injected to guide the LLM to summarize the results for the user. If the synthesis call fails, it falls back to `buildTerminateResponse` (raw JSON).

**Test update**: `internal/agent/loop_test.go` -- `TestAgentLoop_TerminatePathSkipsLLMFollowUp` renamed to verify 2 LLM calls (tool call + synthesis) instead of 1.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
