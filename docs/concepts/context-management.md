# Context Management

Meept's context management system controls how conversation history fits within LLM context windows. It uses a three-layer hybrid approach that starts with knowledge-preserving compaction and falls back to progressively more aggressive strategies when needed.

## Why Context Management Matters

LLM context windows are finite. During long-running agentic tasks -- debugging sessions, multi-file refactors, complex research -- an agent can accumulate thousands of tokens of conversation history. Without management, the context window fills up and the LLM either truncates input or errors out.

The challenge is not just reducing context size, but doing so while **preserving accumulated knowledge**. When an agent has read 20 files, tried 5 approaches, and made critical decisions, losing that information causes the agent to re-read files, re-derive conclusions, and potentially repeat failed approaches.

## The Three-Layer Hybrid Approach

Meept uses three layers of context management, triggered at increasing utilization thresholds:

| Layer | Strategy | Trigger | Purpose |
|-------|----------|---------|---------|
| 1 | LLM-based compaction | ~60% utilization | Primary: summarize old messages into structured summary |
| 2 | Proactive compressor | ~70% utilization | Safety net: multi-stage truncation by importance |
| 3 | Overflow strategy | ~80% utilization | Final safety valve: restart, drop, or summarize |

Each layer runs only if the previous layer failed to bring utilization below its threshold. This means most context pressure is handled by compaction (which preserves knowledge), with fallback strategies available if compaction fails or is insufficient.

## Layer 1: LLM-Based Compaction

When context utilization exceeds the configured trigger ratio (default 60%), the `ContextCompactor` replaces old messages with a structured summary produced by a dedicated LLM call. This is the primary context reduction strategy.

### How Compaction Works

1. **Trigger check**: The `ContextFirewall` measures token utilization before each LLM call. If it exceeds `trigger_ratio`, compaction begins.

2. **Cut point selection**: The compactor walks backwards from the end of the message list, counting tokens. It identifies a cut point that keeps the most recent `keep_recent_tokens` tokens of messages. The cut point respects:
   - System messages are never compacted
   - Tool call / tool result pairs are kept together
   - The cut prefers user message boundaries

3. **Summarization**: Messages before the cut point are flattened to text (including tool call details) and sent to the compaction model with a structured prompt requesting: Goal, Constraints, Progress, Key Decisions, Files, Important Discoveries, Errors Encountered, Next Steps, and Critical Context.

4. **Replacement**: The old messages are replaced with a single system message containing the structured summary plus cumulative file operation tracking.

5. **Iteration continues**: The conversation continues with full awareness of what happened before.

### Structured Summary Format

The compaction prompt asks the LLM to extract information into these sections:

```
## Goal
[What the user is trying to accomplish]

## Constraints
[Requirements, restrictions, or preferences]

## Progress
[What has been done so far, including attempts and outcomes]

## Key Decisions
- [list of decisions, one per line]

## Files
- read: /path/to/file.go
- write: /path/to/new_file.go
- edit: /path/to/modified.go

## Important Discoveries
- [list of findings, one per line]

## Errors Encountered
- [list of errors and lessons learned]

## Next Steps
[What remains to be done, in priority order]

## Critical Context
[API endpoints, config values, commands, etc.]
```

The `Files` section uses prefixes (`read:`, `write:`, `edit:`) to distinguish file operation types.

### Iterative Summary Updates

When a previous compaction summary already exists, the compactor uses an **update prompt** that merges the old summary with new messages rather than re-summarizing from scratch. This is more efficient and preserves information from earlier compaction windows.

### Cumulative File Operation Tracking

The compactor maintains a cumulative `FileOperationSet` across compaction events. Each compaction:
1. Parses the new summary for file paths and operation types
2. Merges with the existing cumulative set
3. Includes the full cumulative file list in the compaction message

This prevents the agent from re-reading files it already examined in earlier compaction windows.

### Split-Turn Handling

When the cut point lands in the middle of a tool call / tool result pair, the compactor produces two summaries:
1. A **history summary** of all messages before the partial turn
2. A **turn prefix summary** of what the assistant was doing when compaction triggered

Both are merged into a single compaction message, preserving context of the interrupted work.

### Dedicated Compaction Model

Compaction can use a different (cheaper, faster) model than the working model. The fallback chain for model resolution is:

1. `compaction.model` from config (if set)
2. `small_model` from models config
3. The working model (same behavior as before compaction existed)

Using a dedicated model saves cost (expensive reasoning models are not needed for summarization) and reduces latency.

## Layer 2: Proactive Compressor

The `ContextCompressor` implements multi-stage compression based on utilization thresholds:

| Stage | Utilization | Action |
|-------|------------|--------|
| 0 | < 50% | No action |
| 1 | 50-60% | Warning log only |
| 2 | 60-70% | LLM summarization of old history, or tail-keep truncation |
| 3 | 70-80% | Aggressive: drop low-importance messages |
| 4 | >= 80% | Hard limit: keep system + last 2 messages |

When a compactor is available, stage 2 delegates to it. Otherwise it falls back to the legacy `summarizeWithLLM` or tail-keep truncation.

## Layer 3: Overflow Strategy

When utilization exceeds the hard limit (default 80%), the firewall applies the configured overflow strategy. Three strategies are available:

### Overflow Strategies

| Strategy | Behavior | Use case |
|----------|----------|----------|
| `"restart"` (default) | Summarize the full conversation into a single handoff document, then replace context with `[system_msgs, summary_msg, last_user_msg]`. The agent gets a clean restart with accumulated knowledge preserved. | Long-running sessions where context must be preserved coherently |
| `"drop"` | Keep system messages + last N non-system messages, discarding everything else. Fast but loses knowledge. | Emergency fallback; maximum speed |
| `"summarize"` | Use the legacy `summarizeOldHistory` path (partial summarization of old messages). | Backward-compatible behavior |

#### Restart Flow

When `overflow_strategy == "restart"` and utilization >= hard limit:

1. Separate system messages from conversation messages
2. Serialize all non-system messages into conversation text (including tool calls and tool results)
3. Call the summary model with the `handoffCompactionPrompt` (structured handoff format)
4. Build restart context: `[system_msgs..., summary_system_msg, last_user_msg]`
5. If summarization fails, fall back to `dropOldContext` (graceful degradation)
6. Log the restart event with token counts before/after

The summary message uses `RoleSystem` with a `[Context Restart]` prefix. The last user message is preserved verbatim so the agent can continue the current turn.

The restart counter (`restart_events` in `FirewallStats`) tracks how many times the restart strategy has been applied.

`drop_context_on_hard_limit` only applies when `overflow_strategy` is `"drop"`. When `"restart"`, the context is always replaced. When `"summarize"`, the existing `summarizeOldHistory` path runs regardless of `drop_context_on_hard_limit`.

### Hard Limit Drop (legacy)

When `overflow_strategy == "drop"`, the firewall drops all old context, keeping only system messages and the last few non-system messages (preserving tool-call/tool-result pairing). This is the original behavior before the overflow strategy selector was introduced.

## Configuration

Compaction is configured in `meept.json5` under the `compaction` section:

```json5
{
  compaction: {
    enabled: true,                   // Master switch (enabled by default)
    model: "",                       // Compaction model ref (empty = small_model or working model)
    reserve_tokens: 16384,           // Tokens reserved for response after compaction
    keep_recent_tokens: 20000,       // Recent tokens to keep verbatim (not summarized)
    max_response_tokens: 13107,      // Max tokens for compaction summary
    summary_format: "structured",    // "structured" or "narrative"
    trigger_ratio: 0.60,             // Utilization ratio to trigger compaction (0.0-1.0)
    iterative_updates: true,         // Merge old summary with new messages
    track_file_ops: true,            // Cumulative file operation tracking across compactions
    timeout_seconds: 30,             // Timeout for compaction LLM call
  },
}
```

### Key Configuration Options

- **`enabled`**: When false, the compactor is not created and the system falls back to the existing compressor and hard limit layers only.
- **`model`**: A model reference in `provider/model-id` format (e.g., `"zai/glm-4.5-air"`). Resolved via the standard model resolver. When empty, falls back to `small_model`, then the working model.
- **`trigger_ratio`**: The utilization threshold (0.0-1.0) that triggers compaction. Default 0.60 means compaction starts at 60% of context limit.
- **`summary_format`**: `"structured"` produces parseable sections (recommended). `"narrative"` produces free-form prose (smaller but less queryable).
- **`iterative_updates`**: When true, subsequent compactions update the existing summary rather than re-summarizing from scratch.
- **`track_file_ops`**: When true, maintains cumulative file operation sets across compaction events.
- **`timeout_seconds`**: If the compaction LLM call exceeds this duration, it is skipped and fallback truncation runs instead.

The context firewall configuration (under `llm.context_firewall`) controls layers 2 and 3, including the proactive compression stages, overflow strategy, and hard limit behavior. The `overflow_strategy` field selects between `"restart"` (default), `"drop"`, and `"summarize"`.

```json5
{
  llm: {
    context_firewall: {
      enabled: true,
      hard_limit: 0.80,              // Trigger overflow strategy at 80% utilization
      overflow_strategy: "restart",  // "drop" | "summarize" | "restart"
      // ... other fields ...
    },
  },
}
```

## Observability

### Logging

Compaction events are logged at `Info` level with token counts before and after, split-turn status, and cumulative file tracking count. When compaction fails or is skipped, warnings are logged and the system falls back to the next layer.

### Firewall Stats

`ContextFirewall.Stats()` returns a `FirewallStats` snapshot including compaction-specific counters:

- `CompactionEvents`: Number of successful compaction passes
- `CompactionTokensSaved`: Total tokens saved by compaction
- `CompactionFallbacks`: Number of times compaction was skipped or failed

These are also exposed through the HTTP API at `GET /api/v1/metrics/live`.

### Compression Stats

The proactive compressor tracks its own statistics via `CompressionStats`:
- Per-stage event counts (warning, summarize, aggressive, hard limit)
- Total tokens saved
- Running average quality score (token ratio weighted by critical message retention)

## File Layout

| File | Purpose |
|------|---------|
| `internal/llm/context_compactor.go` | `ContextCompactor` -- cut point algorithm, serialization, structured summarization, iterative updates, file tracking |
| `internal/llm/context_compressor.go` | `ContextCompressor` -- multi-stage compression pipeline (layer 2) |
| `internal/llm/context_firewall.go` | `ContextFirewall` -- orchestrates all three layers, overflow strategy (restart/drop/summarize), budget enforcement |
| `internal/config/schema.go` | `CompactionConfig` -- configuration structure |
| `config/meept.json5` | Configuration template with compaction section |
| `internal/agent/loop.go` | Wires compactor into the agent loop, resolves compaction model |
