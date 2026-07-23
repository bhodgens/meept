# DISPATCH INSTRUCTION

**Parent**: `20260723-panic-replacement/master.md`  
**Scope**: Investigate and fix unconditional panic in session store  
**Dependencies**: None  
**Estimated Context**: ~45K (read session.go, investigate context, write changes, run tests)  
**Files to touch**: `internal/session/session.go`

## Goal

Investigate and fix the unconditional `panic()` at `internal/session/session.go:1921`. Unlike the other panics (which have conditions), this one appears unconditional. We need to understand why it exists and replace it with appropriate error handling or remove it if it's unreachable.

## Tasks

### Task 1: Read and understand context

Read `internal/session/session.go` around line 1921 (±30 lines) to see:
- What function contains the panic
- Is it truly unconditional, or is there a guard we're missing?
- What is the function supposed to do?
- What would trigger this code path?

### Task 2: Determine if panic is reachable

Trace back through the function:
- Are there early returns that prevent reaching line 1921?
- Is this in a `default:` case of a switch that should be exhaustive?
- Is this defensive programming for "should never happen" cases?

If the panic is truly unreachable (e.g., in a `default` case where all enum values are handled), document this and consider replacing with a comment explaining why it's unreachable.

### Task 3: If reachable, determine appropriate handling

If the code path IS reachable:
- What condition triggers it?
- Should it return an error?
- Should it log and continue?
- Should it return a default/safe value?

### Task 4: Implement fix

Based on analysis:

**Option A: Unreachable defensive panic**
```go
// This should never be reached because all enum values are handled above.
// If we get here, it indicates a programming error (new enum value added without handler).
panic("unreachable: unhandled case in switch")
```
→ Replace with:
```go
// Unreachable: all enum values handled above. If this fires, a new enum value
// was added without a corresponding case. See <link to enum definition>.
return fmt.Errorf("internal error: unhandled case (programming error)")
```

**Option B: Reachable error condition**
```go
if someCondition {
    panic("bad state")
}
```
→ Replace with:
```go
if someCondition {
    return fmt.Errorf("session: invalid state: %w", ErrInvalidState)
}
```

**Option C: Remove if truly dead code**
If static analysis shows the code is unreachable, remove the entire block.

### Task 5: Define sentinel error if needed

If returning an error, define at package level:

```go
var ErrInvalidState = errors.New("session: invalid internal state")
```

### Task 6: Trace callers

Use `search_files` to find callers of the function containing the panic. Ensure they handle the new error appropriately.

### Task 7: Add test case if reachable

If the error condition is reachable, add a test that triggers it and verifies the error is returned correctly.

### Task 8: Verify

Run:
```bash
go build ./internal/session/...
go test ./internal/session/...
```

## Interface Contract

This leaf exposes:
- **Potential new error**: `session.ErrInvalidState` (or similar) — if the panic was reachable
- **Behavior change**: Either returns error, logs and continues, or removes dead code

## Investigation Notes

**Critical**: This panic may be fundamentally different from the others. The architectural review noted it as "unconditional panic" — which suggests either:
1. It's in a `default` case meant to catch programming errors
2. It's truly unreachable dead code
3. It's a bug (should have a condition)

Spend time understanding the intent before changing it. If uncertain, ask the orchestrator for guidance.

## Self-Verification Checklist

- [ ] Read full context around line 1921 (±30 lines)
- [ ] Determined if panic is reachable or unreachable
- [ ] Understood the function's purpose and contract
- [ ] Chose appropriate fix (error return, log+continue, or removal)
- [ ] Implemented fix with proper error handling
- [ ] Defined sentinel error if needed
- [ ] Traced callers to ensure error handling
- [ ] Added test case if error condition is reachable
- [ ] `go build ./internal/session/...` succeeds
- [ ] `go test ./internal/session/...` passes
- [ ] No debug artifacts left

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
