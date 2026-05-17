# Skills RPC Handlers Dead-Letter When Skills Disabled

**Date**: 2026-05-15
**Phase**: 8 (skills system)
**Severity**: high
**Status**: FIXED (2026-05-16)
**Component**: `internal/rpc/proxy.go`

## Resolution

Removed the `skills.*` proxy handler registrations from `proxy.go`. The `skills.list`, `skills.get`, `skills.execute`, and `skills.triage` methods were unconditionally registered as bus proxies in `RegisterProxyMethods()`. When skills are disabled (`skills.enabled: false`), the direct RPC handlers in `RegisterSkillsHandlers()` are not registered, causing the proxy to publish to bus topics that nobody listens to, resulting in a 10-second timeout.

**Fix**: Removed the proxy registrations entirely from `proxy.go:76-80`. When skills are enabled, `RegisterSkillsHandlers` (called in `daemon.go:165-171`) registers direct RPC handlers that bypass the bus entirely. When skills are disabled, the RPC server returns "method not found" instead of timing out after 10 seconds, giving users an immediate clear error message.

**Changes**:
- `internal/rpc/proxy.go`: Removed `skills.list`, `skills.get`, `skills.execute`, `skills.triage` proxy registrations
