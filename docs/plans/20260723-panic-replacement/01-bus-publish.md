# DISPATCH INSTRUCTION

**Parent**: `20260723-panic-replacement/master.md`  
**Scope**: Replace panic in bus.Publish with error return  
**Dependencies**: None  
**Estimated Context**: ~40K (read bus.go, write changes, run tests)  
**Files to touch**: `internal/bus/bus.go`, `internal/bus/bus_test.go`

## Goal

Replace the `panic()` call at `internal/bus/bus.go:178` with a proper error return. The code already has a warning log path — we need to convert the panic into an error that callers can handle.

## Tasks

### Task 1: Read and understand current behavior

Read `internal/bus/bus.go` lines 160-190 to see the full Publish method context.

Note:
- Line 171-176: Warning is already logged when no subscribers
- Line 177-179: Panic only fires if `panicOnUndrainedSubscription` flag is set
- This is a debug/test flag, not production behavior by default

### Task 2: Define sentinel error

At package level (around line 20), add:

```go
var ErrNoSubscribers = errors.New("bus: publish with no subscribers")
```

Import `"errors"` if not already imported.

### Task 3: Replace panic with error return

Change lines 177-180 from:

```go
if panicOnUndrainedSubscription {
    panic(fmt.Sprintf("bus: Publish(%q) with no subscribers", topic))
}
return 0
```

To:

```go
if panicOnUndrainedSubscription {
    b.logger.Error("bus: Publish with no subscribers (panic mode enabled)",
        "topic", topic,
        "source", msg.Source,
        "msg_id", msg.ID,
    )
    return 0, ErrNoSubscribers
}
return 0, ErrNoSubscribers
```

### Task 4: Update function signature

The `Publish` method currently returns `(int, error)` — verify this is correct. If it returns just `int`, change signature to `(int, error)`.

Check all callers of `Publish` to ensure they handle the error. Use `search_files` for pattern `\.Publish\(` in Go files.

### Task 5: Add test case

In `internal/bus/bus_test.go`, add a test:

```go
func TestBusPublishNoSubscribers(t *testing.T) {
    b := NewBus()
    msg := Message{Source: "test", ID: "msg-1"}
    
    count, err := b.Publish(context.Background(), "nonexistent-topic", msg)
    
    if err != ErrNoSubscribers {
        t.Errorf("expected ErrNoSubscribers, got %v", err)
    }
    if count != 0 {
        t.Errorf("expected 0 subscribers, got %d", count)
    }
}
```

### Task 6: Verify

Run:
```bash
go build ./internal/bus/...
go test ./internal/bus/...
```

Ensure all tests pass.

## Interface Contract

This leaf exposes:
- **New error**: `bus.ErrNoSubscribers` — returned when Publish has no subscribers
- **Behavior change**: `Publish` now always returns error when no subscribers (previously only panicked in debug mode)

## Self-Verification Checklist

- [ ] Sentinel error defined at package level
- [ ] Panic replaced with error return
- [ ] Warning log preserved (upgraded to Error in panic mode)
- [ ] Function signature updated if needed
- [ ] All callers handle new error
- [ ] Test added for no-subscriber case
- [ ] `go build ./internal/bus/...` succeeds
- [ ] `go test ./internal/bus/...` passes
- [ ] No debug artifacts left (fmt.Println, TODOs)

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
