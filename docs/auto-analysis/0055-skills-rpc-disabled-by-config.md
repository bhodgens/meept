# 0055 - Skills RPC timeouts when config has skills.enabled = false

**Date**: 2026-05-16
**Phase**: 8 (Skills System)
**Severity**: High
**Status**: FIXED (2026-05-16)
**Tested**: 2026-05-16

## Resolution

This is the same issue as #0015. Fixed by removing the `skills.*` proxy method registrations from `proxy.go`. Direct RPC handlers in `RegisterSkillsHandlers()` handle skills when enabled. When disabled, no handler exists and callers get "method not found" instead of a 10-second timeout.

**Related**: #0015-skills-rpc-proxy-dead-letter (same root cause, merged fix)
