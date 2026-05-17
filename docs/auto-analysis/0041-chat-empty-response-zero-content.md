# Chat Command Returns Empty Response with Zero-Length Content
**Date**: 2026-05-15
**Phase**: 1
**Severity**: high
**Component**: internal/agent/handler.go, internal/agent/dispatcher.go
**Evaluation Dimension**: correctness, communication

## Description
The `meept chat "message"` command frequently returns an empty string as the reply. The CLI exits with code 0 and prints only a newline. The response time is typically 10-27 seconds (matching an LLM call duration), suggesting the LLM returns content but it is lost somewhere in the pipeline.

## Reproduction
```bash
~/git/meept/bin/meept chat "my name is alice"
# Expected: some acknowledgment text
# Actual: (empty line, exit 0, ~10s elapsed)
```

Observed in multiple test runs:
- `meept chat "Tell me a joke"` -- empty, 23ms (instant, suspiciously fast)
- `meept chat "my name is alice"` -- empty, 17s
- `meept chat "bonjour, comment ca va?"` -- empty, 27s
- `meept chat "... --- ..."` -- empty, 23ms

## Evidence
```
=== TEST 3: Session continuity ===
time ~/git/meept/bin/meept chat "my name is alice" 2>&1

real	0m17.138s
user	0m0.011s
sys	0m0.005s
---EXIT:0---
```

Daemon logs show the dispatcher classifying correctly:
```
WARN msg="LLM classifier failed, trying keyword" component=dispatcher error="request failed: Post \"http://127.0.0.1:8080/v1/chat/completions\": dial tcp 127.0.0.1:8080: connect: connection refused"
INFO msg="Dispatched request" component=dispatcher agent=chat intent_type=chat confidence=0.6 memory_refs=0 has_task=false
INFO msg="Created agent loop" id=chat name="Chat Assistant"
```

But then the agent loop fails silently or returns empty content.

## Root Cause
Multiple potential causes:
1. The local LLM classifier at 127.0.0.1:8080 is down, causing fallback to keyword classifier with low confidence (0.3-0.6). The dispatcher routes to chat agent, but the response is lost.
2. The `StripReport()` function may return empty string if the LLM output is entirely consumed as a "report" structure.
3. When the daemon is killed mid-request (by concurrent agents), the context cancellation returns an error, but the RPC proxy returns empty reply instead of the error message.
4. The RPC `chat.response` may contain `{"reply": ""}` when the agent loop fails -- the handler sets `reply` to empty on error but still publishes a response.

## Impact on Platform Quality
Users see no output and no error. This breaks the fundamental chat interaction pattern and makes the CLI unreliable. Exit code 0 suggests success, but no content was delivered.

## Proposed Fix
1. When the agent loop returns an error, the chat handler should include the error message in the response rather than sending an empty reply.
2. Add a non-empty check: if `reply` is empty after processing, include a fallback message like "No response was generated."
3. Fix the local LLM classifier dependency -- if 127.0.0.1:8080 is unavailable, degrade gracefully without logging errors on every request.

## Classification
[x] Harness bug  [ ] Model quality issue  [ ] Communication issue  [ ] Efficiency issue  [ ] Design gap  [ ] Both
