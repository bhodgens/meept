# Over-Classification of Simple Chat as Compound Intent
**Date**: 2026-05-15
**Phase**: 2
**Severity**: high
**Component**: `internal/agent/dispatcher.go`
**Evaluation Dimension**: correctness, efficiency

## Description
Simple chat messages are being classified as compound (multi-intent) tasks and dispatched to multiple agents asynchronously, even for straightforward conversational inputs. This results in task plans being created for simple greetings, thank-yous, and philosophical questions.

## Reproduction
Send any message that happens to match keyword patterns for multiple intents:
```
~/git/meept/bin/meept chat "what can you do?"
~/git/meept/bin/meept chat "thanks, that's all for now"
~/git/meept/bin/meept chat "what is the meaning of life?"
~/git/meept/bin/meept chat "pretend you are a pirate and explain what you do"
```

## Evidence
Messages classified as compound tasks (multi-agent dispatch):
- "what can you do?" -> 2 subtasks, agents: chat + scheduler
- "do you remember anything about our previous conversations?" -> 2 subtasks
- "thanks, that's all for now" -> 2 subtasks, agents: chat + scheduler
- "can you summarize what I just told you about my project?" -> 3 subtasks
- "what is the meaning of life?" -> 2 subtasks, agents: analyst + scheduler
- "pretend you are a pirate and explain what you do" -> 2 subtasks

None of these should be compound tasks. They are simple conversational messages that should be handled by a single agent in a single pass.

## Root Cause
This is likely the same issue documented in bug 0006 (tiny model overclassifies intents), but observed again with the current model. The `classifyMultiIntent()` function runs both keyword and LLM classifiers, and the results are aggregated. With the LLM classifier failing due to budget issues, the keyword classifier may be over-matching, or the multi-intent detection threshold is too low.

The `DetectCompound()` method likely has a low bar for what constitutes "compound" -- if 2+ intents are detected with even low confidence, the message is treated as compound.

## Impact
- Simple messages get inflated into multi-agent tasks with 8-13 minute time estimates
- Wastes resources (task creation, agent scheduling, bus messages)
- Poor user experience: "hello" should not produce a task plan
- Related to known bug 0006 but persists with current configuration

## Proposed Fix
1. Add a minimum confidence threshold for compound detection (e.g., both intents must have >0.6 confidence)
2. Add a "chat" intent bypass: if the primary intent is clearly "chat" type, skip compound detection
3. Add a minimum complexity heuristic: messages shorter than N characters or lacking conjunctions should not be classified as compound
4. Log the classification chain for debugging: which classifier produced which intents at what confidence

## Classification
- Directly impacts user experience
- Related to bug 0006 (tiny model overclassification)
- May be exacerbated by budget-related LLM classifier failure (fallback to keyword-only)

## Resolution
**Status: FIXED** (Round 6 - Dispatcher Heuristics)

Applied three layered guardrails:
1. **Compound signal word filter** (`hasCompoundSignalWords` in `classifyMultiIntent`): Messages under 80 chars without compound conjunctions (and also, as well as, plus, while, then, etc.) are skipped from multi-intent analysis entirely.
2. **Confidence floor for compound** (`DetectCompound`): Requires at least 2 intents with confidence >= 0.5 AND at least one non-chat/non-platform intent. A "chat + scheduler" pair (e.g. "thanks, that's all") no longer triggers compound routing.
3. **Short-message guard** (`isShortSimpleMessage` in `classifyIntent`): Messages under 50 chars without specialist keywords are routed directly to chat before any classifier chain runs, preventing false positives even from poor LLM classifier output.
