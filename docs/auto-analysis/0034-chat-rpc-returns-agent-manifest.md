# 0034: Chat RPC Returns Agent Manifest Instead of LLM Response

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **Critical** |
| Component | `internal/agent/dispatcher.go`, `internal/agent/loop.go` |
| Evaluation Dimension | Correctness, Helpfulness |
| Reporter | QA Phase 3 |

## Description

The `chat` RPC method returns the full JSON-serialized agent manifest (all 13 agent definitions with their complete system prompts) instead of the actual LLM-generated response. This makes the non-interactive `meept chat "message"` CLI completely non-functional for the `chat` agent.

## Reproduction

```bash
~/git/meept/bin/meept chat "write a Go function that reverses a string"
```

Expected: A Go function that reverses a string.
Actual: A JSON object containing all 13 agent definitions with full system prompts (~8KB of JSON).

## Evidence

Daemon log shows:
```
msg="Agent loop complete" agent=chat iterations=1 conversation=cli-15932
msg="Agent completed" component=dispatcher action=close agent=chat has_report=false
```

The agent runs, completes in 1 iteration, but `has_report=false`. The response contains the agent manifest JSON as the `reply` field.

The LLM (glm-4.7) appears to be regurgitating the system context (agent definitions) rather than generating a helpful response. The `has_report=false` indicates the agent loop's report extraction failed to find a structured report in the LLM output.

## Root Cause

Two related issues:
1. The LLM model (z.ai/glm-4.7) receives the full agent registry as context and regurgitates it as JSON instead of answering the user's question. The system prompt includes all agent definitions, which dominates the context window.
2. When `has_report=false`, the system falls back to returning the raw LLM output, which in this case is the agent manifest dump rather than a user-friendly error or retry.

## Impact

- **Critical**: Non-interactive chat mode is completely broken for simple queries routed to the `chat` agent
- Users see pages of JSON instead of helpful responses
- Makes automated/scripted usage of meept impossible

## Proposed Fix

1. Filter agent definitions from the system prompt to only include the routing table, not the full system prompts
2. Add output validation to detect agent manifest JSON in responses and retry or reject
3. When `has_report=false` and the raw output looks like JSON metadata, return a user-friendly error instead

## Classification

- Type: Bug (LLM context pollution)
- Regression: Unknown
- Priority: P0 - blocks all non-interactive usage
