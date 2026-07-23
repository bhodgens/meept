# DISPATCH INSTRUCTION

**Parent**: `20260723-panic-replacement/master.md`  
**Scope**: Replace panic in agent loop sessionID check  
**Dependencies**: None  
**Estimated Context**: ~50K (read loop.go, trace callers, write changes, run tests)  
**Files to touch**: `internal/agent/loop.go`, potentially caller sites

## Goal

Replace the `panic()` at `internal/agent/loop.go:1408` with a proper error return. This panic fires when a required sessionID is missing — instead of crashing, return a structured error.

## Tasks

### Task 1: Read and understand context

Read `internal/agent/loop.go` around line 1408 (±20 lines) to see:
- What function contains the panic
- What condition triggers it
- What the function returns currently
- How callers use this function

Use `read_file` with offset/limit to get the exact context.

### Task 2: Identify the function signature

Determine if the function already returns an error. If it returns `(result, error)`, we can wrap the panic condition. If it returns only a value, we need to change the signature.

### Task 3: Define sentinel error

At package level (check existing error definitions in the file), add:

```go
var ErrSessionIDRequired = errors.New("agent: sessionID is required but was empty")
```

Or use an existing error if one matches semantically.

### Task 4: Replace panic with error return

Change the panic site from:

```go
if sessionID == "" {
    panic("sessionID is required")
}
```

To:

```go
if sessionID == "" {
    return zeroValue, ErrSessionIDRequired
}
```

Where `zeroValue` is the appropriate zero value for the return type.

### Task 5: Trace and update callers

Use `search_files` with pattern matching to find all callers of this function. For each caller:
- Check if they already handle errors
- If not, add error handling
- Ensure errors propagate up appropriately

Common patterns:
```go
result, err := someFunction(...)
if err != nil {
    return fmt.Errorf("context: %w", err)
}
```

### Task 6: Add test case

If there's a test file (`loop_test.go`), add a test for the empty sessionID case:

```go
func TestLoopRequiresSessionID(t *testing.T) {
    // Setup minimal loop/config
    // Call function with empty sessionID
    // Assert error is ErrSessionIDRequired
}
```

If no test file exists, note this as a gap for future work.

### Task 7: Verify

Run:
```bash
go build ./internal/agent/...
go test ./internal/agent/...
```

Ensure compilation succeeds and tests pass.

## Interface Contract

This leaf exposes:
- **New error**: `agent.ErrSessionIDRequired` (or reuse existing) — returned when sessionID is empty
- **Behavior change**: Function returns error instead of panicking

## Dependencies

None — this is an isolated change within the agent package.

## Self-Verification Checklist

- [ ] Read the full function context (line 1408 ±20)
- [ ] Identified function signature and return types
- [ ] Defined or reused sentinel error
- [ ] Replaced panic with error return
- [ ] Traced all callers (use search_files)
- [ ] Updated callers to handle error
- [ ] Added test case (if test file exists)
- [ ] `go build ./internal/agent/...` succeeds
- [ ] `go test ./internal/agent/...` passes
- [ ] No debug artifacts left

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
