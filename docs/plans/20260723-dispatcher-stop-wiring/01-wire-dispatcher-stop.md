# DISPATCH INSTRUCTION

**Parent**: `20260723-dispatcher-stop-wiring/master.md`  
**Scope**: Wire Dispatcher.Stop() into daemon shutdown  
**Dependencies**: None  
**Estimated Context**: ~25K  
**Files to touch**: `internal/daemon/components.go`

## Goal

Add a call to `dispatcher.Stop()` in the `Components.Stop()` method to ensure the BuildIndex goroutine is cleanly stopped during daemon shutdown.

## Tasks

### Task 1: Read Components.Stop() method

Read `internal/daemon/components.go` and find the `Stop()` method on the `Components` struct. Note what components are already being stopped and in what order.

### Task 2: Identify dispatcher field

Find where the dispatcher is stored in the Components struct. It's likely `c.dispatcher` or similar.

### Task 3: Add Stop() call

In the `Stop()` method, add:

```go
if c.dispatcher != nil {
    c.logger.Info("stopping dispatcher")
    if err := c.dispatcher.Stop(); err != nil {
        c.logger.Error("failed to stop dispatcher", "error", err)
    }
}
```

Place it in logical shutdown order (typically early, before components that depend on the dispatcher).

### Task 4: Verify Stop() signature

Check `internal/agent/dispatcher.go` to confirm `Stop()` returns `(error)` or just `()`. Adjust the call accordingly.

### Task 5: Verify

Run:
```bash
go build ./internal/daemon/...
```

Ensure compilation succeeds.

## Self-Verification Checklist

- [ ] Read Components.Stop() method
- [ ] Identified dispatcher field
- [ ] Added Stop() call with error handling
- [ ] Placed in logical shutdown order
- [ ] Verified Stop() signature
- [ ] `go build ./internal/daemon/...` succeeds
- [ ] No debug artifacts

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
