# RPC writeTimeout kills long-running selfimprove.cycle

- **Date**: 2026-05-16
- **Phase**: Phase 9 (Self-Improvement System)
- **Severity**: High
- **Component**: rpc + selfimprove

## Description

The `selfimprove full-cycle` command fails with `call failed after 4 attempts: not connected to daemon` when the analysis phase involves LLM calls. The RPC server has a hardcoded 30-second write timeout that closes the connection before the cycle response can be sent.

## Reproduction

```bash
# With selfimprove.enabled=true and ai_infra.enabled=true
meept selfimprove full-cycle
# Result: Error: cycle failed: call failed after 4 attempts: not connected to daemon
```

## Daemon Log Evidence

```
time=2026-05-16T12:50:25.994-06:00 level=INFO msg="starting improvement cycle" component=selfimprove cycle_id=cycle-dd6c7a03
time=2026-05-16T12:50:25.994-06:00 level=INFO msg="phase 1 - detecting issues" component=selfimprove
time=2026-05-16T12:50:26.873-06:00 level=INFO msg="phase 2 - analyzing issues" component=selfimprove count=220
# [no further selfimprove log entries - connection dropped]
time=2026-05-16T12:50:56.158-06:00 level=INFO msg="daemon: received signal" signal=terminated
```

The daemon reached phase 2 (analyzing 220 issues) and stopped producing logs. Phase 2 analyzes issues using LLM calls (each call to `RootCauseAnalyzer.Analyze()` fires an LLM request). If the local LLM at `localhost:8080` is unavailable, each LLM call fails with "connection refused" after ~5-10 seconds. With 220 issues and `MaxIterationsPerCycle: 5`, even 5 LLM calls will take 25-50 seconds.

## Root Cause

`internal/rpc/server.go` line 22:

```go
writeTimeout = 30 * time.Second // Max time to write response
```

At line 238:
```go
if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
```

This means the RPC server will terminate the connection 30 seconds after receiving a request, regardless of whether the handler is still processing. The `selfimprove.cycle` handler runs the full 5-phase cycle which involves:

1. Detection (~1s)
2. Analysis (220 issues x LLM call time) -- **dominant**
3. Generation (N issues x LLM call time)
4. Validation (N fixes x test execution)
5. Application (git operations)

Even with a working LLM backend, phases 2-3 alone can easily exceed 30 seconds.

The CLI handles this with retries (`SetTimeout(10 * time.Minute)` + 4 retries), but the server-side write timeout fires before the response is ready, causing the client to see "not connected" errors on each attempt.

## Impact

- `selfimprove full-cycle` is **unusable** for any non-trivial codebase
- No amount of client-side timeout adjustment can fix this - the server kills the connection
- Long-running RPC operations in general are affected

## Proposed Fix

Options:
1. **Remove/extend writeTimeout**: The 30-second write timeout is overly aggressive for long-running RPC calls. Increase to a much larger value (e.g., 10 minutes) or make it per-call using `jsonrpc` context.
2. **Streaming responses**: Use JSON-RPC notifications to stream progress updates, then send the final result separately. The write timeout would not apply to the streaming notifications.
3. **Async cycle with polling**: Start the cycle asynchronously, return a cycle ID, and poll for status. This is the most robust approach but requires more architectural change.

The quickest fix is option 1: increase `writeTimeout` or add per-call timeout support to the RPC framework.
