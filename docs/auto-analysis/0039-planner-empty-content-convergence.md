# 0039: Planner Agent Returns Empty Content, Hits Convergence Detection

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **Medium** |
| Component | `internal/agent/loop.go` (convergence detection) |
| Evaluation Dimension | Correctness, Efficiency |
| Reporter | QA Phase 3 |

## Description

The planner agent (used for strategic planning and task decomposition) consistently returns empty content from LLM calls. After 3 consecutive empty responses, the convergence detection kicks in and aborts the loop with "agent responses converged without progress". This causes all compound/complex task planning to fail.

## Reproduction

Any compound or complex task that triggers strategic planning:
```bash
~/git/meept/bin/meept chat "create a new module in ~/git/meept-playground/buggy-app/ called 'utils'"
# Triggers compound intent -> strategic planning -> planner agent
```

## Evidence

```
msg="LLM returned empty content, nudging for more information" agent=planner iteration=1
msg="LLM returned empty content, nudging for more information" agent=planner iteration=2
msg="Convergence detected in responses" agent=planner content_hash=e3b0c442 count=3
msg="Convergence detected, aborting loop" agent=planner iteration=3
msg="Reasoning cycle failed" error="agent responses converged without progress"
msg="Plan generation failed, creating single-step fallback"
```

The content hash `e3b0c442` is the SHA-256 hash of an empty string, confirming all 3 responses were empty.

The system creates a "single-step fallback" which delegates to the `chat` agent, but that agent also returns empty responses (bug #0037).

## Root Cause

1. The planner agent's LLM calls return empty content (potentially related to the model or prompt construction)
2. The nudge mechanism ("provide more information") doesn't fix the empty content issue
3. Convergence detection (correctly) identifies 3 identical empty responses and aborts
4. The fallback creates a single-step plan that routes to `chat`, which also fails

This may be related to the z.ai/glm-4.7 model's behavior with the planner's system prompt - the model may not produce text output for planning queries, or the response parsing strips all content.

## Impact

- **Medium**: Complex/multi-step tasks cannot be planned
- Falls back to single-step execution, losing the benefit of task decomposition
- Wastes 3 LLM calls (with nudging) before giving up
- Combined with bug #0037, the fallback also fails

## Proposed Fix

1. Investigate why the planner LLM calls return empty - check prompt construction and response parsing
2. Add model-specific response handling for models that return structured JSON instead of text
3. Reduce the nudge iterations for empty content from 3 to 1 to avoid wasting tokens
4. Log the actual LLM response (even if empty) for debugging

## Classification

- Type: Bug (LLM interaction)
- Regression: Unknown
- Priority: P2 - degrades complex task handling
