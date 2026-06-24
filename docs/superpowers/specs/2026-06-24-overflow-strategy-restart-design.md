# Overflow Strategy: Summarize-Then-Restart

**Date:** 2026-06-24
**Status:** Approved

## Problem

When context utilization hits the hard limit (~80%), the firewall's only action is `dropOldContext()` — a destructive operation that keeps system messages + the last few messages and discards everything else. This causes:

- Loss of accumulated knowledge (decisions, file paths, errors, findings)
- Agents re-reading files and re-deriving conclusions after context drops
- No mechanism to produce a coherent summary and continue cleanly

Additionally, the existing `CompactionConfig` in `config/schema.go` is **never wired** from the daemon to the agent loop. The daemon calls `SetContextFirewallConfig()` (which sets firewall-level fields) but never passes `cfg.Compaction` through, so `loop.config.Compaction.Enabled` is always false and the `ContextCompactor` is never created outside of tests.

## Solution

### 1. Overflow Strategy Selector

Add an `OverflowStrategy` string field to `ContextFirewallConfig` and `LLMContextFirewallConfig`:

| Value | Behavior at hard limit |
|-------|----------------------|
| `"restart"` (default) | Summarize full conversation, replace context with `[system_msgs, summary_msg, last_user_msg]` |
| `"drop"` | Existing `dropOldContext()` — keep system + last N, discard rest |
| `"summarize"` | Existing `summarizeOldHistory()` — legacy partial summarization |

The firewall's `processMessages()` dispatches on the strategy at the hard limit check.

### 2. Summarize-Then-Restart Logic

When `overflow_strategy == "restart"` and utilization >= hard limit:

1. Separate system messages from conversation messages
2. Serialize all non-system messages into conversation text (reusing the compactor's `serializeMessages` pattern)
3. Call the summary model with `handoffCompactionPrompt` (already exists for high-fidelity handoffs)
4. Build restart context: `[system_msgs..., summary_system_msg, last_user_msg]`
5. If summarization fails, fall back to `dropOldContext` (graceful degradation)
6. Log the restart event with token counts before/after

The summary message uses `RoleSystem` with `[Context Restart]` prefix. The last user message is preserved verbatim so the agent can continue the current turn.

### 3. Daemon Compaction Wiring Fix

Wire `cfg.Compaction` into the agent loop alongside the existing `SetContextFirewallConfig` call. Add a new `SetCompactionConfig` method on `AgentLoop` that stores the compaction settings so the firewall construction in `Start()` can create the compactor.

### 4. Relationship to Existing Layers

```
Utilization  Layer                    Action
-----------  ------------------------ --------------------------------
~60%         Compaction (Layer 1)     LLM-summarize old messages in-place
~70%         Compressor (Layer 2)     Multi-stage truncation by importance
~80%         Overflow Strategy        restart: summarize all, fresh context
             (Layer 3)                drop: keep system + last N (existing)
                                      summarize: legacy partial (existing)
```

Compaction and compression run before the overflow strategy. The overflow strategy only fires when utilization >= hard_limit despite the earlier layers. With `restart`, the agent gets a clean handoff document instead of fragmented context.

## Changes

### Core LLM Package

| File | Change |
|------|--------|
| `internal/llm/context_firewall.go` | Add `OverflowStrategy` field to `ContextFirewallConfig`. Add `summarizeAndRestart()` method. Modify `processMessages()` hard limit block to dispatch on strategy. Add restart counter to `FirewallStats`. |
| `internal/llm/context_firewall.go` | Default `OverflowStrategy` to `"restart"` in `NewContextFirewall` when empty. |

### Config Schema

| File | Change |
|------|--------|
| `internal/config/schema.go` | Add `OverflowStrategy string` to `LLMContextFirewallConfig` with `json:"overflow_strategy"` tag. |

### Agent Loop Wiring

| File | Change |
|------|--------|
| `internal/agent/loop.go` | Add `OverflowStrategy` to `AgentConfig`. Thread it into `ContextFirewallConfig` at firewall construction time. Add `SetCompactionConfig()` method and call it from daemon. Fix compaction wiring to use the config. |

### Daemon Wiring

| File | Change |
|------|--------|
| `internal/daemon/components.go` | Call `SetCompactionConfig(cfg.Compaction)` alongside existing `SetContextFirewallConfig(cfg.LLM.ContextFirewall)`. Pass `OverflowStrategy` through existing firewall config setter. |

### Config Template

| File | Change |
|------|--------|
| `config/meept.json5` | Add `overflow_strategy: "restart"` to `llm.context_firewall` section. Change `compaction.enabled` default to `true` (the wiring now works). |

### Config UI

| File | Change |
|------|--------|
| `internal/configui/sections_llm.go` | Add overflow strategy selector field (dropdown: restart/drop/summarize) after `drop_context_on_hard_limit`. |

### Documentation

| File | Change |
|------|--------|
| `docs/concepts/context-management.md` | Document the three overflow strategies, the restart flow, and the compaction wiring fix. |
| `docs/workflows/context-firewall.md` | Add overflow strategy section with config examples. |

### Tests

| File | Change |
|------|--------|
| `internal/llm/context_firewall_test.go` | Add tests: restart strategy produces `[system, summary, user]`, restart fallback on LLM failure, drop strategy unchanged, summarize strategy unchanged. |

## Error Handling

- If the summary model call fails during restart, fall back to `dropOldContext()` and log a warning. The agent continues with reduced context rather than failing entirely.
- If the summary model call times out (30s default from compactor config), same fallback.
- If there are fewer than 3 messages, restart is a no-op (not enough to summarize).

## Config Reference

```json5
{
  llm: {
    context_firewall: {
      enabled: true,
      // ... existing fields ...
      hard_limit: 0.80,
      drop_context_on_hard_limit: true,
      overflow_strategy: "restart",  // "drop" | "summarize" | "restart" (default: "restart")
    },
  },
  compaction: {
    enabled: true,  // now wired (was previously dead config)
    // ... existing fields unchanged ...
  },
}
```

`drop_context_on_hard_limit` only applies when `overflow_strategy` is `"drop"`. When `"restart"`, the context is always replaced (the whole point is to restart). When `"summarize"`, the existing `summarizeOldHistory` path runs regardless of `drop_context_on_hard_limit`.
