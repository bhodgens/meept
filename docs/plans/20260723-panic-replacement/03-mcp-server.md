# DISPATCH INSTRUCTION

**Parent**: `20260723-panic-replacement/master.md`  
**Scope**: Replace panic in MCP server json.Marshal  
**Dependencies**: None  
**Estimated Context**: ~35K (read server.go, write changes, run tests)  
**Files to touch**: `internal/mcp/server.go`

## Goal

Replace the `panic()` at `internal/mcp/server.go:317` that fires on `json.Marshal` failure. JSON marshaling of valid Go structs should never fail — if it does, it indicates a programming error. However, panicking crashes the daemon. Replace with error logging and graceful degradation.

## Tasks

### Task 1: Read and understand context

Read `internal/mcp/server.go` around line 317 (±15 lines) to see:
- What is being marshaled
- Why it might fail (circular refs? unsupported types?)
- What the function does with the marshaled data
- What happens after the marshal

### Task 2: Analyze failure modes

`json.Marshal` fails only for:
- Circular references
- Unsupported types (channels, functions, complex numbers)
- Invalid map keys (non-string)

If the data being marshaled is well-structured, this should never fail. But we must handle it gracefully.

### Task 3: Replace panic with error handling

Change from:

```go
data, err := json.Marshal(something)
if err != nil {
    panic(err)
}
```

To:

```go
data, err := json.Marshal(something)
if err != nil {
    logger.Error("mcp: failed to marshal response", "error", err)
    // Return error response or skip this operation
    return fmt.Errorf("marshal failed: %w", err)
}
```

Choose the appropriate action based on what the function does:
- If it's sending an HTTP/WebSocket response → send error response
- If it's internal processing → return error to caller
- If it's logging/debug → skip and continue

### Task 4: Preserve functionality

Ensure the replacement doesn't break the intended behavior:
- If the marshal result is sent to a client, send an error message instead
- If it's used internally, propagate the error up
- Log the error with enough context to debug

### Task 5: Add defensive check (optional)

If the marshal target could have circular refs, consider adding a pre-check or using `json.Marshal` with a custom encoder that detects cycles.

### Task 6: Verify

Run:
```bash
go build ./internal/mcp/...
go test ./internal/mcp/...
```

Ensure compilation succeeds and tests pass.

## Interface Contract

This leaf exposes:
- **Behavior change**: Marshal failures now return errors instead of panicking
- **Logging**: Errors are logged with context before returning

## Self-Verification Checklist

- [ ] Read full context around line 317
- [ ] Understood why marshal might fail
- [ ] Determined appropriate error handling strategy
- [ ] Replaced panic with error return + logging
- [ ] Preserved intended functionality (error response or propagation)
- [ ] `go build ./internal/mcp/...` succeeds
- [ ] `go test ./internal/mcp/...` passes
- [ ] No debug artifacts left

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
