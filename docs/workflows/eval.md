# Evaluation (Eval)

The `internal/eval` package provides trace analysis evaluation — classification benchmarks, failure mode analysis, and conversational analysis sessions for interactive trace review.

---

## Overview

Meept's HALO agent (Recursive LLM trace analysis) autonomously inspects production traces to identify failure modes. The eval package supports this workflow with three capabilities:

1. **LLM-based intent classification** with benchmark runner and metrics collection
2. **Failure mode detection** via recursive LLM analysis over trace data
3. **Interactive analysis sessions** for multi-turn human-in-the-loop review of analysis results

---

## Analysis Sessions

Analysis sessions enable a user to have an ongoing conversation about trace analysis results, with follow-up questions and iterative deepening into specific failure modes.

### Problem

After an initial trace analysis run, operators need a way to iteratively explore findings — asking follow-up questions, drilling into specific failure modes, and building an audit trail of the investigation. Previously this required external chat tools. Analysis sessions provide first-class session management for this workflow.

### Behavior

A `AnalysisSession` has a lifecycle: **active** (accepting turns) → **paused** → **completed**. Each turn records a user query, analyst response, and references to trace/span IDs. The session automatically generates follow-up suggestions based on the analysis severity and response content.

Sessions can be exported as structured JSON (with human-readable indentation) for audit trails.

```go
mgr := NewAnalysisSessionManager(basePath)
session := mgr.CreateSession(traceIDs, failureModes)
session.AddTurn("What traces are affected?", "3 traces show the issue.", nil, nil)
session.Close()
data, _ := session.ExportJSON()
```

### State Machine

```
active ──Pause()──> paused ──Resume()──> active
  │                        │
  │  Close()               │  Close()
  └────────────────────────┴──> completed (read-only)
```

### Session Manager

`AnalysisSessionManager` handles session lifecycle in-process with thread-safe access (mutex-protected map). Sessions persist in memory; `ExportSession()` writes to disk.

### Edge Cases

- Adding turns after `Close()` returns `nil` — caller must check
- `Resume()` and `Pause()` are no-ops on incompatible states (no panic)
- `GetFollowUpSuggestions()` returns `nil` for sessions with no turns
- Follow-up suggestions are generated at turn-commit time, not lazily
