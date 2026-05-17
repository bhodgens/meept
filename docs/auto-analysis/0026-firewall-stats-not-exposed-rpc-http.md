# Context Firewall Stats Not Exposed via RPC or HTTP

**Date**: 2026-05-15
**Phase**: 10 (context firewall and pressure management)
**Severity**: medium
**Component**: `internal/rpc/`, `internal/comm/http/`, `internal/services/`

## Description

The `ContextFirewall.Stats()` method returns a comprehensive `FirewallStats` struct with counters for summarization failures, dropped messages, drop events, compaction events/tokens saved/fallbacks, compression events by stage, quality scores, and total compressions. However, none of these stats are exposed through any RPC method, HTTP endpoint, or CLI command.

The `meept status` command shows token budget (used/total/cost) and RPC/bus info, but nothing about context firewall health. There is no way for an operator to monitor whether:
- Compaction is triggering correctly
- Summarization is failing (and how often)
- Messages are being dropped (context loss)
- Compression quality is degrading
- The firewall is doing anything at all

This makes the context firewall effectively unobservable in production.

## Reproduction

1. Run `./bin/meept status`
2. Observe output shows: Status, PID, Uptime, LLM Model, Token Budget (used/cost), RPC Server (methods, bus subs)
3. No firewall stats are present
4. Check RPC server: `grep -r "firewall\|Firewall" internal/rpc/` returns zero results
5. Check HTTP handlers: `grep -r "firewall\|Firewall" internal/comm/http/` returns zero results
6. The `Stats()` method exists on `ContextFirewall` but is only called in tests

## Evidence

- `ContextFirewall.Stats()` defined at `internal/llm/context_firewall.go:266`
- No RPC handler references firewall or Firewall: `internal/rpc/` has zero matches
- No HTTP handler references firewall: `internal/comm/http/` has zero matches
- `meept status` output shows no firewall information
- The `FirewallStats` struct has 11 fields, none of which are accessible at runtime

## Root Cause

The firewall stats infrastructure was implemented (atomic counters, snapshot method, structured stats type) but was never wired to any transport layer. The RPC server and HTTP handlers were not updated to include firewall stats in their responses.

## Proposed Fix

1. Add a `GetFirewallStats` RPC method to `internal/rpc/server.go`
2. Include firewall stats in the `status` RPC response and `meept status` CLI output
3. Optionally add `GET /api/v1/metrics/firewall` HTTP endpoint
4. Include firewall stats in the existing metrics dashboard for the menubar app

Key fields to surface:
- `summarization_failures` -- high values indicate summarization is broken
- `dropped_messages` / `drop_events` -- context is being lost
- `compaction_events` / `compaction_tokens_saved` -- is compaction working
- `compaction_fallbacks` -- how often compaction fails and falls through
- `compression_*_events` -- which compression stages are firing
- `avg_quality_score` -- is compression quality degrading

## Fix Applied

1. **AgentLoop** (`internal/agent/loop.go`): Added `FirewallStats()` method that type-asserts `l.llm` to `*llm.ContextFirewall` and returns a `map[string]any` with all stats (summarization failures, dropped messages, drop events, compaction events/tokens/fallbacks, and compression metrics including quality scores).

2. **RPC Server** (`internal/rpc/server.go`): The `FirewallStatsGetter` callback field (lines 42-44) and handlers (`firewall.stats` standalone endpoint and inclusion in `status` response) were already present but never wired. Fixed by wiring the getter in `internal/daemon/daemon.go` to `components.AgentLoop.FirewallStats()`.

3. **HTTP Server** (`internal/comm/http/server.go` + `api_handlers.go`): Added `FirewallStatsGetter` callback field to `Server` struct, route `GET /api/v1/metrics/firewall`, and `handleFirewallStats` handler. Wired in `internal/daemon/daemon.go`.

4. **Daemon wiring** (`internal/daemon/daemon.go`): Registered `FirewallStatsGetter` on both RPC server (inside the `if rpcServer != nil` block, after scheduler handlers) and HTTP server (after `NewServer()` call). Both use a closure pointing to `components.AgentLoop.FirewallStats()`.

## Verification

- `go build ./...` passes
- `go test ./internal/agent/... ./internal/daemon/... ./internal/comm/http/... ./internal/llm/... ./internal/rpc/...` all pass

## Exposed Endpoints

- **RPC**: `firewall.stats` -- standalone JSON-RPC method returning `map[string]any`
- **RPC**: `status` and `daemon.status` -- includes `"firewall": <map[string]any>` key when stats are available
- **HTTP**: `GET /api/v1/metrics/firewall` -- returns same JSON as RPC `firewall.stats`

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both

**Status**: FIXED
