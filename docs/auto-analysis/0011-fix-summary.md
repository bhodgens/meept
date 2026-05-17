# Fix Summary — Harness Bugs #0003, #0007, #0008, #0009, #0010

**Date**: 2026-05-15
**Status**: Fixed

## Fixes Applied

### #0007 — LLM Empty Content / No Response Body Logging (CRITICAL)
**Files changed**: `internal/llm/client.go`, `internal/llm/models.go`, `internal/llm/client_test.go`

1. Added response body preview logging at debug level (500 char truncation)
2. Changed `ResponseMessage.Content` from `*string` to `json.RawMessage`
3. Added `ContentString()` method that handles:
   - Plain string content: `"Hello"`
   - Null/empty content: `null`
   - Array content blocks: `[{"type":"text","text":"..."}]`
4. Updated `parseResponse()` to use `ContentString()` instead of `*string` dereference
5. Updated test fixtures from `stringPtr()` to `json.RawMessage()`
6. Added 4 new tests for `ContentString()` edge cases

### #0009 — Task/Queue List Panic on Flag Conflict (CRITICAL)
**Files changed**: `cmd/meept/task.go:87`, `cmd/meept/queue.go:150`

Removed `-s` shorthand from `--state` flags in both `task list` and `queue list` commands. The `-s` shorthand conflicted with the global `--socket` flag.

### #0008 — Scheduler RPC Handlers Not Wired (CRITICAL)
**Files changed**: `internal/daemon/daemon.go`

Added `scheduler.RegisterRPCHandlers(rpcServer, components.Scheduler)` call after the queue handler registration. This wires the direct Go RPC handlers that override the bus proxy handlers, eliminating the timeout on `meept jobs`.

### #0010 — Duplicate Help Command (low)
**Files changed**: `cmd/meept/main.go`

Changed `rootCmd.AddCommand(newHelpCmd(rootCmd))` to `rootCmd.SetHelpCommand(newHelpCmd(rootCmd))`. This replaces the default cobra help command instead of adding a duplicate.

### #0003 — Status Missing Model Info (medium)
**Files changed**: `internal/rpc/server.go`, `internal/daemon/daemon.go`

1. Added `defaultModel` field and `SetDefaultModel()` method to `rpc.Server`
2. Updated status handler to use `s.defaultModel` instead of hardcoded `""`
3. Added `rpcServer.SetDefaultModel(components.ModelsConfig.Model)` call during daemon startup

## Verification

- All LLM tests pass including new `ContentString()` tests
- All agent tests pass including `ShouldTerminate` tests
- `./bin/meept --help` shows only one `help` entry
- `./bin/meept task list --help` and `./bin/meept queue list --help` work without panic
- `./bin/meept status` shows `LLM Model: zai/glm-4.7` instead of `n/a`
- `./bin/meept jobs` returns instantly instead of timing out

## Remaining Findings (Not Yet Fixed)

| # | Finding | Severity | Status |
|---|---------|----------|--------|
| #0001 | Config loading priority (CWD shadows home) | medium | Open |
| #0002 | SQLite FTS5 missing, LIKE fallback | medium | Open |
| #0004 | API key warning hardcodes GALA_API_KEY | low | Open |
| #0005 | Tool termination skips LLM follow-up synthesis | high | Open (needs design review) |
| #0006 | Tiny model over-classifies intents | medium | Open (guardrails needed) |
