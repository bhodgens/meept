# 0037: Chat Agent Produces Empty Reply (has_report=false)

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **High** |
| Component | `internal/agent/loop.go`, `internal/agent/handler.go` |
| Evaluation Dimension | Correctness, Communication |
| Reporter | QA Phase 3 |

## Description

The `chat` agent consistently completes with `has_report=false` and produces an empty reply string. The agent loop runs 1 iteration, the LLM call succeeds (no errors logged), but the response is empty. This affects all requests routed to the `chat` agent, making them return nothing to the user.

## Reproduction

```bash
~/git/meept/bin/meept chat "hello"
# Output: (empty, just a newline)
```

## Evidence

Daemon log for multiple conversations:
```
msg="Agent loop complete" agent=chat iterations=1 conversation=cli-71890
msg="Agent completed" component=dispatcher action=close agent=chat has_report=false

msg="Agent loop complete" agent=chat iterations=1 conversation=cli-72640
msg="Agent completed" component=dispatcher action=close agent=chat has_report=false

msg="Agent loop complete" agent=chat iterations=1 conversation=cli-81386
msg="Agent completed" component=dispatcher action=close agent=chat has_report=false
```

No LLM errors logged for these conversations - the LLM call succeeds but the response extraction fails.

## Root Cause

The agent loop processes the LLM response but fails to extract a usable report. The `has_report=false` flag indicates the report router found no structured report in the LLM output. When no report is found, the raw response should be used as a fallback, but the raw response also appears to be empty.

Possible causes:
1. The LLM response contains only tool calls (like `platform_agents`) that don't produce text output
2. The response stripping logic removes all content, leaving an empty string
3. The LLM returns empty content (known issue #0007) and the nudge loop doesn't fix it within 1 iteration

Note: The first call to the daemon DID produce output (the agent manifest JSON). Subsequent calls return empty. This suggests the LLM's first response fills some cache or context, and subsequent calls with the same conversation ID hit a different code path.

## Impact

- **High**: All `chat` agent requests return empty responses
- Users see no output at all - just a blank line
- Makes the CLI completely unusable for conversational queries
- No error message to indicate what went wrong

## Proposed Fix

1. Add logging when `has_report=false` to show what the raw LLM response contained
2. Implement a proper fallback: if report extraction fails, return the raw LLM text content
3. If both report and raw text are empty, return a user-visible error: "Agent completed but produced no response"
4. Increase the minimum iteration count to allow the nudge mechanism to work (currently stops at 1 iteration)

## Classification

- Type: Bug (response extraction failure)
- Regression: Unknown
- Priority: P1 - makes chat agent completely non-functional
