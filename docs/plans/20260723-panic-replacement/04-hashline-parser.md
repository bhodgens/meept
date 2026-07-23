# DISPATCH INSTRUCTION

**Parent**: `20260723-panic-replacement/master.md`  
**Scope**: Replace panic in hashline parser odd arg count  
**Dependencies**: None  
**Estimated Context**: ~30K (read hashline_parser.go, write changes, run tests)  
**Files to touch**: `internal/tools/builtin/hashline_parser.go`

## Goal

Replace the `panic()` at `internal/tools/builtin/hashline_parser.go:168` that fires when key-value args have odd count. This is a user input validation issue — return a parse error instead of crashing.

## Tasks

### Task 1: Read and understand context

Read `internal/tools/builtin/hashline_parser.go` around line 168 (±20 lines) to see:
- What function contains the panic
- What input causes odd arg count
- What the function returns currently
- How callers use this function

### Task 2: Understand the parsing logic

The hashline parser likely expects key-value pairs like `key1=val1 key2=val2`. An odd count means something like `key1=val1 orphan_key` — a key without a value.

### Task 3: Define sentinel error

At package level, add:

```go
var ErrOddArgCount = errors.New("hashline: odd number of arguments, expected key=value pairs")
```

### Task 4: Replace panic with error return

Change from:

```go
if len(args)%2 != 0 {
    panic("odd number of arguments")
}
```

To:

```go
if len(args)%2 != 0 {
    return nil, ErrOddArgCount
}
```

Adjust based on actual return types.

### Task 5: Improve error message

Include the actual args in the error for debugging:

```go
if len(args)%2 != 0 {
    return nil, fmt.Errorf("%w: got %d args (%v)", ErrOddArgCount, len(args), args)
}
```

### Task 6: Trace callers

Use `search_files` to find callers of this parser function. Ensure they handle the error appropriately. Most tool parsers should propagate errors up to the tool execution layer.

### Task 7: Add test case

In `hashline_parser_test.go` (create if needed), add:

```go
func TestParseOddArgCount(t *testing.T) {
    args := []string{"key1=val1", "orphan"}
    _, err := ParseHashline(args)
    if !errors.Is(err, ErrOddArgCount) {
        t.Errorf("expected ErrOddArgCount, got %v", err)
    }
}
```

### Task 8: Verify

Run:
```bash
go build ./internal/tools/builtin/...
go test ./internal/tools/builtin/...
```

## Interface Contract

This leaf exposes:
- **New error**: `tools/builtin.ErrOddArgCount` — returned when key-value args have odd count
- **Behavior change**: Parser returns error instead of panicking on malformed input

## Self-Verification Checklist

- [ ] Read full context around line 168
- [ ] Understood parsing logic and failure mode
- [ ] Defined sentinel error
- [ ] Replaced panic with error return
- [ ] Improved error message with context
- [ ] Traced callers to ensure error handling
- [ ] Added test case for odd arg count
- [ ] `go build ./internal/tools/builtin/...` succeeds
- [ ] `go test ./internal/tools/builtin/...` passes
- [ ] No debug artifacts left

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
