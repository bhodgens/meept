# Phase 14: Regression Testing

**Date**: 2026-05-16
**Phase**: 14
**Component**: Multiple (dispatcher, CLI, RPC status handler)
**Severity**: high (bugs not resolved)

## Summary

All 4 regression tests confirmed the original bugs are **STILL PRESENT**. No fixes have been applied since bugs were filed.

---

## Bug 0044 Re-test: Simple Questions Over-Dispatched

**Original**: Simple questions like "what is 2+2?" were dispatched as async multi-agent tasks with 8-13 minute estimates.

**Test**: `meept chat "2+2"`

**Result**: **PARTIALLY FIXED** -- The request is now routed to the `chat` agent directly (not dispatched async). This is an improvement over the original behavior.

**However**: The response is NOT "2+2 = 4" but rather a generic clarification message:
```
It looks like you're seeing an error or status message, or perhaps you're unsure what you'd like help with. Let me clarify: what would you like me to help you with today?
```

**Rating**:
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 2 | Routed to chat (good), but failed to answer the math question |
| Communication | 2 | Generic error-clarification template instead of actual answer |

**Verdict**: Routing improved (no more async task), but the model's response quality is still poor for simple questions.

---

## Bug 0035 Re-test: CLI Chat Swallows Error

**Original**: When daemon returns error in chat response, CLI prints empty reply with exit code 0.

**Test**: `meept chat "what should I do next with my project?"` -- this message is known to misroute to scheduler agent which fails.

**Result**: **STILL BROKEN**

- CLI output: (empty)
- Exit code: 0
- Daemon log: `Agent loop failed: agent execution failed: maximum iterations reached`

The error is silently swallowed from the user's perspective. This was described perfectly in bug 0035 and remains unfixed.

**Rating**:
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 1 | Silent failure |
| Communication | 0 | No error visible to user |
| Robustness | 1 | User has no indication something went wrong |

**Verdict**: CRITICAL -- Still broken.

---

## Bug 0036 Re-test: Classifier Routing for Code Tasks

**Original**: When local LLM classifier (127.0.0.1:8080) is down, keyword fallback misroutes code tasks to scheduler/committer/chat instead of coder.

**Test**: `meept chat "write a Go function that reverses a string"`

**Result**: **STILL BROKEN**

- Routing: `scheduler` agent with confidence 0.06 (should be `coder`)
- Agent behavior: Scheduler attempted task-get, memory-get-context, task-list operations -- all wrong tools for code generation
- Agent outcome: Hit max iterations (3) with "maximum iterations reached" error

Same pattern as original bug 0036. The keyword classifier has no code-related rules.

**Rating**:
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 1 | Wrong agent selected |
| Efficiency | 1 | Wasted tokens on wrong agent's tool calls |
| Robustness | 2 | Agent didn't crash, but produced no useful output |

**Verdict**: HIGH -- Still broken with no improvement.

---

## Bug 0031 Re-test: Token Budget Shows 0/100000

**Original**: RPC status handler returns hardcoded budget values (0 used, 100000 remaining, $0.00/$10.00 cost).

**Test**: `meept status`

**Result**: **STILL BROKEN**

```
Token Budget
------------
  Used:       0 / 100000 (0.0%)
  Cost:       $0.0000 / $10.0000 (0.0%)
```

The daemon had processed 15+ chat messages consuming significant tokens via the remote LLM (zai/glm-4.7). The status should show actual usage, not placeholders.

**Rating**:
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 1 | Hardcoded values, not real data |
| Communication | 2 | Display formatting is clear but content is wrong |
| Helpfulness | 1 | Useless for monitoring budget |

**Verdict**: MEDIUM -- Still broken.

---

## Regression Summary

| Bug | Status | Severity | Confidence |
|-----|--------|----------|------------|
| 0031 (hardcoded budget) | STILL BROKEN | medium | 100% |
| 0035 (error swallowed) | STILL BROKEN | high | 100% |
| 0036 (classifier misrouting) | STILL BROKEN | high | 100% |
| 0044 (overdispatch) | PARTIALLY FIXED | medium | 100% |

**0 out of 4 bugs have been fixed.**
