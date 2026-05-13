# Context Firewall

## Overview
The context firewall manages context pressure through a three-layer system: LLM-based compaction (preserves knowledge), multi-stage compression (importance-based truncation), and hard-limit drops (last resort). See [Context Management](../concepts/context-management.md) for the full architecture.

## Problem
LLM context windows are limited, requiring careful management of conversation history. The context firewall addresses:
- Context window overflow prevention
- Knowledge-preserving summarization via LLM compaction
- Intelligent message prioritization
- Automatic summarization of older content
- Performance optimization under context pressure

## Behavior

### Three-Layer Context Reduction

The firewall applies context reduction in order of increasing severity:

1. **LLM compaction** (~60% utilization): A dedicated LLM produces a structured summary of old messages, preserving decisions, file paths, progress, and next steps. See the [context management concept doc](../concepts/context-management.md) for details on how compaction works, iterative updates, file tracking, and split-turn handling.

2. **Proactive compression** (~70% utilization): Multi-stage compression with importance-based message prioritization. Stage 2 delegates to the compactor when available; otherwise uses legacy LLM summarization or tail-keep truncation.

3. **Hard limit drop** (~80% utilization): Last resort that keeps only system messages and the last 2 non-system messages.

### Compaction (Layer 1)

When `compaction.enabled` is true and utilization exceeds `trigger_ratio`, the `ContextCompactor` replaces old messages with a structured summary. Key behaviors:

- **Structured summaries** with sections for Goal, Constraints, Progress, Key Decisions, Files, Discoveries, Errors, Next Steps, and Critical Context
- **Iterative updates** that merge new messages with an existing summary rather than re-summarizing from scratch
- **Cumulative file tracking** across compaction events so the agent does not re-read files
- **Split-turn handling** when the cut point lands mid-tool-call
- **Dedicated model** support (cheaper/faster model for summarization)
- **Fallback**: If compaction fails (LLM error, timeout), the compressor and hard limit layers still protect against overflow

### Compressor (Layer 2)

| Stage | Utilization | Action |
|-------|------------|--------|
| 0 | < 50% | No action |
| 1 | 50-60% | Warning log only |
| 2 | 60-70% | Summarization (compactor or legacy LLM or tail-keep) |
| 3 | 70-80% | Aggressive: drop low-importance messages |
| 4 | >= 80% | Hard limit: keep system + last 2 |

### Stats Monitoring

`ContextFirewall.Stats()` returns a `FirewallStats` snapshot with:
- **Summarization Failures**: Unsuccessful legacy summarization attempts
- **Dropped Messages**: Messages removed for space
- **Drop Events**: Hard limit context drop incidents
- **Compaction Events**: Successful compaction passes
- **Compaction Tokens Saved**: Total tokens saved by compaction
- **Compaction Fallbacks**: Times compaction was skipped or failed
- **Compression Stats**: Per-stage event counts, tokens saved, quality scores

## Configuration

### Compaction (in `meept.json5`)

```json5
{
  compaction: {
    enabled: false,                  // Master switch
    model: "",                       // Compaction model (empty = small_model or working model)
    reserve_tokens: 16384,           // Tokens reserved for response
    keep_recent_tokens: 20000,       // Recent tokens to keep verbatim
    max_response_tokens: 13107,      // Max tokens for compaction summary
    summary_format: "structured",    // "structured" or "narrative"
    trigger_ratio: 0.60,             // Utilization to trigger compaction
    iterative_updates: true,         // Merge old summary with new messages
    track_file_ops: true,            // Cumulative file operation tracking
    timeout_seconds: 30,             // Timeout for compaction LLM call
  },
}
```

### Context Firewall (in `meept.json5` under `llm.context_firewall`)

```json5
{
  llm: {
    context_firewall: {
      enabled: true,
      summarize_history: true,
      drop_context_on_hard_limit: true,
      wrap_up_threshold: 0.50,       // Warn at 50% utilization
      hard_limit: 0.80,              // Drop at 80% utilization
      proactive_compression: true,   // Enable multi-stage compressor
      hierarchical_summarization: true,
      max_summary_level: 3,
    },
  },
}
```

## Observability

### Logging
- Compaction events (tokens before/after, split-turn status, file tracking count)
- Context pressure events per compressor stage
- Summarization operations (legacy path)
- Message drop decisions (hard limit)
- Performance statistics

### Metrics
- Context utilization percentage
- Compaction success rate and tokens saved
- Summarization success rate
- Message drop frequency
- Average compression quality score

### Debug Info
- Current context composition
- Compaction summary content and cumulative file operations
- Compressor stage reached
- Summarization model status
- Firewall rule effectiveness

## Edge Cases

### Compaction Failure
- Compactor returns without compacting (too few messages, empty conversation text)
- LLM summarization call fails or times out
- System falls back to compressor stage 2 (legacy summarization or tail-keep truncation)
- All failures are logged with reasons for debugging

### Summarization Failure (Legacy)
- Fallback to message dropping
- Logs failure reason for debugging
- Alternative summarization approaches attempted

### Critical Context Loss
- System messages always retained
- Anchor messages never compacted or dropped
- Compaction preserves structured knowledge rather than discarding it

### Model Context Limit
- Hard limit enforced to prevent errors
- Graceful degradation through three layers
- User notified of context constraints via logging

### Performance Degradation
- Monitoring detects slowdowns
- Adaptive strategies applied
- User notified of performance issues