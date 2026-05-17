# Scheduler RPC Handlers Registered But Never Wired

**Date**: 2026-05-15
**Phase**: 7 (job scheduler)
**Severity**: high
**Status**: FIXED (2026-05-16)
**Component**: `internal/scheduler/rpc.go`, `internal/daemon/daemon.go`

## Resolution

The direct RPC handlers were **already wired** during a prior fix. In `daemon.go:204-207`:

```go
// Register scheduler RPC handlers (direct Go handlers override bus proxy)
if components.Scheduler != nil {
    scheduler.RegisterRPCHandlers(rpcServer, components.Scheduler)
    logger.Info("Scheduler RPC handlers registered")
}
```

This call is made after `proxy.RegisterProxyMethods()` registers the same `scheduler.*` methods on the RPC server. Since the RPC server uses last-registration-wins for method dispatch, the direct handlers override the bus proxy handlers, eliminating the timeout issue.

**Note**: The scheduler's direct handlers also supersede the proxy entries in `proxy.go:63-65` (`scheduler.list_jobs`, `scheduler.add_job`, `scheduler.schedule_agent_task`). The proxy entries will still fire as fallback for unimplemented scheduler methods, but the direct handlers handle the core CRUD operations.
