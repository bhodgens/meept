# Phases 13-14-15 Summary: Communication Quality, Regression, End-to-End

**Date**: 2026-05-16
**Test harness**: `/Users/caimlas/go/bin/meept`
**Daemon**: running via `/Users/caimlas/go/bin/meept-daemon -f`
**Default LLM**: `zai/glm-4.7` (remote)
**Classifier LLM**: `local/lfm-code` (local llama.cpp at 127.0.0.1:8080) -- **unavailable**

---

## Executive Summary

Phases 13 (Communication Quality), 14 (Regression), and 15 (End-to-End Workflows) tested 5 + 4 + 4 = 13 scenarios across three dimensions of system quality. **13 out of 14 direct-chat scenarios failed**, primarily due to the keyword classifier fallback misrouting requests. The one compound-intent scenario succeeded via async dispatch.

**Critical Finding**: The system works well for complex, multi-step requests (compound intent detection is strong). The single-step/direct-chat path is largely broken when the local classifier is unavailable, because the keyword fallback misroutes most non-trivial requests.

---

## Phase 13: Communication Quality

**Overall verdict**: POOR across all 5 dimensions

| Test | Correctness | Communication | Efficiency | Cleverness | Robustness | Helpfulness |
|------|-------------|---------------|------------|------------|------------|-------------|
| Proactive suggestions | 3 | 2 | 4 | 1 | 3 | 2 |
| Proactive guidance | 1 | 0 | 1 | 0 | 1 | 0 |
| Mixed language | 2 | 1 | 3 | 1 | 3 | 2 |
| Roleplay | 1 | 1 | 4 | 0 | 3 | 1 |
| Emotional response | 3 | 2 | 4 | 2 | 4 | 3 |
| **Average** | **2.0** | **1.2** | **4.0** | **1.0** | **2.8** | **1.6** |

**Key issues**:
1. The chat agent uses a generic "error/clarification" template in response to ~60% of prompts, even when the prompt is not an error message.
2. No empathy for emotional content.
3. No roleplay/persona adoption.
4. No language awareness (French treated as English).
5. No proactive suggestions (no awareness of user state/context).

---

## Phase 14: Regression

**0 out of 4 bugs fixed.**

| Bug | Status | Severity |
|-----|--------|----------|
| 0031: Token budget hardcoded 0/100000 | STILL BROKEN | medium |
| 0035: CLI swallows error responses | STILL BROKEN | high |
| 0036: Classifier misroutes code tasks | STILL BROKEN | high |
| 0044: Simple questions over-dispatched | PARTIALLY FIXED | medium |

**Bug 0044 partial fix detail**: Simple questions like "2+2" are now routed directly to chat agent (not dispatched async). However, the chat agent still fails to answer, producing a generic clarification message instead.

---

## Phase 15: End-to-End Workflows

| Test | Result | Notes |
|------|--------|-------|
| Go project creation | FAILED | Misrouted to chat (keyword fallback) |
| Debug workflow | FAILED | Chat agent generic template |
| Memory store/retrieve | FAILED | Misrouted to scheduler (keyword "remember") |
| Multi-agent analysis | PASSED | Compound intent dispatch works |

**Pattern**: Compound/complex queries work via async multi-agent dispatch. Simple/medium direct-chat requests fail due to keyword classifier misrouting.

---

## Root Cause Analysis

The single dominant failure mode across all three phases is the **keyword classifier fallback** (bug 0036):

```
User message
    -> LLM classifier (zai/glm-4.7 via dispatcher prompt) -> works when available
    -> LOCAL classifier (lfm-code at 127.0.0.1:8080) -> CONNECTION REFUSED
    -> Keyword fallback -> MISROUTES most requests
    -> Wrong agent -> Wrong tools -> Max iterations or generic output
```

When the local classifier is down, the keyword fallback produces:
- "create project" -> scheduler (not coder)
- "remember" -> scheduler (not chat/memory)
- "tests failing, fix them" -> chat (should be debugger)
- "write a Go function" -> scheduler (not coder)
- "what should I do next" -> scheduler (not analyst/chat)

The compound intent detection appears to bypass the keyword fallback through a different code path (possibly through the orchestrator's own LLM classification), which explains why only the multi-agent test succeeded.

---

## Fix Priority

1. **P0 -- Bug 0036 (keyword classifier)**: Fix routing. This single fix would resolve:
   - Phase 13 tests 2, 3, 4, 5 (all misrouted)
   - Phase 14 bugs 0035 (error path), 0044 (overdispatch)
   - Phase 15 tests 1, 2, 3 (all misrouted)

2. **P1 -- Bug 0035 (error swallowing)**: Surface errors to CLI user.

3. **P1 -- System prompt refinement**: The chat agent's default response pattern is a "clarification template" that doesn't engage with user prompts naturally. The main LLM (zai/glm-4.7) should produce direct, contextual responses instead.

4. **P2 -- Bug 0031 (hardcoded budget)**: Connect status handler to live budget tracker.

5. **P3 -- Communication quality (persona, empathy, language)**: Model-level capability issues that may need prompt engineering or different model.

---

## Files Created

- `/Users/caimlas/git/meept/docs/auto-analysis/0062-phase-13-communication-quality.md`
- `/Users/caimlas/git/meept/docs/auto-analysis/0063-phase-14-regression.md`
- `/Users/caimlas/git/meept/docs/auto-analysis/0064-phase-15-end-to-end.md`
- `/Users/caimlas/git/meept/docs/auto-analysis/0065-phases-13-14-15-summary.md` (this file)
