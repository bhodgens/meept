# Token Counting Ignores ToolCalls and ToolCallID Fields

**Date**: 2026-05-15
**Phase**: 10 (context firewall and pressure management)
**Severity**: medium
**Component**: `internal/llm/context_firewall.go` (countTokens), `internal/llm/context_compressor.go` (countTokens), `internal/llm/context_compactor.go` (countTokens)

## Description

All three token counting implementations in the context management system count only `msg.Content` and ignore the `ToolCalls` and `ToolCallID` fields on `ChatMessage`. In multi-turn agent conversations that involve tool use, tool call function names, arguments, and tool results contribute significant token volume that is invisible to the firewall.

This causes the context firewall to systematically underestimate actual token usage, which means:
- Utilization ratios are reported lower than reality
- Context reduction triggers fire too late (or not at all)
- `ValidateContextSize` may pass validation when the actual payload is over the model limit
- The API provider may truncate or reject the request

## Reproduction

1. Inspect `countTokens` in `context_firewall.go` line 733:
   ```go
   func (f *ContextFirewall) countTokens(messages []ChatMessage) int {
       total := 0
       for _, msg := range messages {
           total += f.tokenizer.CountTokens(msg.Content)
       }
       return total
   }
   ```
2. Same pattern in `context_compressor.go` line 296 and `context_compactor.go` line 466.
3. None of these count `msg.ToolCalls[i].Function.Name` or `msg.ToolCalls[i].Function.Arguments` or `msg.ToolCallID`.
4. A message with 10 tool calls each having 500 tokens of arguments would report only the `Content` token count, potentially missing thousands of tokens.

## Evidence

Three identical `countTokens` implementations all iterate `msg.Content` only:
- `internal/llm/context_firewall.go:733-738`
- `internal/llm/context_compressor.go:296-301`
- `internal/llm/context_compactor.go:466-469`

The `ChatMessage` struct at `internal/llm/models.go:21-35` has:
```go
type ChatMessage struct {
    Role       string     `json:"role"`
    Content    string     `json:"content"`
    ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    ...
}
```

The `ToOpenAIDict()` method serializes both `Content` and `ToolCalls`, confirming both are sent to the API and consume tokens.

Daemon log shows context utilization reported as ~5.5% for agent conversations. In conversations with tool use (coder, debugger agents), the actual token usage could be significantly higher.

## Root Cause

The token counting was implemented to count only the `Content` field, likely because that is the primary text field. ToolCalls were not considered because they are optional and structurally separate. However, in tool-intensive conversations (which are the primary use case for coder and debugger agents), tool calls can represent a substantial portion of total tokens.

## Proposed Fix

Unify the three `countTokens` implementations into a shared function that accounts for all message fields:

```go
func countMessageTokens(msg ChatMessage, tokenizer Tokenizer) int {
    total := tokenizer.CountTokens(msg.Content)
    for _, tc := range msg.ToolCalls {
        total += tokenizer.CountTokens(tc.Function.Name)
        total += tokenizer.CountTokens(tc.Function.Arguments)
    }
    if msg.ToolCallID != "" {
        total += tokenizer.CountTokens(msg.ToolCallID)
    }
    if msg.Name != "" {
        total += tokenizer.CountTokens(msg.Name)
    }
    return total
}
```

Then use this in all three places (firewall, compressor, compactor).

## Resolution

The fix was already applied before this issue was documented. All three token
counting implementations in `internal/llm/` now include a `countMessageTokens`
helper that accounts for `Content`, `ToolCalls` (function name + arguments),
`ToolCallID`, and `Name` fields:

- `context_firewall.go` lines 751-763
- `context_compressor.go` lines 307-319
- `context_compactor.go` lines 476-488

Each calls `tokenizer.CountTokens(...)` for tool names, argument strings, tool
call IDs, and the `Name` field. A local build (`go build ./...`) and full test
run (`go test ./internal/llm/... -v`) both pass clean.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
