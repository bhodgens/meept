# Daemon Instability Under Concurrent Agent Load
**Date**: 2026-05-15
**Phase**: 1
**Severity**: high
**Component**: internal/daemon/daemon.go, internal/rpc/server.go
**Evaluation Dimension**: robustness

## Description
The daemon is highly unstable when multiple agents interact with it concurrently. During testing, the daemon was repeatedly killed and restarted by another concurrent test agent, causing:
- Active LLM requests being context-canceled
- RPC socket disappearing mid-request
- Client "not connected" errors
- Worker state transition warnings

The daemon lacks protection against concurrent management operations (stop/restart) and does not implement any form of locking or graceful handoff.

## Reproduction
Run two test agents simultaneously against the same daemon. One agent runs `meept chat` commands while the other runs `meept daemon stop && meept-daemon -f`.

## Evidence
Daemon logs showing context cancellation during active requests:
```
time=2026-05-16T00:23:37.409-06:00 level=ERROR msg="LLM call failed" agent=chat iteration=1 error="request failed: Post \"https://api.z.ai/api/coding/paas/v4/chat/completions\": context canceled"
time=2026-05-16T00:23:37.409-06:00 level=ERROR msg="Reasoning cycle failed" agent=chat conversation=cli-51625 error="LLM call failed: request failed: Post \"https://api.z.ai/api/coding/paas/v4/chat/completions\": context canceled"
time=2026-05-16T00:23:37.409-06:00 level=WARN msg="Invalid state transition" worker=worker-1778912604294550000 from=idle to=stopped
```

Client error:
```
Error: chat error: call failed after 4 attempts: not connected to daemon
```

Also observed: the daemon auto-restarts with a different binary (`~/go/bin/meept-daemon` vs `~/git/meept/bin/meept-daemon`), causing configuration and capability mismatches.

## Root Cause
1. No PID file locking to prevent concurrent daemon instances
2. The `meept daemon stop` command sends SIGTERM without checking for active requests
3. Auto-restart mechanism picks up the wrong binary from `$PATH` or `$GOBIN`
4. Worker state machine does not handle shutdown transitions cleanly ("Invalid state transition" warnings)
5. RPC server shutdown timeout (10s) may not be sufficient for long-running LLM requests

## Impact on Platform Quality
- Testing is severely impacted by daemon instability
- In production, any management operation would disrupt active users
- The wrong binary restarting means configuration and features may be different from what was expected
- Worker state corruption from invalid transitions

## Proposed Fix
1. Add PID file locking with `flock(2)` to prevent concurrent instances
2. Implement graceful shutdown that waits for active requests to complete
3. Use a fixed path for the daemon binary rather than relying on `$PATH`
4. Fix worker state transitions to handle `idle -> stopped` as valid
5. Add a daemon health check that verifies the running binary matches the expected version
6. Consider adding a management API that rejects stop/restart while requests are active

## Classification
[ ] Harness bug  [ ] Model quality issue  [ ] Communication issue  [ ] Efficiency issue  [x] Design gap  [ ] Both
