# Worker Invalid State Transitions on Shutdown
**Date**: 2026-05-15
**Phase**: 1
**Severity**: low
**Component**: internal/agent/handler.go, internal/queue/dispatcher.go
**Evaluation Dimension**: robustness

## Description
Worker state transitions from "idle" to "stopped" and "error" to "stopped" are flagged as invalid during daemon shutdown. These transitions are expected during graceful shutdown and should be allowed.

## Reproduction
Stop the daemon while workers are in idle or error state.

## Evidence
From daemon logs:
```
time=2026-05-16T00:23:37.409-06:00 level=WARN msg="Invalid state transition" worker=worker-1778912604294550000 from=idle to=stopped
time=2026-05-16T00:23:37.409-06:00 level=WARN msg="Invalid state transition" worker=worker-1778912604294540000 from=idle to=stopped
time=2026-05-16T00:23:37.409-06:00 level=WARN msg="Invalid state transition" worker=worker-1778912604294522000 from=idle to=stopped
time=2026-05-16T00:23:37.409-06:00 level=WARN msg="Invalid state transition" worker=worker-1778912604294545000 from=idle to=stopped
```

Also:
```
time=2026-05-16T00:21:45.090-06:00 level=WARN msg="Invalid state transition" worker=worker-1778912458788464000 from=error to=stopped
```

## Root Cause
The worker state machine does not include `stopped` as a valid target from `idle` or `error` states. During shutdown, all workers are transitioned to `stopped` regardless of their current state.

## Impact on Platform Quality
- Log noise during normal shutdown
- May mask real state transition bugs
- Does not affect functionality

## Proposed Fix
Add `idle -> stopped` and `error -> stopped` as valid transitions in the worker state machine.

## Applied Fix

Added `StateStopped` to the valid targets for `StateIdle` and `StateError` in `internal/worker/state.go`:
- `StateIdle: {StateClaiming, StateStopping, StateStopped}` (added StateStopped)
- `StateError: {StateIdle, StateStopping, StateStopped}` (added StateStopped)

## Classification
[x] Harness bug  [ ] Model quality issue  [ ] Communication issue  [ ] Efficiency issue  [ ] Design gap  [ ] Both
