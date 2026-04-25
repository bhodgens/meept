# Context Compression

Meept uses a multi-stage context compression system to manage conversation history and keep token usage within model limits. When enabled, the compressor runs before each LLM request and applies progressively more aggressive strategies as context fills up.

## How It Works

The compressor monitors **context utilization** -- the ratio of current tokens to the model's context limit -- and triggers actions at four thresholds:

| Stage | Utilization | Action | Messages Kept |
|-------|-------------|--------|---------------|
| 1 - Warning | 50% | Log warning only. No messages removed. | All |
| 2 - Summarize | 60% | Trim old history, keeping system prompt + last 4 messages. | System + last 4 |
| 3 - Aggressive | 70% | Aggressive trim, keeping system prompt + last 2 messages. | System + last 2 |
| 4 - Hard Limit | 80% | Drop old context entirely, keeping system prompt + last 2 messages. | System + last 2 |

Each stage is logged with the current utilization, token counts before and after, and the number of messages dropped.

## Configuration

Proactive compression is controlled via `~/.meept/meept.toml`:

```toml
[context_firewall]
enabled = true
proactive_compression = true
wrap_up_threshold = 0.50
hard_limit = 0.80
drop_context_on_hard_limit = true
```

Threshold ratios can be tuned per deployment. The defaults are conservative and work well for most models with 8k-128k context windows.

## Code-Aware Compression

When the compressor trims history, it preserves **system messages** unconditionally. This means system prompts, skill instructions, and injected code context (e.g., AST summaries or LSP diagnostics) are never dropped. Only user/assistant conversation turns are candidates for removal.

For finer control over which messages survive compression, see the `CompressByImportance` method on conversations (which scores messages by recency, role, and relevance).

## Observability

Compression stats are exposed through `ContextFirewall.Stats()`, which returns a `FirewallStats` snapshot:

```go
type FirewallStats struct {
    SummarizationFailures         uint64
    DroppedMessages               uint64
    DropEvents                    uint64
    CompressionWarningEvents      uint64  // Stage 1 triggers
    CompressionSummarizeEvents    uint64  // Stage 2 triggers
    CompressionAggressiveEvents   uint64  // Stage 3 triggers
    CompressionHardLimitEvents    uint64  // Stage 4 triggers
    CompressionTokensSaved        uint64  // Cumulative tokens freed
}
```

These counters are atomic-safe for concurrent access. Use them to monitor context pressure and tune thresholds:

- **High warning events** but low summarize/aggressive events: the model context is large enough. Thresholds can stay as-is.
- **Frequent hard limit events**: consider a model with a larger context window, or reduce conversation history earlier by lowering the summarize ratio.
- **Rapidly growing tokens saved**: compression is doing significant work. Check if conversation inputs are unnecessarily large.

### Log Messages

Each compression stage emits structured log messages:

- **Stage 1**: `context utilization entering warning zone` (warn level)
- **Stage 2**: `context compressed via summarization` (info level)
- **Stage 3**: `aggressive context compression applied` (warn level)
- **Stage 4**: `hard limit reached, old context dropped` (error level)

All log entries include `utilization`, `tokens_before`, `tokens_after`, and `saved` fields for monitoring and alerting.

## Metrics Integration

Compression stats feed into the metrics subsystem and are available through the HTTP REST API:

```
GET /api/v1/metrics/live
```

See [metrics](metrics.md) for the full metrics API reference.
