# Dispatcher Routes Simple Questions as Full Multi-Agent Tasks
**Date**: 2026-05-15
**Phase**: 1
**Severity**: medium
**Component**: internal/agent/dispatcher.go
**Evaluation Dimension**: efficiency, helpfulness

## Description
The dispatcher classifies simple conversational questions (like "what is 2+2?") as requiring multi-agent orchestration, routing them through analyst and scheduler agents as async tasks. This results in a task acknowledgment instead of an immediate answer, with estimated completion times of 8-13 minutes for trivial questions.

## Reproduction
```bash
~/git/meept/bin/meept chat "what is 2+2?"
# Returns:
## starting task
**task:** what is 2+2?
**id:** `task-20260516060445.961607000`
**plan:** `task-20260516060445.961607000` | 2 subtasks | est. 8-13 min
**agents:** analyst, scheduler
**subtasks:**
- what is 2+2? (scheduler)
- what is 2+2? (analyst)
```

## Evidence
Observed with multiple simple messages:
- "What is 2+2?" -> task with analyst + scheduler, 2 subtasks, 8-13 min estimate
- "hello what is up?" -> task with analyst + chat + scheduler, 3 subtasks, 12-17 min estimate

Meanwhile, "hello" alone correctly routes to a simple chat response.

## Root Cause
The LLM classifier (local model at 127.0.0.1:8080) is unavailable, causing fallback to the keyword classifier. The keyword classifier apparently matches math-related words ("2+2") to analyst/scheduler capabilities. The `ShouldDispatchAsync()` method returns true for these intent types, triggering the async task pipeline instead of inline chat.

The classifier chain:
1. LLM classifier at 127.0.0.1:8080 -> FAILS (connection refused)
2. Keyword classifier -> matches poorly with low confidence
3. Intent type gets `RequiresPlanning=true`, triggering async dispatch

## Impact on Platform Quality
- Simple questions get 8-13 minute estimated completion times
- Tasks pile up in the queue (6 pending observed) and never complete (no workers claim them)
- Dead letter queue fills up (30 items observed)
- User experience is terrible for basic interactions

## Proposed Fix
1. Add a confidence threshold for async dispatch: if classifier confidence < 0.7 AND the message is short (< 100 chars), default to inline chat
2. Add heuristic: single-sentence messages should rarely be dispatched async
3. Fix the local LLM classifier dependency (make it optional, improve fallback)
4. Add a "simple" intent type for trivial questions that bypasses task creation

## Classification
[ ] Harness bug  [ ] Model quality issue  [x] Communication issue  [x] Efficiency issue  [x] Design gap  [ ] Both
