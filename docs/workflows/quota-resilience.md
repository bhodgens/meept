# Quota Reset Resilience

## Overview

When an LLM provider hits a quota limit (subscription usage window, billing
cap, or plan quota), meept handles it gracefully instead of failing the task.
The system detects quota errors at the HTTP client layer, blocks the affected
provider credential in the Resolver, keeps tasks moving via alias rotation,
and surfaces quota state to users with auto-resume and escalating
notifications.

## Design

### Components

1. **QuotaResetError** (`internal/llm/errors_quota.go`) — structured error
   with reset time, code, retry-after, and status. Parsed from 429/402
   responses via `ParseQuotaResponse` (structured body fields first, then
   rate-limit headers). Implements `NonRetryable` so the client short-retry
   loop exits immediately.
2. **QuotaRetryConfig** (`internal/config/schema.go:758`) — `llm.quota_retry`
   section gating quota-aware wait/retry/deferral behavior.
3. **QuotaWaitChatter** (`internal/llm/resolver_direct.go`) — wraps a Chatter
   with a single ctx-aware wait+retry on quota errors.
4. **QuotaEpisodeTracker** (`internal/agent/quota_episode.go`) — manages
   per-agent+provider quota episodes with a 12h/20h/24h escalation ladder and
   publishes `agent.quota_wait` bus events.
5. **Agent-loop integration** (`internal/agent/loop.go`) — tracks quota
   errors, marks Resolver blocks, and never counts quota as alias health
   failure.

### Error Flow

```
Provider returns 429/402
    -> Client classifies: quota-window shape -> QuotaResetError
       (short-cycle retriable rate limits stay RateLimitError)
    -> QuotaResetError exits the client retry loop immediately
    -> Agent loop: tracker.Enter(...), Resolver block marked,
       RecordAliasFailure is NOT called (quota is not a health failure)
    -> Rotation skips blocked candidates; when all are blocked the
       Resolver returns an all-blocked error
    -> Bus event published: agent.quota_wait (reason "quota_blocked")
    -> User sees "quota wait, resets in Xh Ym" status
```

### Classification Rule

A 429/402 response is a quota error when the structured body carries a
quota/usage-window shape (`usage_limit_reached`, `insufficient_quota`,
`quota_exceeded`, `resource_exhausted`, numeric provider codes, or a reset
horizon) or the status is 402. OpenRouter-style short-cycle limits
(`retriable: true` with `retry_after` < 5 minutes) stay `RateLimitError`.
Anthropic `rate_limit_error` on 429 classifies as quota (design decision in
docs/plans/quota-reset-resilience/01-quota-error-type.md Notes).

### Escalation Ladder

| Time | Escalation Tier | Notification |
|------|-----------------|--------------|
| Entry | "" (initial) | Bus event + push notification |
| 12h | warn | Push notification |
| 20h | action_recommended | Push notification + UI indicator |
| 24h | blocked | Terminal state - human action needed |

The tracker reaps episodes on a background ticker, so tiers fire without
re-entry. Escalation dedup is per agent+provider for the episode lifetime; a
cleared-then-reblocked provider starts a fresh episode.

### Bus Events

Topic: `agent.quota_wait`
- Published by QuotaEpisodeTracker on Enter, tier escalation, Clear, and
  BlockedByEscalation.
- Classified as `agent_progress` in the WS handler (never `chat_message`).
- Payload (`agent.QuotaEvent` JSON): `agent_id`, `task_id`, `from`, `to`,
  `reason` ("quota_blocked", "quota_cleared", or the escalation tier),
  `provider_id`, `credential_key`, `model_id`, `unblock_at` (RFC3339),
  `escalation` ("" | "warn" | "action_recommended" | "blocked"),
  `fallback_model` (optional), `timestamp`.

## Configuration

In `~/.meept/meept.json5` under `llm`:

```json5
llm: {
  // Quota reset resilience (defaults shown).
  quota_retry: {
    enabled: true,             // master switch for quota-aware behavior
    max_wait: "24h",           // upper bound on any quota wait/block/defer
    default_estimate: "1h",    // assumed reset horizon when unknown
    defer_check_interval: "10m" // re-check cadence for deferred work
  }
}
```

Defaults are applied by `DefaultConfig()`; `NormalizeQuotaRetryDefaults`
clamps non-positive durations back to defaults. See
`docs/configuration/llm.md` for the full LLM section.

## Testing

```bash
go test ./internal/llm/ -run 'Quota' -v
go test ./internal/agent/ -run 'Quota' -v
go test ./internal/config/ -run 'Quota' -v
go test ./internal/services/ -run 'Quota' -v
go test -race ./internal/llm/ -run 'Quota' -count=1
```

## Invariants

- Quota errors never re-enter ANY client short-retry loop. All five
  (Chat, ChatWithProgress, streaming delta, anthropic non-streaming,
  anthropic streaming) early-exit on `QuotaResetError` before the
  `RateLimitError`/retryable-status checks.
- QuotaWaitChatter retries exactly once per quota error.
- Context cancellation takes precedence over quota wait.
- A wait exceeding MaxWait returns the error immediately (no blocking).
- Quota is not an alias health failure: RecordAliasFailure is never called
  for a QuotaResetError, and quota blocks live in separate Resolver state
  (`entryBlocks` / `credentialBlock`). `RecordAliasSuccess` lazily deletes
  EXPIRED blocks only (unexpired blocks persist; map growth is bounded).
- Sticky aliases (BalancedStickyRequests) re-pin around quota blocks:
  `resolveStickyCaller` releases pins on quota-blocked models and skips
  blocked candidates when re-pinning.
- Bus event classification: `agent.quota_wait` -> `agent_progress` (never
  `chat_message`).
- Agent state transitions: running -> quota_wait -> blocked (at 24h), with
  Clear returning to running/idle.
- Quota blocks are in-memory only; a daemon restart re-probes providers.

## Known open gaps (audited 2026-08-31)

- `QuotaEvent.FallbackModel` has no producer: declared and parsed by both
  UIs, never set by any publisher.
- No episode GC at the 24h boundary: a blocked episode stays in
  QuotaEpisodeTracker until a successful call on that provider clears it.
- `llm.quota_retry.defer_check_interval` is unconsumed: QuotaResumeWatcher
  hardcodes the 10m `DefaultQuotaResumePollInterval`.
- Unblock-time rendering drift: TUI detail lines use local time, Flutter
  uses UTC.
- Flutter drops the event `model_id`: the goals-pane primary line shows
  "unknown" instead of the blocked model.

## Files

- `internal/llm/errors_quota.go` — QuotaResetError, ParseQuotaResponse,
  QuotaCredentialKey, classification decision.
- `internal/llm/client.go`, `internal/llm/anthropic.go` — 429/402
  classification sites + short-retry early exits.
- `internal/llm/resolver.go` — quota block state, candidate skipping,
  ActiveQuotaBlocks.
- `internal/llm/resolver_direct.go` — QuotaWaitChatter, QuotaWaitConfig.
- `internal/llm/provider_manager.go` — per-credential blocks + probe
  integration for multi-provider setups.
- `internal/agent/quota_episode.go` — QuotaEpisodeTracker, QuotaEvent.
- `internal/agent/loop.go` — quota branch: episode tracking, block marking,
  no RecordAliasFailure.
- `internal/agent/agent_state.go` — quota_wait/blocked states.
- `internal/services/quota_notifier.go` — deduplicated push notifications.
- `internal/comm/http/server.go` — WS classification.
- `internal/tui/`, `ui/flutter_ui/lib/` — quota status surfaces (parity).
