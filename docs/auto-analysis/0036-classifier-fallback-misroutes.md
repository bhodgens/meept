# 0036: LLM Classifier Failure Causes Severe Agent Misrouting

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **High** |
| Component | `internal/agent/dispatcher.go` (keyword fallback) |
| Evaluation Dimension | Correctness, Efficiency |
| Reporter | QA Phase 3 |

## Description

When the local LLM classifier (at 127.0.0.1:8080) is unavailable, the dispatcher falls back to keyword-based classification which routes requests to completely wrong agents. Code generation requests get routed to `scheduler`, `committer`, or `chat` instead of `coder`. Bug investigation requests get routed to `chat` instead of `debugger`.

## Reproduction

1. Ensure local LLM at 127.0.0.1:8080 is not running (classifier fails)
2. Run: `~/git/meept/bin/meept chat "write a Go function that reverses a string"`
3. Observe: Routed to `chat` (confidence 0.6) or `scheduler` instead of `coder`

## Evidence

Daemon log entries from multiple test runs:

```
# Test: "write a Go function that reverses a string"
msg="LLM classifier failed, trying keyword" error="connection refused"
msg="Dispatched request" agent=chat intent_type=chat confidence=0.6

# Test: "create a file at ~/git/meept-playground/buggy-app/handler.go"
msg="Dispatched request" agent=scheduler intent_type=schedule confidence=0.104

# Test: file creation task
msg="Dispatched request" agent=committer intent_type=committer confidence=0.86944

# Test: "fix it" (bug fix request)
msg="Dispatched request" agent=chat intent_type=chat confidence=0.02

# Test: "add authentication to buggy-app"
msg="Dispatched request" agent=committer intent_type=committer confidence=0.94528

# Test: code reading request
msg="Dispatched request" agent=chat intent_type=chat confidence=0.3
```

## Root Cause

The keyword classifier fallback (`internal/agent/dispatcher.go`) has extremely low confidence thresholds and poor keyword matching:

1. "write a Go function" matches `chat` instead of `coder` -- likely because "function" isn't in the coder keywords
2. "create a file" matches `scheduler` or `committer` -- "create" matches schedule/create keywords
3. "fix it" matches nothing well, falls to `chat` with 0.02 confidence
4. "add authentication" matches `committer` -- "add" is probably in committer keywords

The keyword classifier uses a simple bag-of-words approach without semantic understanding. When the LLM classifier is unavailable, the keyword approach has insufficient coverage for code-related intents.

## Impact

- **High**: All code tasks are misrouted when local LLM is down
- `coder` agent is never selected via keyword fallback
- Tasks go to wrong agents, causing wasted tokens and failed operations
- Users see wrong agent behavior (git operations for file creation, scheduling for code tasks)

## Proposed Fix

1. Improve keyword fallback with explicit routing rules for code-related keywords:
   - "write", "create.*function", "implement", "add.*feature" -> coder
   - "fix", "bug", "debug", "crash", "error in" -> debugger
   - "read", "explain", "what does" -> analyst
2. Add minimum confidence threshold - if best match is < 0.3, route to `coder` as default for technical queries
3. Log a warning when keyword fallback produces very low confidence (< 0.2)
4. Consider making classifier robustness a startup check - warn if local LLM is unreachable

## Classification

- Type: Bug (poor fallback logic)
- Regression: No
- Priority: P1 - critical for correct operation without local LLM

## Resolution
**Status: FIXED** (Round 6 - Dispatcher Heuristics)

Replaced the unguarded keyword classifier fallback with a targeted `heuristicFallback()` function that uses explicit routing rules ordered by specificity:
1. **Code keywords** (0.55 confidence -> `coder`): "write a", "create [a] file", "implement [the]", "add a function/feature/method", "build [me]", "generate [a]", "code a"
2. **Code verb + indicator combo** (0.5 confidence -> `coder`): Any code verb (write, create, implement, etc.) paired with code indicators ("function", "method", "class", "struct", "import", etc.)
3. **Debug keywords** (0.55 confidence -> `debugger`): "fix", "bug", "error:", "exception", "crash", "panic", "not working", "broken", "debug", "stack trace"
4. **Git keywords** (0.55 confidence -> `committer`): "commit", "push", "pull", "merge", "branch", "rebase", "revert", "checkout"
5. **Analysis keywords** (0.45 confidence -> `analyst`): "what is", "what does", "explain", "how does", "how to", "compare"

Additionally, the KeywordClassifier now enforces a 0.3 minimum confidence threshold -- matches below this are rejected and deferred to the heuristic fallback.
