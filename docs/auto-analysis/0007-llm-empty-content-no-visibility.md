# LLM Returns Empty Content - No Response Body Logging

**Date**: 2026-05-15
**Phase**: 2 (core agent loop & LLM integration)
**Severity**: high
**Component**: `internal/llm/client.go`

## Description

The z.ai API (glm-4.7) returns HTTP 200 with valid JSON, but the agent loop reports "LLM returned empty content" on every iteration. After 3 empty responses, the convergence detector triggers and the agent loop fails.

The LLM client logs the HTTP status and content type but does NOT log the response body at any level (debug or otherwise), making it impossible to diagnose why the content is empty without modifying the code.

## Reproduction

1. Configure `zai/glm-4.7` as the default model
2. Start daemon with `--debug`
3. Send any chat message
4. Observe: HTTP 200 received, but `Content` is empty/null
5. Agent retries 3 times with nudges, then convergence detector kills the loop

## Evidence

```
level=DEBUG msg="Making LLM request" url=https://api.z.ai/api/coding/paas/v4/chat/completions model=glm-4.7
level=DEBUG msg="LLM response received" status=200 content_type="application/json; charset=UTF-8"
level=WARN msg="LLM returned empty content, nudging for more information" iteration=1
... (repeats 3 times)
level=WARN msg="Convergence detected, aborting loop"
level=ERROR msg="Agent loop failed" error="agent responses converged without progress"
```

The `parseResponse()` method checks `msg.Content` (type `*string`), which is nil. This means the API response's `choices[0].message.content` is either `null` or absent.

## Root Cause

Two issues:

1. **Missing diagnostic logging**: `client.go:555` only logs status and content type. The raw response body should be logged at debug level (truncated to 500 chars) to enable diagnosis.

2. **Possible API response format mismatch**: The z.ai API may return content in a non-standard format (e.g., as an array of content blocks like `[{type: "text", text: "..."}]` instead of a plain string). The `ResponseMessage.Content` field is `*string`, so if the API returns content as an array, JSON unmarshaling would leave it as nil.

## Proposed Fix

1. Add response body preview to debug logging after `LLM response received`:
   ```go
   preview := string(respBody)
   if len(preview) > 500 {
       preview = preview[:500] + "..."
   }
   c.logger.Debug("LLM response body", "body_preview", preview)
   ```

2. Handle OpenAI-style content array format in `ResponseMessage`:
   ```go
   Content json.RawMessage `json:"content"` // Parse both string and array formats
   ```
   Then in `parseResponse`, try both `string` and `[{type, text}]` formats.

3. If content is nil/empty but tool_calls are present, that's valid — don't warn.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
