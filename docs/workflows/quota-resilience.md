# Quota Reset Resilience

## Overview

When an LLM provider hits a quota limit (rate limit on token usage, billing cap, or subscription tier), meept handles it gracefully instead of failing the task. The system tracks quota episodes, applies escalation notifications, and allows automatic recovery when the quota window passes.

## Design

### Components

1. **QuotaResetError** — structured error type with reset time, credential key, and retry-after info
2. **QuotaRetryConfig** — configuration for quota-aware wait+retry behavior
3. **QuotaWaitChatter** — wraps LLM chatter with ctx-aware wait+retry on quota errors
4. **QuotaEpisodeTracker** — manages per-agent+provider quota episodes with 12h/20h/24h escalation
5. **Dispatcher Integration** — tracks quota errors in agent loop, publishes bus events

### Error Flow

```
QuotaResetError raised by provider
    -> QuotaWaitChatter detects via errors.As()
    -> Computes wait time (RetryAfter or default)
    -> Waits ctx-aware (respects context deadline)
    -> Retries once
    -> If still failing, returns error to loop
    -> Loop tracks episode via QuotaEpisodeTracker
    -> Bus event published: agent.quota_wait
    -> User sees "quota_wait, resets in Xh" status
```

### Escalation Ladder

| Time | Escalation Tier | Notification |
|------|-----------------|--------------|
| Entry | warn | User notified via bus event |
| 12h | warn | Desktop notification |
| 20h | action_recommended | Desktop notification + UI indicator |
| 24h | blocked | Terminal state - human action needed |

### Bus Events

Topic: `agent.quota_wait`
- Published by QuotaEpisodeTracker on Enter, Clear, BlockedByEscalation
- Classified as `agent_progress` in WS handler (not `chat_message`)
- Payload: QuotaEvent with agent_id, provider, model, unblock_at, escalation

### Configuration

Add to `~/.meept/meept.json5` under `[llm]`:

```json5
llm: {
  // Quota retry resilience
  quota_retry: {
    enabled: true,
    max_wait: "24h",           // Maximum wait time before giving up
    default_estimate: "1h",    // Default wait if Retry-After absent
    defer_check_interval: "30s" // How often to check for unblock
  }
}
```

## Testing

Run quota-specific tests:
```bash
go test ./internal/llm/ -run 'TestQuota' -v
go test ./internal/agent/ -run 'TestQuota' -v
go test ./internal/config/ -run 'TestQuota' -v
```

## Invariants

- Quota errors are transient (non-retryable flag = true so short-retry loop exits)
- QuotaWaitChatter never retries more than once per quota error
- Context cancellation takes precedence over quota wait
- Wait exceeding MaxWait returns error immediately (no blocking)
- Episode tracker uses lazy maps (nil-safe on first use)
- Bus event classification: `agent.quota_wait` -> `agent_progress` (never `chat_message`)
- Agent state transitions: running -> quota_wait -> blocked (at 24h)
- Clear transitions: quota_wait -> running, blocked -> running (if user fixes config)

## Files

- `internal/llm/errors_quota.go` — QuotaResetError, ParseQuotaResponse, QuotaCredentialKey
- `internal/llm/resolver_direct.go` — QuotaWaitChatter, QuotaWaitConfig
- `internal/llm/broker.go` — ChatterForModel quota wrapping
- `internal/agent/quota_episode.go` — QuotaEpisodeTracker, QuotaEvent
- `internal/agent/loop.go` — Integration point for quota error handling
