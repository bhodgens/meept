# Status Command Shows LLM Model as "n/a"

**Date**: 2026-05-15
**Phase**: 1 (daemon startup & core transport)
**Severity**: medium
**Component**: `internal/rpc/server.go` (lines 317-318)

## Description

The `meept status` command always shows `LLM Model: n/a` even when the daemon has a properly configured and resolved model. This is because the RPC status handler hardcodes both `model` and `default_model` to empty strings in its response.

## Reproduction

1. Start the daemon with a valid model configured (e.g., `zai/glm-4.7`)
2. Run `./bin/meept status`
3. Observe: `LLM Model: n/a`

## Evidence

In `internal/rpc/server.go:313-326`:
```go
return map[string]any{
    RPCKeyStatus:             "running",
    "version":            "0.2.0-go",
    "uptime_seconds":     time.Since(s.startTime).Seconds(),
    RPCKeyModel:              "",        // <-- hardcoded empty
    "default_model":      "",            // <-- hardcoded empty
    "tokens_used":        0,
    "tokens_remaining":   100000,
    "budget_used":        0.0,
    "budget_remaining":   10.0,
    "registered_methods": methods,
    "bus_subscribers":    busStats["_total"],
}, nil
```

The CLI (`cmd/meept/status.go:98-104`) checks both fields, falls back to `"n/a"`:
```go
model := status.Model
if model == "" {
    model = status.DefaultModel
}
if model == "" {
    model = "n/a"
}
```

Additionally, token usage and budget are hardcoded to 0/100000 even when budget tracker data is available from the daemon's `Components.BudgetTracker`.

## Root Cause

The RPC status handler was written as a static placeholder and never wired to the actual LLM configuration or budget tracker. The daemon has access to both via `Components` but the RPC server's `registerBuiltinHandlers` method doesn't have access to those.

## Proposed Fix

1. Pass the resolved model name and default model to the RPC server at construction time (or via a callback/interface)
2. Wire the budget tracker's `GetStatus()` output to the status response
3. The bus-based `StatusHandler` in `components.go` already does this correctly for budget — the RPC handler should follow the same pattern

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
