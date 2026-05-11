# LLM-Based Context Compaction (Tentative -- Needs Review)

**Status:** Tentative, needs review before implementation.
**Date:** 2026-05-11
**Inspirations:** Pi Agent's context compaction approach; Meept's existing multi-strategy truncation.

---

## 1. Problem Statement

Meept's current context management strategies all share the same fundamental weakness: they **discard information**. Whether by dropping old messages (LRU truncation), removing low-importance messages, or discarding everything before the tail window, the knowledge contained in those messages is permanently lost.

This matters most during long-running agentic tasks. An agent debugging a complex issue may read 20 files, try 5 approaches, and accumulate critical knowledge about the codebase. When context fills up, the current strategies throw away the earlier work and the agent re-reads files it already examined, re-derives conclusions it already reached, and potentially repeats failed approaches.

**What Pi Agent does differently:** When context approaches the limit, Pi uses a dedicated LLM call to produce a structured summary of the old conversation. This summary preserves decisions, file paths, progress, and next steps in a compact form. The agent continues with full awareness of what happened before, rather than losing it.

**The key insight:** A single LLM summarization call (which costs a few hundred tokens of input/output on a cheap model) can preserve thousands of tokens of accumulated knowledge that would otherwise be lost.

---

## 2. Current Architecture

### 2.1 Conversation (`internal/agent/conversation.go`)

The `Conversation` struct manages chat message history with:

- **Message classification** (`MessageClassification`): Messages are classified by semantic type -- `MessageUserInput`, `MessageAssistantPlan`, `MessageAssistantConclusion`, `MessageToolResult`, `MessageToolResultKey`, `MessageReasoningStep`.
- **Importance levels** (`MessageImportance`): 4-level priority system -- Critical > High > Medium > Low -- derived from classification.
- **Anchor messages**: Messages marked as exempt from truncation (validation instructions, escalation triggers).
- **Truncation methods**:
  - `Truncate()`: LRU-style eviction, keeps last `maxMessages` (default 200).
  - `TruncateByTokens(budget)`: Removes old messages to fit a token budget, walking backwards from the end.
  - `TruncateByImportance(budget)`: Removes lowest-importance messages first, then by token count within same importance.
  - `CompressByImportance(ratio)`: Ratio-based variant of importance truncation.
  - `GetWindowedMessages(budget)`: Preserves system prompt + original user message + anchor messages + recent messages within budget.
- **Memory context injection** via `InjectContext` and `InjectContextBounded`.
- **TurnBudgetTracker**: Multi-turn budget allocation with warning zone and wrap-up detection.

### 2.2 ContextFirewall (`internal/llm/context_firewall.go`)

The `ContextFirewall` wraps a `Chatter` and enforces context budgets. It is created in `AgentLoop.initialize()` (line ~769 of `loop.go`) and replaces the raw LLM client.

**Processing pipeline** (`processMessages()`):
1. Estimate token usage via tokenizer.
2. If `ProactiveCompression` is enabled, run the `ContextCompressor` multi-stage pipeline first.
3. Check hard limit (80% utilization): if exceeded and `DropContextOnHardLimit`, drop old context (keep system + last 2).
4. Check wrap-up threshold (50%): log warning.
5. Chunk large inputs if configured.
6. Summarize old history if over hard limit and `SummarizeHistory` enabled.

**Summarization** (`summarizeWithLevel()`):
- Uses a structured prompt template (`structuredSummaryPromptTemplate`) that asks the LLM to produce sections: DECISIONS, FILES, QUESTIONS, STATUS, FINDINGS, SUMMARY.
- Parses the response into `SummaryExtract` via regex-based section splitting.
- Formats as compact system message.
- Supports hierarchical summarization (recursive re-summarization up to `MaxSummaryLevel`).

**Current limitation:** The summary model defaults to `nil`, which means the firewall uses the **same model** as the agent's working model for summarization (see line 784 of `loop.go`: `nil, // summaryModel - uses inner by default`). There is no way to configure a separate, cheaper model for this purpose.

### 2.3 ContextCompressor (`internal/llm/context_compressor.go`)

Multi-stage compression based on utilization thresholds:
- **Stage 0** (< 50%): No action.
- **Stage 1** (50-60%): Warning log only.
- **Stage 2** (60-70%): LLM-based summarization of old history, keep system + summary + last 4.
- **Stage 3** (70-80%): Aggressive -- drop low-importance, keep system + critical + last 4.
- **Stage 4** (>= 80%): Hard limit -- keep system + last 2 only.

The compressor has a `summarizer Chatter` field. When nil, summarization falls back to tail-keep truncation (losing information).

### 2.4 Model Resolution (`internal/llm/resolver.go`)

The `Resolver` resolves model references (`"provider/model-id"`) from `models.json5`. It supports:
- Default model and small model references.
- Skill-based resolution (cheapest model satisfying capability requirements).
- Alias-based resolution with rotation and cooldown.
- Direct ref resolution via `ResolveRef()`.

The `ModelBroker` wraps multiple provider clients and routes to healthy ones.

### 2.5 Configuration (`internal/config/schema.go`)

Current config structure relevant to context management:

```go
type LLMConfig struct {
    Budget          BudgetConfig
    Broker          LLMBrokerConfig
    AdaptiveTimeout LLMAdaptiveTimeoutConfig
    ContextFirewall LLMContextFirewallConfig  // <-- context management lives here
    Metrics         LLMMetricsConfig
    Cache           LLMSimpleFeatureConfig
}
```

`LLMContextFirewallConfig` has fields for proactive compression, hierarchical summarization, thresholds, etc. There is **no dedicated compaction model** field.

### 2.6 Integration Point (`internal/agent/loop.go`)

In `AgentLoop.initialize()` (line ~769), the `ContextFirewall` is created with `nil` for the summary model. The `SetContextFirewallConfig()` method wires config values from the user-facing `LLMContextFirewallConfig` into the agent loop config struct. There is no mechanism to inject a dedicated summarization model.

---

## 3. Proposed Architecture

### 3.1 Overview

Introduce LLM-based **context compaction** as the primary context reduction strategy. When context approaches the limit, instead of discarding old messages, use a dedicated (configurable) LLM model to produce a structured summary. The summary replaces the old messages, preserving knowledge while dramatically reducing token count.

This is not a replacement for the entire firewall -- it is a **new, smarter summarization backend** that replaces the current `summarizeWithLLM()` and `summarizeOldHistory()` implementations with a more sophisticated approach inspired by Pi Agent.

### 3.2 Compaction Trigger

**Current behavior:** Summarization triggers at the hard limit (80% utilization) in the firewall, and at stage 2 (60%) in the compressor. Both use simple "keep last N messages" logic for selecting what to summarize.

**Proposed behavior:** Trigger compaction when context utilization exceeds a configurable threshold (default: 60%). Use the model's actual token counts from the most recent LLM response (if available) rather than heuristic estimates for more accurate triggering.

```go
// Trigger condition
currentTokens := tokenizer.CountTokens(messages)
utilization := float64(currentTokens) / float64(model.ContextLimit)
if utilization >= compactionConfig.TriggerRatio {
    // Compact
}
```

### 3.3 Cut Point Algorithm

**Current behavior:** The compressor keeps the last 4 messages (stage 2) or last 2 messages (stage 4). The firewall keeps the last 4 messages. Neither considers turn boundaries or tool result pairs.

**Proposed behavior:** Walk backwards from the end of the message list, counting tokens. Keep the most recent `keepRecentTokens` tokens of messages. Find the cut boundary using these rules:

1. Never cut in the middle of a tool call / tool result pair (assistant message with `ToolCalls` followed by tool result messages). These must stay together.
2. Prefer cutting at user message boundaries (start of a new user turn).
3. Never cut system messages.
4. Never cut anchor messages.
5. Never cut the original user message.

```go
type CutResult struct {
    CutIndex      int              // Index of first message in the "keep" section
    ToCompact     []ChatMessage    // Messages before cut (to be summarized)
    ToKeep        []ChatMessage    // Messages at and after cut (preserved verbatim)
    SystemMsgs    []ChatMessage    // System messages (preserved)
    SplitTurn     bool             // Whether the cut landed mid-turn
}
```

### 3.4 Structured Summarization Prompt

**Current behavior:** Uses `structuredSummaryPromptTemplate` with sections: DECISIONS, FILES, QUESTIONS, STATUS, FINDINGS, SUMMARY. The prompt asks for a flat summary of the conversation text.

**Proposed behavior:** Enhanced structured prompt with additional sections and explicit file operation tracking:

```
You are summarizing a conversation to preserve context for continued work.
Extract the following structured information:

## Goal
[What the user is trying to accomplish]

## Constraints
[Requirements, restrictions, or preferences mentioned]

## Progress
[What has been done so far, including approach attempts and outcomes]

## Key Decisions
- [list key decisions made, one per line]

## Files
- [list all file paths READ, one per line, prefixed with "read: "]
- [list all file paths WRITTEN/CREATED, one per line, prefixed with "write: "]
- [list all file paths EDITED/MODIFIED, one per line, prefixed with "edit: "]

## Important Discoveries
- [list important findings, one per line]

## Errors Encountered
- [list errors or failures encountered and what was learned, one per line]

## Next Steps
[What remains to be done, in order of priority]

## Critical Context
[Any context that must be preserved for the work to continue correctly,
 such as API endpoints, configuration values, test commands, etc.]

<conversation>
{serialized conversation}
</conversation>
```

### 3.5 Iterative Summary Updates

**Current behavior:** Each summarization call produces a standalone summary. If a previous summary exists, it is treated as just another system message and re-summarized along with everything else.

**Proposed behavior:** When a previous compaction summary already exists, use an **update prompt** that merges the old summary with new messages, rather than re-summarizing from scratch. This is more efficient and avoids losing information from the previous summary.

```go
// If existing summary exists:
updatePrompt := fmt.Sprintf(`You are updating a conversation summary with new context.

## Previous Summary
%s

## New Messages Since Last Summary
%s

Produce an updated summary in the same format. Preserve all information from the
previous summary that is still relevant. Add new information from the new messages.
Remove information that is no longer relevant or has been superseded.`, existingSummary, newMessagesText)
```

### 3.6 Split-Turn Handling

**Current behavior:** Not handled. The cut point may land mid-turn (e.g., after the assistant's tool call but before the tool result), which would break the message sequence.

**Proposed behavior:** If the cut point lands mid-turn (inside a tool call / tool result pair), generate two summaries:

1. **History summary**: Summary of all messages before the split turn.
2. **Turn prefix summary**: Summary of the partial turn (assistant message + any tool calls and results before the cut).

Merge both summaries into a single compaction message. This preserves the context of what the agent was doing when compaction occurred.

### 3.7 File Operation Tracking

**Current behavior:** The structured summary captures file paths in a FILES section, but does not distinguish between read, written, and edited files. No cumulative tracking across compactions.

**Proposed behavior:** Maintain cumulative sets of file operations across compaction events:

```go
type FileOperationSet struct {
    Read   map[string]bool  // Files read (cumulative)
    Written map[string]bool  // Files created/written (cumulative)
    Edited map[string]bool  // Files modified (cumulative)
}
```

When a compaction occurs:
1. Parse the new summary for file operations.
2. Merge with the existing `FileOperationSet`.
3. Include the cumulative file set in the summary message sent to the agent.

This prevents the agent from re-reading files it has already examined in earlier compaction windows.

### 3.8 Serialized Conversation Flattening

**Current behavior:** The firewall serializes messages as `"role: content\n"` lines (line 723 of `context_firewall.go`). Tool calls are not serialized.

**Proposed behavior:** Flatten the conversation to text before sending to the summarization LLM, including tool call information:

```go
func serializeMessages(messages []ChatMessage) string {
    var sb strings.Builder
    for _, msg := range messages {
        switch msg.Role {
        case RoleUser:
            fmt.Fprintf(&sb, "[User]: %s\n", msg.Content)
        case RoleAssistant:
            if len(msg.ToolCalls) > 0 {
                fmt.Fprintf(&sb, "[Assistant]: %s\n", msg.Content)
                for _, tc := range msg.ToolCalls {
                    fmt.Fprintf(&sb, "  [Tool Call]: %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
                }
            } else {
                fmt.Fprintf(&sb, "[Assistant]: %s\n", msg.Content)
            }
        case RoleTool:
            fmt.Fprintf(&sb, "  [Tool Result]: %s\n", msg.Content)
        }
    }
    return sb.String()
}
```

### 3.9 Dedicated Compaction Model

**Current behavior:** The `ContextFirewall` and `ContextCompressor` accept an optional `summaryModel Chatter`. In practice, this is always `nil` (line 784 of `loop.go`), so the working model is used for summarization.

**Proposed behavior:** Add a `compactionModel` field to the configuration. Resolve it via the existing `Resolver` to create a dedicated `Chatter` for compaction. This model should be:
- **Cheaper** than the working model (lower cost per token).
- **Fast** (low latency for timely compaction).
- **Sufficient** for summarization (does not need code/reasoning capabilities).

The compaction model is separate from the existing `small_model` concept. While `small_model` is used for classification and lightweight tasks, the compaction model is specifically for context compaction and may be a different model entirely.

---

## 4. Pros/Cons Analysis

### 4.1 Pi Agent's Single LLM-Based Approach

**Pros:**
- Preserves structured knowledge instead of discarding it.
- Agent retains awareness of past decisions, files examined, errors encountered.
- Better continuity across compaction boundaries.
- File operation tracking prevents redundant file reads.

**Cons:**
- Costs an extra LLM call per compaction event.
- Adds latency to the turn where compaction triggers.
- LLM summarization quality varies; may hallucinate or lose details.
- Single strategy -- no fallback if summarization fails.

### 4.2 Meept's Current Multi-Strategy Approach

**Pros:**
- Zero-cost truncation (no extra LLM calls).
- Multiple strategies provide defense in depth.
- Deterministic -- no variability in what is kept/dropped.
- Immediate (no latency from extra API calls).
- Already has importance classification, anchor messages, hierarchical summarization.

**Cons:**
- All strategies discard information.
- Agent loses context about past work after compaction.
- May re-read files, re-derive conclusions, repeat failed approaches.
- Importance classification is heuristic-based (keyword matching) and may misclassify.
- Token estimation uses `charsPerToken = 3` heuristic, which can be inaccurate.

### 4.3 Proposed Hybrid Approach

Use LLM-based compaction as the **primary** strategy, with existing mechanisms as **safety nets**:

| Layer | Strategy | Trigger | Purpose |
|-------|----------|---------|---------|
| 1 | LLM compaction (new) | ~60% utilization | Primary context reduction |
| 2 | Proactive compressor stage 3 (existing) | ~70% utilization | If compaction was insufficient |
| 3 | Hard limit drop (existing) | ~80% utilization | Last resort |

This means:
- Most context pressure is handled by LLM compaction, which preserves knowledge.
- If compaction fails (LLM error, timeout), the existing compressor stages still protect against context overflow.
- The hard limit drop remains as a safety valve.

### 4.4 Why a Dedicated Compaction Model

| Factor | Working model for compaction | Dedicated compaction model |
|--------|----------------------------|---------------------------|
| Cost | Expensive model used for trivial summarization | Cheap model saves money |
| Latency | Slow model adds delay to agent turns | Fast model minimizes disruption |
| Budget impact | Consumes working model's rate limit quota | Preserves rate limit for actual work |
| Context window | Uses working model's context for summarization input | Can use a model with larger context for summarization |
| Quality | May be overkill (reasoning model for summarization) | Adequate quality at lower cost |

For example, if the working model is `zai/glm-4.7` (128K context, reasoning capabilities), the compaction model could be `zai/glm-4.5-air` (already configured as `small_model`) or even a cheaper local model via Ollama. The summarization task does not need reasoning -- it needs good language understanding, which cheaper models provide.

---

## 5. Configuration

### 5.1 New `compaction` Section in `meept.json5`

Add a top-level `compaction` section (not nested under `llm.context_firewall`, because compaction is conceptually distinct from the firewall's threshold-based pipeline):

```json5
{
  compaction: {
    // Master switch -- when false, falls back to existing truncation
    enabled: true,

    // Model reference for compaction (provider/model-id format).
    // Resolved via the existing Resolver. If empty, falls back to
    // small_model, then to the working model.
    model: "zai/glm-4.5-air",

    // Tokens to reserve for the response after compaction.
    // The compaction summary + recent messages must fit within
    // (model.ContextLimit - reserveTokens).
    reserveTokens: 16384,

    // Number of recent tokens to keep verbatim (not summarized).
    // Messages within this budget from the end of the conversation
    // are preserved in full.
    keepRecentTokens: 20000,

    // Maximum tokens for the compaction summary response.
    // Capped at 80% of reserveTokens to leave room for recent messages.
    maxResponseTokens: 13107,

    // Summary format: "structured" (recommended) or "narrative".
    // Structured produces parseable sections; narrative produces
    // free-form prose (smaller but less queryable).
    summaryFormat: "structured",

    // Utilization ratio that triggers compaction (0.0-1.0).
    // Default 0.60 (60% of context limit).
    triggerRatio: 0.60,

    // Enable iterative summary updates (merge old summary + new messages).
    // When true, subsequent compactions update the existing summary
    // rather than re-summarizing from scratch.
    iterativeUpdates: true,

    // Enable cumulative file operation tracking across compactions.
    trackFileOps: true,

    // Timeout for compaction LLM call (seconds).
    // If compaction exceeds this, it is skipped and fallback truncation runs.
    timeoutSeconds: 30,
  },
}
```

### 5.2 Schema Changes

Add to `internal/config/schema.go`:

```go
// CompactionConfig configures LLM-based context compaction.
type CompactionConfig struct {
    Enabled          bool    `json:"enabled"             toml:"enabled"`
    Model            string  `json:"model"               toml:"model"`
    ReserveTokens    int     `json:"reserve_tokens"      toml:"reserve_tokens"`
    KeepRecentTokens int     `json:"keep_recent_tokens"  toml:"keep_recent_tokens"`
    MaxResponseTokens int    `json:"max_response_tokens" toml:"max_response_tokens"`
    SummaryFormat    string  `json:"summary_format"      toml:"summary_format"`
    TriggerRatio     float64 `json:"trigger_ratio"       toml:"trigger_ratio"`
    IterativeUpdates bool    `json:"iterative_updates"   toml:"iterative_updates"`
    TrackFileOps     bool    `json:"track_file_ops"      toml:"track_file_ops"`
    TimeoutSeconds   int     `json:"timeout_seconds"     toml:"timeout_seconds"`
}
```

Add `Compaction CompactionConfig` to the `Config` struct.

### 5.3 Models Config

No changes required to `models.json5`. The compaction model is resolved via an existing model reference (e.g., `"zai/glm-4.5-air"`) using the standard `ResolveModelRef()` function. The model must already be defined in the providers section.

Optionally, a new alias could be added:

```json5
{
  "model_aliases": {
    "compaction": {
      "models": ["zai/glm-4.5-air"],
      "timeout": 30,
      "max_fails": 3
    }
  }
}
```

---

## 6. Implementation Phases

### Phase 1: Core Compaction Engine

**Goal:** Build the `ContextCompactor` that implements cut point finding, serialized flattening, structured summarization, and iterative updates.

**Files to create:**
- `internal/llm/context_compactor.go` -- Main compactor struct and methods.
- `internal/llm/context_compactor_test.go` -- Unit tests for cut point algorithm, serialization, prompt building.

**Key types:**

```go
// ContextCompactor performs LLM-based context compaction.
type ContextCompactor struct {
    config       CompactorConfig
    summarizer   Chatter        // Dedicated compaction model client
    tokenizer    Tokenizer
    logger       *slog.Logger
    fileOps      *FileOperationSet  // Cumulative file tracking
    lastSummary  string         // Last compaction summary (for iterative updates)
}

type CompactorConfig struct {
    ReserveTokens     int
    KeepRecentTokens  int
    MaxResponseTokens int
    SummaryFormat     string  // "structured" or "narrative"
    TrackFileOps      bool
    TimeoutSeconds    int
}
```

**Key methods:**
- `Compact(ctx, messages) CompactResult` -- Main entry point.
- `findCutPoint(messages) CutResult` -- Walk backwards, respect turn boundaries.
- `serializeMessages(messages) string` -- Flatten to text.
- `buildSummaryPrompt(conversationText, existingSummary) string` -- Build the LLM prompt.
- `parseSummaryResponse(raw) SummaryExtract` -- Parse structured response.
- `updateFileOps(summary SummaryExtract)` -- Update cumulative file ops.
- `buildCompactionMessage(summary, fileOps) ChatMessage` -- Build the replacement message.

**Tasks:**
1. Implement `findCutPoint()` with turn boundary awareness.
2. Implement `serializeMessages()` with tool call serialization.
3. Implement structured summary prompt template.
4. Implement `parseSummaryResponse()` using existing section parsing from `context_firewall.go`.
5. Implement `FileOperationSet` with merge semantics.
6. Implement iterative update logic (detect existing summary, use merge prompt).
7. Write comprehensive unit tests for each component.

### Phase 2: Configuration and Model Resolution

**Goal:** Wire the compaction config into the existing config system and resolve the compaction model.

**Files to modify:**
- `internal/config/schema.go` -- Add `CompactionConfig` struct and field to `Config`.
- `internal/config/config.go` -- Add path expansion for compaction (if needed).
- `internal/agent/loop.go` -- Wire compaction config into agent loop, create compactor.

**Files to create:**
- `internal/llm/context_compactor_config_test.go` -- Config loading tests.

**Tasks:**
1. Add `CompactionConfig` struct to schema.go.
2. Add `Compaction CompactionConfig` field to `Config` struct.
3. Add defaults in `DefaultConfig()`.
4. In `AgentLoop.initialize()`, resolve the compaction model:
   ```go
   // Resolve compaction model
   compactionModelRef := cfg.Compaction.Model
   if compactionModelRef == "" {
       compactionModelRef = resolver.SmallModel().ModelID  // fallback to small model
   }
   compactionModelCfg := resolver.ResolveRef(compactionModelRef)
   compactionClient := broker.ChatWithModel(ctx, compactionModelRef, ...)
   ```
5. Create `ContextCompactor` with the resolved model.
6. Write tests for config loading with compaction section.

### Phase 3: Integration with Context Pipeline

**Goal:** Replace the existing summarization backend in the context pipeline with the new compactor.

**Files to modify:**
- `internal/llm/context_compressor.go` -- Use `ContextCompactor` for stage 2 summarization.
- `internal/llm/context_firewall.go` -- Use `ContextCompactor` for `summarizeOldHistory()`.
- `internal/agent/loop.go` -- Pass compactor to firewall and compressor.

**Tasks:**
1. Add `compactor *ContextCompactor` field to `ContextCompressor`.
2. In `summarizeOldHistory()`, if compactor is available, use `compactor.Compact()` instead of the existing `summarizeWithLLM()`.
3. Add `compactor *ContextCompactor` field to `ContextFirewall`.
4. In `summarizeWithLevel()`, delegate to compactor when available.
5. In `AgentLoop.initialize()`, pass the compactor to both firewall and compressor.
6. Ensure fallback: if compaction fails (LLM error, timeout), fall back to existing tail-keep truncation.
7. Write integration tests verifying the full pipeline.

### Phase 4: Split-Turn Handling

**Goal:** Handle the case where the cut point lands mid-turn.

**Files to modify:**
- `internal/llm/context_compactor.go` -- Add split-turn detection and dual-summary logic.

**Tasks:**
1. In `findCutPoint()`, detect when the cut lands inside a tool call/result pair.
2. When split is detected, separate the partial turn from the history.
3. Generate two summaries: history summary and turn prefix summary.
4. Merge summaries into a single compaction message.
5. Write tests for split-turn scenarios.

### Phase 5: Testing and Quality

**Goal:** Ensure correctness and quality of compaction.

**Files to create:**
- `internal/llm/context_compactor_integration_test.go` -- Integration tests with mock LLM.
- `internal/agent/conversation_compaction_test.go` -- End-to-end compaction tests with conversation.

**Tasks:**
1. Create a mock `Chatter` that returns deterministic summaries for testing.
2. Test compaction with various conversation patterns:
   - Short conversations (no compaction needed).
   - Long conversations with file reads/writes.
   - Conversations with tool call chains.
   - Split-turn scenarios.
   - Iterative updates (multiple compactions in sequence).
3. Test fallback behavior when compaction fails.
4. Test file operation tracking across multiple compactions.
5. Test that the compaction message preserves critical information.
6. Add logging/observability for compaction events.

### Phase 6: Documentation and Cleanup

**Goal:** Update documentation and clean up any dead code.

**Files to modify:**
- `docs/concepts/context-management.md` (or relevant docs) -- Document compaction.
- `CLAUDE.md` -- Update architecture section if needed.
- `config/meept.json5` -- Add compaction section to template.

**Tasks:**
1. Document the compaction feature in user-facing docs.
2. Update the context management architecture diagram.
3. Add compaction to the meept.json5 config template with comments.
4. Review existing summarization code for cleanup opportunities (the old `summarizeWithLLM` in `context_compressor.go` can potentially be removed once compactor is the primary path).

---

## 7. Model Resolution

The compaction model is resolved through the existing model resolution infrastructure:

```
meept.json5 (compaction.model: "zai/glm-4.5-air")
    |
    v
Resolver.ResolveRef("zai/glm-4.5-air")
    |
    v
ResolveModelRef("zai/glm-4.5-air", providersConfig)
    |
    v
ModelConfig{ProviderID: "zai", ModelID: "glm-4.5-air", ...}
    |
    v
ModelBroker.ChatWithModel(ctx, "zai/glm-4.5-air", messages, opts...)
    |
    v
Chatter (LLM client for compaction)
```

**Fallback chain:**
1. Use `compaction.model` from config if set.
2. If empty, fall back to `models.json5` `small_model`.
3. If `small_model` is empty, fall back to the working model (same as current behavior).
4. If compaction LLM call fails, fall back to existing truncation strategies.

**Why not use an alias?** The compaction model can be an alias reference if desired. The resolver supports both direct refs (`"zai/glm-4.5-air"`) and alias names. Using an alias would enable failover between multiple compaction models.

**Integration in AgentLoop:**

```go
// In AgentLoop.initialize():
func (loop *AgentLoop) initialize(ctx context.Context) error {
    // ... existing model resolution ...

    // Resolve compaction model
    var compactor *llm.ContextCompactor
    if loop.config.Compaction.Enabled {
        compactionModelRef := loop.config.Compaction.Model
        if compactionModelRef == "" && loop.resolver != nil {
            if sm := loop.resolver.SmallModel(); sm != nil {
                compactionModelRef = fmt.Sprintf("%s/%s", sm.ProviderID, sm.ModelID)
            }
        }
        if compactionModelRef != "" && loop.broker != nil {
            // Create compactor with broker-routed model
            compactor = llm.NewContextCompactor(llm.CompactorConfig{
                ReserveTokens:     loop.config.Compaction.ReserveTokens,
                KeepRecentTokens:  loop.config.Compaction.KeepRecentTokens,
                MaxResponseTokens: loop.config.Compaction.MaxResponseTokens,
                SummaryFormat:     loop.config.Compaction.SummaryFormat,
                TrackFileOps:      loop.config.Compaction.TrackFileOps,
                TimeoutSeconds:    loop.config.Compaction.TimeoutSeconds,
            }, loop.broker, compactionModelRef, tokenizer, logger)
        }
    }

    // Pass compactor to firewall and compressor
    firewall := llm.NewContextFirewall(
        loop.llm, model, firewallConfig,
        nil, // summaryModel (compactor handles summarization internally)
        logger, tokenizer,
    )
    firewall.SetCompactor(compactor)  // New method
    // ...
}
```

---

## 8. Open Questions

1. **Should compaction be session-scoped or per-conversation?** The `FileOperationSet` and `lastSummary` state need to live somewhere. Per-conversation makes more sense (each conversation tracks its own compaction state).

2. **How to handle the compaction model's context limit?** If the compaction model has a smaller context limit than the working model, the conversation text to summarize might not fit. Solution: truncate the conversation text to fit within the compaction model's context limit before sending.

3. **Should compaction respect the existing hierarchical summarization?** If compaction produces a summary that is still too large, should it be re-compacted? The iterative update approach should prevent this in most cases, but a safety check is prudent.

4. **Thread safety:** The `ContextCompactor` will be called from the `ContextFirewall` which is called per-request. The compactor's mutable state (`fileOps`, `lastSummary`) needs synchronization if the same compactor is shared across concurrent requests for different conversations. Solution: make compactor per-conversation, or use a map keyed by conversation ID.

5. **Metrics:** Should compaction events be tracked in the metrics store? Yes -- track compaction count, tokens saved, summary size, compaction latency, and compaction failures.

6. **User visibility:** Should the TUI show when compaction occurs? A subtle indicator (e.g., "[context compacted]") in the conversation view would help users understand what happened.

---

## 9. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Compaction LLM call fails | Medium | Medium | Fallback to existing truncation |
| Compaction produces poor summary | Medium | Medium | Structured prompt with explicit sections; quality checks |
| Compaction adds unacceptable latency | Low | High | Use fast/cheap model; configurable timeout; async? |
| Compaction model unavailable | Low | Low | Fallback chain (small_model -> working model -> truncation) |
| Compaction loses critical information | Medium | High | Structured prompt; file tracking; never drop anchors/system messages |
| Thread safety issues | Medium | Medium | Per-conversation compactor instances |
| Configuration complexity | Low | Low | Sensible defaults; optional feature (disabled by default initially) |

---

## 10. Success Criteria

1. Compaction preserves key decisions, file paths, and progress from the summarized conversation.
2. Compaction reduces context by at least 50% (measured by token count).
3. Compaction latency is under 5 seconds with a fast model.
4. Fallback to truncation works correctly when compaction fails.
5. File operation tracking prevents redundant file reads across compaction boundaries.
6. Iterative updates produce summaries that are at least as good as full re-summarization.
7. No regressions in existing context management tests.
8. Configuration is well-documented with sensible defaults.
