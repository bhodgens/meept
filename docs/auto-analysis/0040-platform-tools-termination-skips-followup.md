# 0040: Tool Termination Signals Skip LLM Follow-up (Bug #0005 Recurrence)

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **Medium** |
| Component | `internal/agent/loop.go` (tool termination) |
| Evaluation Dimension | Correctness, Efficiency |
| Reporter | QA Phase 3 |

## Description

Known bug #0005 observed again in Phase 3 testing. When the `committer` agent executes tools that signal termination (specifically `platform_tools`), the agent skips the LLM follow-up that would generate a user-facing response. The result is an empty reply with `has_report=false`.

## Reproduction

```bash
~/git/meept/bin/meept chat "create a file at ~/git/meept-playground/buggy-app/handler.go with a basic HTTP handler"
```

## Evidence

```
msg="Executing tool" agent=committer tool=file_read args_summary="{\"path\":\"~/git/meept-playground/buggy-app/.git/config\"}"
msg="Executing tool" agent=committer tool=list_directory args_summary="{\"path\":\"~/git/meept-playground/buggy-app\",\"recursive\":true}"
msg="Executing tool" agent=committer tool=platform_tools args_summary={}
msg="All tools signal termination, skipping LLM follow-up" agent=committer conversation=cli-72267 iteration=3
msg="Agent completed" component=dispatcher action=close agent=committer has_report=false
```

The committer agent:
1. Reads git config (reasonable)
2. Lists directory (reasonable)
3. Calls `platform_tools` (unnecessary for this task)
4. `platform_tools` signals termination
5. System skips LLM follow-up -> no response generated

## Root Cause

`platform_tools` returns a termination signal, and the agent loop logic treats any tool signaling termination as "the task is complete, don't bother asking the LLM for more". This is incorrect when the agent hasn't actually completed the user's task.

## Impact

- **Medium**: Tasks routed to `committer` produce empty responses
- Unnecessary tool calls waste tokens
- The tool call pattern (read config -> list dir -> platform_tools -> terminate) is a model behavior issue but the system should handle it gracefully

## Proposed Fix

See bug #0005 for the original analysis. The fix should:
1. Only skip LLM follow-up when the task was actually completed (check result indicators)
2. Don't treat `platform_tools` as a task-completing tool
3. If no response has been generated yet, always do a final LLM call for summary

## Classification

- Type: Bug (known, recurrence of #0005)
- Regression: No
- Priority: P2 - causes empty responses for committer-routed tasks
