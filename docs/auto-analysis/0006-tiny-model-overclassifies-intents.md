# Tiny Model Over-Classifies Simple Greeting as Compound Intent

**Date**: 2026-05-15
**Phase**: 2 (core agent loop & LLM integration)
**Severity**: medium
**Component**: `internal/agent/dispatcher.go` (classifier), classifier model config

## Description

The classifier model (local `LFM2.5-1.2B-Instruct.Q8_0`) detects 3 parallel intents from a simple "hello, what can you do?" greeting. This triggers the compound intent path, which creates a multi-step task plan with 3 subtasks across scheduler and chat agents.

For a simple greeting, this causes:
- 3 subtasks instead of 1 simple chat response
- Orchestrator + planner + chat + reviewer agent chain
- ~60 seconds of processing for what should be a 5-second response
- 18,892 tokens consumed (vs. ~500 for a direct response)

## Reproduction

1. Configure a tiny local model as classifier
2. Send any casual message: `./bin/meept chat "hello"`
3. Observe: `Compound intent detected intents=3 type=parallel`

## Evidence

```
level=DEBUG msg="Making LLM request" component=classifier-llm url=http://127.0.0.1:8080/v1/chat/completions model=LFM2.5-1.2B-Instruct.Q8_0
level=INFO msg="Compound intent detected" component=dispatcher intents=3 type=parallel
```

## Root Cause

This is primarily a model quality issue — the tiny 1.2B model lacks the capability to properly classify single-intent messages. However, the harness has no safeguards:

1. No minimum confidence threshold for compound intent detection
2. No sanity check on intent count vs. message length
3. No fallback to simple chat for very short messages
4. The dispatcher trusts the classifier output unconditionally

## Proposed Fix

Add guardrails to the dispatcher:
1. If message length < N characters, skip classifier and route directly to chat agent
2. If compound intent count seems excessive for the input length, fall back to single intent
3. Add a configurable confidence threshold for compound vs. single intent
4. Log the classifier's raw output at debug level for diagnosis (may already happen)

Note: This is a harness resilience issue — the harness should be robust against poor classifier output, not just trust it blindly.

## Model vs Harness
[X] Harness bug (missing guardrails)  [X] Model quality issue  [ ] Both

## Resolution
**Status: FIXED** (Round 6 - Dispatcher Heuristics)

Applied the following guardrails in `internal/agent/dispatcher.go`:
1. **Short-message guard** (`isShortSimpleMessage`): Messages under 50 chars without specialist keywords, or with purely conversational patterns, route directly to chat at 0.9 confidence, bypassing the full classifier chain.
2. **Compound confidence threshold** (`DetectCompound`): Requires at least 2 intents with confidence >= 0.5, and at least one must be a non-chat/non-platform intent.
3. **Compound signal word filter** (`hasCompoundSignalWords`): `classifyMultiIntent` only proceeds to multi-intent analysis if the message is >= 80 chars AND contains compound signal words (and also, plus, while, then, etc.).
4. **Heuristic fallback** (`heuristicFallback`): When all classifiers fail, targeted keyword rules route code/debug/git tasks to the correct specialist agents with proper confidence thresholds before falling to generic chat.
5. **Keyword classifier minimum confidence**: Keyword classifier results below 0.3 confidence are rejected, deferring to heuristic fallback.
