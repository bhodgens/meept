# CLI Chat Command Silently Swallows Error Responses
**Date**: 2026-05-15
**Phase**: 2
**Severity**: high
**Component**: `cmd/meept/chat.go`, `internal/tui/rpc.go`
**Evaluation Dimension**: robustness, helpfulness

## Description
When the daemon returns a chat response with an error (non-empty `error` field), the CLI `chat` command only prints the `reply` field and exits with code 0. If the reply is empty (common in error cases), the user sees no output at all and has no indication that something went wrong.

## Reproduction
1. Send a chat message that will fail (e.g., when budget is exceeded):
   ```
   ~/git/meept/bin/meept chat "hello"
   ```
2. Observe: no output, exit code 0
3. Direct RPC shows the actual response:
   ```json
   {"conversation_id":"test-9","error":"agent execution failed: LLM call failed: Token budget exceeded - request blocked","reply":""}
   ```

## Evidence
Source code in `cmd/meept/chat.go` (line 75-80):
```go
reply, err := client.Chat(message, conversationID)
if err != nil {
    return fmt.Errorf("chat error: %w", err)
}
fmt.Println(reply)
```

And in `internal/tui/rpc.go` (line 277-284):
```go
var resp struct {
    Reply string `json:"reply"`
}
if err := json.Unmarshal(result, &resp); err != nil {
    return "", fmt.Errorf("failed to parse chat response: %w", err)
}
return resp.Reply, nil
```

The RPC `Chat()` method only unmarshals `reply` from the response, completely ignoring the `error` field. The CLI then prints an empty string.

## Root Cause
The `ChatResponse` struct in the agent handler has both `Reply` and `Error` fields, but the client-side RPC adapter only reads `Reply`. There's no check for the error field in the response payload.

## Impact
- Users see no output and no error for failed chat messages
- Exit code 0 suggests success
- Makes debugging impossible without direct RPC inspection
- Violates the principle of visible failures

## Proposed Fix
1. In `internal/tui/rpc.go`, unmarshal both `reply` and `error` from the response:
   ```go
   var resp struct {
       Reply string `json:"reply"`
       Error string `json:"error"`
   }
   if resp.Error != "" {
       return resp.Reply, fmt.Errorf("%s", resp.Error)
   }
   return resp.Reply, nil
   ```
2. In `cmd/meept/chat.go`, if `reply` is non-empty AND `err` is non-nil, print both:
   ```go
   if reply != "" {
       fmt.Println(reply)
   }
   return fmt.Errorf("chat error: %w", err)
   ```

## Classification
- Critical for user experience and debuggability
- Simple fix in client-side response parsing
- Affects all error responses from the daemon
