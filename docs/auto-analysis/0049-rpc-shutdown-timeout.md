# RPC Server Shutdown Timeout (10s) on Daemon Stop
**Date**: 2026-05-15
**Phase**: 1
**Severity**: low
**Component**: internal/rpc/server.go
**Evaluation Dimension**: robustness

## Description
The RPC server shutdown times out at 10 seconds when the daemon is stopped while requests are in progress. The timeout message is logged but does not prevent the daemon from eventually stopping.

## Reproduction
Send a chat request (which takes ~10-15s for LLM response), then stop the daemon during the request.

## Evidence
```
time=2026-05-16T00:23:37.411-06:00 level=INFO msg="registry: stopping component" name=rpc.server
time=2026-05-16T00:23:47.409-06:00 level=WARN msg="rpc: shutdown timed out"
time=2026-05-16T00:23:47.409-06:00 level=INFO msg="rpc: server stopped"
```

The 10-second gap between "stopping" and "timed out" confirms the shutdown waited for the full timeout period.

## Root Cause
The RPC server uses a fixed 10-second shutdown timeout that may be insufficient for long-running LLM requests (which can take 20-30+ seconds).

## Impact on Platform Quality
- Daemon shutdown takes at least 10 seconds when requests are active
- Active requests are forcefully terminated rather than completing gracefully
- No user-visible impact but degrades operational experience

## Proposed Fix
1. Make the shutdown timeout configurable
2. Track active request count and wait for requests to drain before shutting down
3. Use a context deadline based on the longest active request

## Applied Fix

- Default shutdown timeout increased from 10s to 30s (`DefaultShutdownTimeout`)
- Added `Config.Shutdown` field (time.Duration) to allow operator configuration
- Added `activeReqs atomic.Int64` to track in-flight RPC requests
- `dispatch()` increments/decrements `activeReqs` around the handler call via defer
- `Stop()` now waits for `activeReqs` to drain with configurable timeout
- Log message on timeout now reports the number of remaining active requests
- Added `Config.ShutdownNotify` callback for external notification

## Classification
[ ] Harness bug  [ ] Model quality issue  [ ] Communication issue  [ ] Efficiency issue  [x] Design gap  [ ] Both
