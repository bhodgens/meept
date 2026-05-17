# Context Firewall Validates Before Reducing -- Blocks Requests That Could Be Saved

**Date**: 2026-05-15
**Phase**: 10 (context firewall and pressure management)
**Severity**: high
**Component**: `internal/llm/context_firewall.go` (Chat, ChatWithProgress, ValidateContextSize)

## Description

The `ContextFirewall.Chat()` method calls `ValidateContextSize()` **before** `processMessages()`. `ValidateContextSize()` returns a `ContextSizeExceededError` when the raw message token count exceeds the model's context limit. This means the firewall rejects the request outright instead of first attempting to reduce the context via compaction, compression, or summarization.

The entire purpose of the context firewall is to handle oversized context by reducing it. The compaction, compression, and summarization pipelines in `processMessages()` are designed for exactly this scenario. But they are never reached because the validation gate runs first and hard-fails.

The error is also marked `NonRetryable`, so the task escalation system treats it as permanent, leading to immediate task death rather than a retry after context reduction.

## Reproduction

1. Have a conversation that builds up context to just below the model limit (e.g., with a model that has `ContextLimit: 8192`)
2. Send one more message that pushes the total over the limit
3. Observe `ValidateContextSize` returns `ContextSizeExceededError` before `processMessages` is ever called
4. The request is rejected; no compaction or summarization is attempted
5. The task is marked as dead (non-retryable)

## Evidence

In `context_firewall.go` lines 411-417:
```go
func (f *ContextFirewall) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
    // Validate context size before processing
    if err := f.ValidateContextSize(messages); err != nil {
        return nil, err
    }
    processed := f.processMessages(ctx, messages)
    ...
}
```

`ValidateContextSize` (line 869) checks `estimated > modelLimit` and returns error. `processMessages` (line 483) contains the entire reduction pipeline (compaction at trigger ratio, proactive compression, hard limit drop, chunking, summarization) but is unreachable when validation fails.

In the daemon logs, there are zero compaction/compression events logged despite the context firewall being enabled for every agent. This is because every request that would trigger reduction is blocked at the validation gate instead.

## Root Cause

`ValidateContextSize` was added as a safety check, but it is positioned before the reduction pipeline rather than after it. The intended flow should be: reduce first, then validate the reduced context. Instead, the current flow is: validate raw context, reject if over limit, never attempt reduction.

## Proposed Fix

Move `ValidateContextSize` to run **after** `processMessages`. The flow should be:
1. `processMessages(ctx, messages)` -- reduce context via compaction, compression, summarization
2. `ValidateContextSize(processed)` -- check if the reduced context fits
3. Only return error if the reduced context still exceeds the limit

Alternatively, remove the pre-validation entirely since `processMessages` already has hard-limit handling with context dropping. The per-stage thresholds in the compressor already prevent context from exceeding the model limit.

## Fix Applied

Moved `ValidateContextSize()` from before to after `processMessages()` in both `Chat()` and `ChatWithProgress()`. The reduction pipeline (compaction, compression, hard-limit context dropping, chunking, summarization) now runs first, and validation only rejects the request if the reduced context still exceeds the model limit.

Diff (unstaged change to `internal/llm/context_firewall.go`):
- `Chat()`: calls `processMessages` first, then `ValidateContextSize(processed)`
- `ChatWithProgress()`: same reordering
- Added explanatory comments documenting the post-reduction validation flow

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both

**Status: FIXED**
