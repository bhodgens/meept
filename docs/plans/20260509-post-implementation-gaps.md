# Post-Implementation Gap Analysis

> **Date:** 2026-05-09
> **Updated:** 2026-05-10
> **Scope:** Review of 5 token/memory compression plans for completeness and correctness

---

## Summary

All 5 plans have been implemented at the feature level. Core functionality is present and wired end-to-end. This document catalogs the gaps, bugs, and deviations found during audit, with detailed remediation plans for each.

**Overall completion: ~85%**

| Plan | Core Feature | Gaps Found |
|------|-------------|------------|
| 1. Token Compression Research | Done | 0 gaps |
| 2. Token Cache Implementation | Done | 4 gaps |
| 3. LLM Memory Consolidation | Partially done | 1 gap (Task 2 unimplemented) |
| 4. Hierarchical Summarization | Done | 0 gaps |
| 5. Proactive Compression | Done | 2 gaps |

---

## Plan 1: Token Compression Research Findings

**File:** `docs/plans/2026-04-25-token-compression-research-findings.md`

**Status: COMPLETE — no gaps.**

All 6 gaps identified in the research doc have been addressed by the other 4 plans:
- Gap 1 (code-aware compression) -> implemented in executor.go + ast/parser.go
- Gap 2 (hierarchical summarization) -> implemented in context_firewall.go
- Gap 3 (proactive compression) -> implemented in context_compressor.go
- Gap 4 (content-aware summarization) -> implemented in context_firewall.go
- Gap 5 (compression quality metrics) -> implemented in context_compressor.go
- Gap 6 (memory consolidation) -> Task 1 done, Task 2 deferred (see Plan 3)

---

## Plan 2: Token Cache Implementation

**File:** `docs/plans/2026-04-25-token-cache-implementation.md`

**Status: COMPLETE with 4 gaps.**

### Gap 2.1: L1 Eviction is FIFO, Not LRU

**File:** `internal/llm/token_cache_l1.go`
**Severity:** Medium

The `evictOldest()` method evicts by `CreatedAt` timestamp (FIFO/oldest-first), not by last-access time (LRU). The plan specified "Size-based eviction (LRU)."

**Impact:** A frequently-accessed old entry will be evicted before a rarely-accessed newer one.

**Fix:** Add `lastAccessedAt time.Time` to `l1CacheEntry`, update on every `Get()`, evict by `lastAccessedAt` instead of `createdAt`.

---

### Gap 2.2: RPC Handler Hardcodes l2_entries to 0

**File:** `internal/rpc/cache.go`
**Severity:** Low

The `handleStats` method sets `"l2_entries": 0` with a comment "Would need L2 cache access for accurate count." However, the coordinator's `Stats()` method already computes `EntryCount` as `l1Count + l2Count`.

**Impact:** CLI `cache status` shows incorrect L2 entry count.

**Fix:** Use the actual L2 count from `coordinator.Stats()`.

---

### Gap 2.3: Eviction Metrics Not Recorded to Metrics Store

**File:** `internal/llm/token_cache.go`
**Severity:** Low

The plan specified `cache_evictions_total` and `cache_entry_count` metrics. The implementation tracks these in-memory in `CacheStats` but does not push them to the `metrics.Store`. Only `cache.hit` and `cache.miss` are recorded.

**Impact:** Eviction events are invisible to the metrics dashboard and historical queries.

**Fix:** Record `cache.eviction` events using the existing `recordMetric()` pattern after eviction operations.

---

### Gap 2.4: HTTP REST Endpoints Incomplete

**File:** `internal/comm/http/server.go`, `internal/comm/http/api_handlers.go`
**Severity:** Medium

Only `GET /api/v1/cache/stats` and `POST /api/v1/cache/clear` are registered as REST endpoints. `cache.invalidate` and `cache.inspect` have no dedicated REST endpoints — they only work via RPC or the HTTP transport's RPC passthrough.

**Impact:** The menubar app and other HTTP-only clients cannot invalidate cache by file path or inspect cache entries by hash.

**Fix:** Add `POST /api/v1/cache/invalidate` (JSON body with `path`) and `GET /api/v1/cache/inspect?hash=<hash>` following the existing handler registration pattern.

---

## Plan 3: LLM Memory Consolidation

**File:** `docs/plans/2026-04-25-llm-memory-consolidation.md`

**Status: PARTIALLY COMPLETE — 1 gap (Task 2 unimplemented).**

### Gap 3.1: Semantic Clustering Not Implemented

**File:** `internal/memory/clustering.go` (does not exist)
**Severity:** Medium

Task 2 from the plan called for embedding-based semantic clustering:
- `ClusterBySimilarity()` function using cosine similarity
- Grouping memories by similarity threshold instead of calendar date
- Using existing `internal/memory/vector/` infrastructure (OpenAI/Ollama embedding providers, cosine similarity search)

The `MergeRelated()` method in `consolidation.go` still only groups by date. The comment at line 355-357 explicitly marks this as `MEM-17 DEFERRED`.

**Existing infrastructure that could be leveraged:**
- `internal/memory/vector/embedding.go` — `Provider` interface with `GenerateEmbedding`/`GenerateEmbeddings`, `OpenAIProvider`, `OllamaProvider`
- `internal/memory/vector/store.go` — `Store` type with cosine similarity search

**Fix:** Create `internal/memory/clustering.go` with `ClusterBySimilarity()`, update `MergeRelated()` to use it when embeddings are available.

---

## Plan 4: Hierarchical Summarization

**File:** `docs/plans/2026-04-25-hierarchical-summarization.md`

**Status: COMPLETE — no gaps.**

All 3 tasks are fully implemented:
- Task 1 (hierarchical/recursive summarization) — `summarizeWithLevel()` in context_firewall.go
- Task 2 (content-aware summarization) — structured extraction prompt with `SummaryExtract` parsing
- Task 3 (compression quality metrics) — `QualityMetrics` struct with token ratio, critical retention tracking

**Notes:**
- Feature is disabled by default (`HierarchicalSummarization: false`)
- `SummaryLevel` is tagged `json:"-"` so it is not persisted across sessions
- `summaryModel` is passed as `nil`, meaning the main model handles summarization

---

## Plan 5: Proactive Compression Implementation

**File:** `docs/plans/2026-04-25-proactive-compression-implementation.md`

**Status: COMPLETE with 2 gaps.**

### Gap 5.1: Stages 3 and 4 Are Functionally Identical

**File:** `internal/llm/context_compressor.go`
**Severity:** Medium

Both `aggressiveCompress()` and `dropOldContext()` call `keepTail(messages, 2)`. The plan specified:
- **Stage 3 (70%):** "aggressive summarization + drop low-importance messages"
- **Stage 4 (80%):** "hard limit enforcement with context drop"

The implementation does not use importance-based selection or summarization at Stage 3.

**Impact:** No meaningful difference between 70% and 80% compression — both just keep system + last 2 messages.

**Fix:** Stage 3 should use importance-based compression: keep system, critical, and last 4 messages; drop low-importance messages first. Stage 4 remains the hard tail-cut. The `CompressByImportance()` logic in `conversation.go` and the `ChatMessage.Critical` field can guide the implementation.

---

### Gap 5.2: All Plan Checkboxes Unchecked

**File:** `docs/plans/2026-04-25-proactive-compression-implementation.md`
**Severity:** Low (documentation)

Every checkbox in the plan remains `- [ ]` despite full implementation. This creates confusion about what has been done.

**Fix:** Update all checkboxes to `- [x]`.

---

## Observations Requiring Remediation

These observations were initially noted as "for awareness" but represent real bugs and deficiencies that need to be fixed.

---

### Observation 2: Proactive Compression Has No Real Summarization Between 60-80%

**File:** `internal/llm/context_compressor.go` (lines 352-357), `internal/llm/context_firewall.go` (lines 542-556, 689-800)
**Severity:** High

#### Problem

The `ContextCompressor.summarizeOldHistory()` is a **placeholder** that just calls `keepTail(messages, 4)` — it drops messages without summarizing them. The real LLM summarization lives in `ContextFirewall.summarizeOldHistory()` which performs structured extraction (decisions, files, questions, findings, narrative) with hierarchical re-summarization up to 3 levels deep. However, the real summarization only triggers above the 80% hard limit.

Between 60-80% utilization, messages are permanently dropped with no content preservation:

| Utilization | Compressor Action | Content Preservation |
|------------|-------------------|---------------------|
| < 50% | None | Full |
| [50%, 60%) | Warning logged | Full |
| [60%, 70%) | `keepTail(4)` | **None — messages silently dropped** |
| [70%, 80%) | `keepTail(2)` | **None — messages silently dropped** |
| >= 80% | `keepTail(2)` then legacy LLM summarize | Partial (most messages already gone) |

The `ContextCompressor` struct has **no reference to a `Chatter`/LLM client** — it only holds `CompressionConfig`, `CompressionStats`, `logger`, and `Tokenizer`. There is no architectural way for it to call an LLM even if the code were written to do so.

Additionally, `aggressiveCompress()` (line 360) and `dropOldContext()` (line 367) are both identical `keepTail(2)` calls, making stages 3 and 4 functionally indistinguishable.

#### Root Cause

1. `ContextCompressor` was designed as a pure token-counting/threshold engine without LLM access
2. The `context.Context` parameter in `summarizeOldHistory` is explicitly unused (`_ context.Context`)
3. The real summarization in `ContextFirewall` is gated behind `currentTokens > ContextLimit * HardLimit` (>= 80%), but by that point the compressor has already dropped the messages

#### Remediation Plan

**Step 1: Give the compressor LLM access**

Add an optional `summarizer Chatter` field to `ContextCompressor`:

```go
// In internal/llm/context_compressor.go

type ContextCompressor struct {
    config    CompressionConfig
    stats     CompressionStats
    summarizer Chatter  // Optional: when set, enables LLM-based summarization at stage 2
    tokenizer TokenCounter
    logger    *slog.Logger
    mu        sync.Mutex
}
```

Update `NewContextCompressor` to accept an optional `Chatter`:

```go
func NewContextCompressor(config CompressionConfig, tokenizer TokenCounter, summarizer Chatter, logger *slog.Logger) *ContextCompressor {
    return &ContextCompressor{
        config:     config,
        tokenizer:  tokenizer,
        summarizer: summarizer,
        logger:     logger,
    }
}
```

In `ContextFirewall.Initialize()` or wherever the compressor is constructed, pass the `summaryModel` (or main model) as the summarizer.

**Step 2: Implement real summarization at stage 2 (60-70%)**

Replace the placeholder `summarizeOldHistory` with actual LLM summarization when a summarizer is available:

```go
func (c *ContextCompressor) summarizeOldHistory(ctx context.Context, messages []ChatMessage) []ChatMessage {
    if c.summarizer != nil {
        summarized, err := c.summarizeWithLLM(ctx, messages)
        if err != nil {
            c.logger.Warn("LLM summarization failed, falling back to tail-keep", "error", err)
        } else {
            return summarized
        }
    }
    // Fallback: keep system + critical + last 4
    return keepTail(messages, 4)
}

func (c *ContextCompressor) summarizeWithLLM(ctx context.Context, messages []ChatMessage) ([]ChatMessage, error) {
    // Separate system messages and the tail we want to keep intact
    var systemMsgs, nonSystemMsgs []ChatMessage
    for _, msg := range messages {
        if msg.Role == RoleSystem {
            systemMsgs = append(systemMsgs, msg)
        } else {
            nonSystemMsgs = append(nonSystemMsgs, msg)
        }
    }

    // Keep last 4 messages as-is (the "recent" window)
    keepCount := 4
    if len(nonSystemMsgs) <= keepCount {
        return messages, nil // Nothing to summarize
    }

    tailStart := len(nonSystemMsgs) - keepCount
    toSummarize := nonSystemMsgs[:tailStart]
    tail := nonSystemMsgs[tailStart:]

    // Build a summarization prompt
    var sb strings.Builder
    sb.WriteString("Summarize the following conversation history into a concise summary that preserves:\n")
    sb.WriteString("- Key decisions made\n")
    sb.WriteString("- Important file paths mentioned\n")
    sb.WriteString("- Unresolved questions\n")
    sb.WriteString("- Current task status\n\n")
    for _, msg := range toSummarize {
        sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
    }

    summaryPrompt := ChatMessage{Role: RoleUser, Content: sb.String()}
    resp, err := c.summarizer.Chat(ctx, append(systemMsgs, summaryPrompt))
    if err != nil {
        return nil, fmt.Errorf("summarizer chat failed: %w", err)
    }

    // Build the compressed message list:
    // system messages + summary message + tail
    summaryMsg := ChatMessage{
        Role:      RoleSystem,
        Content:   fmt.Sprintf("[Conversation Summary]\n%s", resp.Content),
        Critical:  true,
    }

    result := make([]ChatMessage, 0, len(systemMsgs)+1+len(tail))
    result = append(result, systemMsgs...)
    result = append(result, summaryMsg)
    result = append(result, tail...)
    return result, nil
}
```

**Step 3: Differentiate aggressive stage (70-80%) from hard limit (80%+)**

Make `aggressiveCompress()` use importance-based selection instead of plain `keepTail(2)`:

```go
func (c *ContextCompressor) aggressiveCompress(_ context.Context, messages []ChatMessage) []ChatMessage {
    // Keep: system + critical + last 4 messages
    // Drop: non-critical messages outside the tail window
    var systemMsgs, nonSystemMsgs []ChatMessage
    for _, msg := range messages {
        if msg.Role == RoleSystem {
            systemMsgs = append(systemMsgs, msg)
        } else {
            nonSystemMsgs = append(nonSystemMsgs, msg)
        }
    }

    keepCount := 4
    tailStart := len(nonSystemMsgs) - keepCount
    if tailStart < 0 {
        tailStart = 0
    }

    keepSet := make(map[int]bool)
    // Keep the tail
    for i := tailStart; i < len(nonSystemMsgs); i++ {
        keepSet[i] = true
    }
    // Keep critical messages outside the tail
    for i := range nonSystemMsgs[:tailStart] {
        if nonSystemMsgs[i].Critical {
            keepSet[i] = true
        }
    }

    result := make([]ChatMessage, 0, len(systemMsgs)+len(keepSet))
    result = append(result, systemMsgs...)
    for i, msg := range nonSystemMsgs {
        if keepSet[i] {
            result = append(result, msg)
        }
    }
    return result
}
```

Leave `dropOldContext()` as `keepTail(2)` — this is the hard-limit escape hatch.

**Step 4: Wire the summarizer in ContextFirewall**

In `ContextFirewall.Initialize()` (or wherever the compressor is created), pass the model:

```go
if f.config.ProactiveCompression {
    f.compressor = NewContextCompressor(
        CompressionConfig{...},
        f.tokenizer,
        f.summaryModel,  // Pass the LLM for summarization
        f.logger,
    )
}
```

**Step 5: Add tests**

- `TestSummarizeOldHistory_WithLLM`: Mock the `Chatter` to return a known summary, verify the output contains the summary + tail messages
- `TestSummarizeOldHistory_FallbackOnLLMError`: Mock the `Chatter` to return an error, verify fallback to `keepTail(4)`
- `TestAggressiveCompress_PreservesCritical`: Verify critical messages outside the tail are retained
- `TestCompressionStages_Differentiate`: Verify that stage 2 (summarize) produces different output than stage 3 (aggressive) and stage 4 (hard limit)

#### Files to Modify

| File | Changes |
|------|---------|
| `internal/llm/context_compressor.go` | Add `summarizer` field, implement `summarizeWithLLM()`, differentiate `aggressiveCompress()` |
| `internal/llm/context_firewall.go` | Pass `summaryModel` when creating compressor |
| `internal/llm/context_compressor_test.go` | New tests for LLM summarization, importance-based compression, stage differentiation |

---

### Observation 3: `ParseSummarizeResponse` Has Fragile JSON Extraction

**File:** `internal/memory/consolidation.go` (lines 563-584)
**Severity:** Medium

#### Problem

`ParseSummarizeResponse` uses a naive prefix/suffix markdown fence stripper that only handles the ideal case where the entire LLM response is a single fenced JSON block with no surrounding text. The codebase already contains two more robust JSON extraction patterns:

1. **`extractJSON`** in `internal/agent/strategic.go` (lines 402-448) — multi-strategy approach with `strings.Index`, brace extraction, and `json.Valid()` validation
2. **`extractJSONFromLLM`** in `internal/agent/llm_classifier.go` (lines 295-310) — brace/bracket finder that locates JSON delimiters anywhere in text

**Failure modes of the current implementation:**

| LLM Response Pattern | Current Behavior |
|----------------------|-----------------|
| `` ```json\n[...]\n``` `` | Works correctly |
| `Here are summaries:\n```json\n[...]\n```\nDone!` | **Fails** — opening fence not at start |
| ```` ```json\n```json\n[...]\n```\n``` ```` | **Fails** — nested fences not stripped |
| `` ```json\n[...]\n```\nExtra text`` | **Fails** — closing fence not at end |
| `[...]` (bare JSON, no fences) | Works (falls through to `json.Unmarshal`) |
| `Here: [...]` (JSON in prose) | **Fails** — `json.Unmarshal` on full text |

When parsing fails, the system silently falls back to `summarizeByDate()` — date-based grouping without LLM intelligence. This means LLM-powered semantic consolidation degrades invisibly whenever the LLM adds conversational framing.

The only existing test (`TestSummarizeWithLLM_MarkdownFencedResponse`) covers the clean case with no prose.

#### Remediation Plan

**Step 1: Replace `ParseSummarizeResponse` with a robust multi-strategy parser**

The new implementation should try strategies in order of specificity:

```go
func ParseSummarizeResponse(content string) ([]Summary, error) {
    content = strings.TrimSpace(content)

    // Strategy 1: Direct JSON parse (fastest path for clean responses)
    var summaries []Summary
    if err := json.Unmarshal([]byte(content), &summaries); err == nil {
        return summaries, nil
    }

    // Strategy 2: Extract from markdown code fences (anywhere in text)
    if extracted := extractJSONFromFences(content); extracted != "" {
        if err := json.Unmarshal([]byte(extracted), &summaries); err == nil {
            return summaries, nil
        }
    }

    // Strategy 3: Find JSON array via bracket matching
    if extracted := extractJSONArray(content); extracted != "" {
        if err := json.Unmarshal([]byte(extracted), &summaries); err == nil {
            return summaries, nil
        }
    }

    return nil, fmt.Errorf("failed to parse summaries from LLM response: no valid JSON array found")
}

// extractJSONFromFences finds JSON inside markdown code fences anywhere in the text.
func extractJSONFromFences(content string) string {
    // Try ```json fences first
    jsonFence := "```json"
    idx := strings.Index(content, jsonFence)
    if idx != -1 {
        after := content[idx+len(jsonFence):]
        // Find closing fence
        closeIdx := strings.Index(after, "```")
        if closeIdx != -1 {
            candidate := strings.TrimSpace(after[:closeIdx])
            if json.Valid([]byte(candidate)) {
                return candidate
            }
        }
    }

    // Try generic ``` fences
    genericFence := "```"
    idx = strings.Index(content, genericFence)
    if idx != -1 {
        after := content[idx+len(genericFence):]
        // Skip language tag line if present
        if newlineIdx := strings.Index(after, "\n"); newlineIdx != -1 {
            after = after[newlineIdx+1:]
        }
        closeIdx := strings.Index(after, genericFence)
        if closeIdx != -1 {
            candidate := strings.TrimSpace(after[:closeIdx])
            if json.Valid([]byte(candidate)) {
                return candidate
            }
        }
    }

    return ""
}

// extractJSONArray finds a JSON array in text by bracket matching.
func extractJSONArray(content string) string {
    start := strings.Index(content, "[")
    if start == -1 {
        return ""
    }

    // Find the matching closing bracket
    depth := 0
    for i := start; i < len(content); i++ {
        switch content[i] {
        case '[':
            depth++
        case ']':
            depth--
            if depth == 0 {
                candidate := content[start : i+1]
                if json.Valid([]byte(candidate)) {
                    return candidate
                }
                // Keep looking for the next opening bracket
                nextStart := strings.Index(content[i+1:], "[")
                if nextStart == -1 {
                    return ""
                }
                return extractJSONArray(content[i+1+nextStart:])
            }
        }
    }
    return ""
}
```

**Step 2: Add comprehensive test coverage**

```go
func TestParseSummarizeResponse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantLen int
    }{
        {
            name:    "clean JSON array",
            input:   `[{"topic":"test","summary":"s","ids":["1"]}]`,
            wantLen: 1,
        },
        {
            name:    "fenced with json tag",
            input:   "```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```",
            wantLen: 1,
        },
        {
            name:    "prose before fence",
            input:   "Here are the summaries:\n\n```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```\n",
            wantLen: 1,
        },
        {
            name:    "prose before and after fence",
            input:   "Sure, here you go:\n```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```\nHope that helps!",
            wantLen: 1,
        },
        {
            name:    "bare JSON in prose",
            input:   "The result is [{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}] as requested.",
            wantLen: 1,
        },
        {
            name:    "nested fences",
            input:   "```json\n```json\n[{\"topic\":\"test\",\"summary\":\"s\",\"ids\":[\"1\"]}]\n```\n```",
            wantLen: 1,
        },
        {
            name:    "multiple summaries",
            input:   "```json\n[{\"topic\":\"a\",\"summary\":\"sa\",\"ids\":[\"1\"]},{\"topic\":\"b\",\"summary\":\"sb\",\"ids\":[\"2\"]}]\n```",
            wantLen: 2,
        },
        {
            name:    "empty string",
            input:   "",
            wantLen: 0, // should error
        },
        {
            name:    "no JSON at all",
            input:   "I couldn't summarize those memories.",
            wantLen: 0, // should error
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := ParseSummarizeResponse(tt.input)
            if tt.wantLen == 0 {
                if err == nil {
                    t.Error("expected error for invalid input")
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if len(result) != tt.wantLen {
                t.Errorf("got %d summaries, want %d", len(result), tt.wantLen)
            }
        })
    }
}
```

**Step 3: Consider extracting a shared `extractJSON` utility**

Both `internal/agent/strategic.go` and this fix implement similar JSON extraction. Consider moving the robust version to `internal/agent/jsonutil/` or `internal/llm/jsonutil/` as a shared package to avoid duplication. This is optional and can be done as a follow-up.

#### Files to Modify

| File | Changes |
|------|---------|
| `internal/memory/consolidation.go` | Replace `ParseSummarizeResponse` with multi-strategy parser, add `extractJSONFromFences` and `extractJSONArray` helpers |
| `internal/memory/consolidation_test.go` | Add `TestParseSummarizeResponse` table-driven tests covering all failure modes |

---

### Observation 4: Memory Consolidation Disabled for Memvid Backend

**File:** `internal/memory/manager.go` (lines 185-192), `internal/memory/consolidation.go`
**Severity:** Medium

#### Problem

When the memvid backend is active (`useMemvid == true`), the Consolidator is never created (`manager.go:186`), making all consolidation features unavailable:

1. **Access-based expiration** — no stale memory cleanup
2. **Episodic memory consolidation** — no grouping/summarization of old memories
3. **Task deduplication** — no duplicate detection/removal
4. **LLM-based intelligent summarization** — no semantic grouping

The root causes are:

1. **The Consolidator directly accesses SQLite backends**: `consolidateEpisodic()` calls `c.manager.episodic.GetOldMemories()`, `c.manager.episodic.Store()`, `c.manager.episodic.DeleteByIDs()`. When memvid is active, `m.episodic` is `nil`.
2. **Memvid's API lacks consolidation operations**: No `GetOldMemories` (time-range query), no `last_accessed_at` tracking, no `FindDuplicates` (FTS5 similarity), no batch `DeleteByIDs`.
3. **Manager blocks memvid deletion**: `DeleteByID()` at line 1374 returns an error for memvid, even though the memvid client has a `Delete()` method.

#### Remediation Plan

**Step 1: Extract a `ConsolidationBackend` interface**

Create an interface that abstracts the operations the Consolidator needs, so it can work with either SQLite or memvid:

```go
// In internal/memory/types.go

// ConsolidationBackend provides the data operations needed for memory consolidation.
type ConsolidationBackend interface {
    // GetOldMemories returns memories older than the cutoff time.
    GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error)

    // GetExpiredMemories returns memories not accessed since the cutoff time.
    GetExpiredMemories(ctx context.Context, notAccessedSince time.Time) ([]MemoryResult, error)

    // StoreSummary persists a consolidated summary.
    StoreSummary(ctx context.Context, category, content string, metadata map[string]string) (string, error)

    // DeleteByIDs removes memories by their IDs.
    DeleteByIDs(ctx context.Context, ids []string) error

    // FindDuplicates finds groups of similar memories.
    FindDuplicates(ctx context.Context, threshold float64) ([][]string, error)
}
```

**Step 2: Implement `SQLiteConsolidationBackend`**

Wrap the existing SQLite-specific operations:

```go
// In internal/memory/consolidation_backend.go

type SQLiteConsolidationBackend struct {
    episodic *EpisodicMemory
    task     *TaskMemory
}

func NewSQLiteConsolidationBackend(episodic *EpisodicMemory, task *TaskMemory) *SQLiteConsolidationBackend {
    return &SQLiteConsolidationBackend{episodic: episodic, task: task}
}

func (b *SQLiteConsolidationBackend) GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
    if b.episodic == nil {
        return nil, errors.New("episodic memory not available")
    }
    return b.episodic.GetOldMemories(ctx, olderThan, limit)
}

func (b *SQLiteConsolidationBackend) GetExpiredMemories(ctx context.Context, notAccessedSince time.Time) ([]MemoryResult, error) {
    // Existing SQLite logic from EpisodicMemory
}

func (b *SQLiteConsolidationBackend) StoreSummary(ctx context.Context, category, content string, metadata map[string]string) (string, error) {
    if b.episodic == nil {
        return "", errors.New("episodic memory not available")
    }
    return b.episodic.Store(ctx, content, category, metadata)
}

func (b *SQLiteConsolidationBackend) DeleteByIDs(ctx context.Context, ids []string) error {
    // Use EpisodicMemory.DeleteByIDs with batch support
}

func (b *SQLiteConsolidationBackend) FindDuplicates(ctx context.Context, threshold float64) ([][]string, error) {
    if b.task == nil {
        return nil, errors.New("task memory not available")
    }
    return b.task.FindDuplicates(ctx, threshold)
}
```

**Step 3: Implement `MemvidConsolidationBackend`**

Bridge memvid's API to the `ConsolidationBackend` interface:

```go
// In internal/memory/memvid/consolidation_backend.go

type MemvidConsolidationBackend struct {
    client *Client
    zone   string
}

func NewMemvidConsolidationBackend(client *Client, zone string) *MemvidConsolidationBackend {
    return &MemvidConsolidationBackend{client: client, zone: zone}
}

func (b *MemvidConsolidationBackend) GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
    // Memvid doesn't have time-range queries. Use Search with a broad query
    // and filter results client-side by created_at.
    //
    // Strategy: search for all memories (empty or broad query), filter by date.
    // This is a limitation — at scale, this needs a server-side endpoint.
    results, err := b.client.WithZone(b.zone).Search(ctx, "", limit)
    if err != nil {
        return nil, err
    }

    var filtered []MemoryResult
    for _, r := range results {
        if r.CreatedAt.Before(olderThan) {
            filtered = append(filtered, MemoryResult{
                ID:        r.ID,
                Content:   r.Content,
                Category:  r.Metadata["category"],
                CreatedAt: r.CreatedAt,
            })
        }
    }
    return filtered, nil
}

func (b *MemvidConsolidationBackend) GetExpiredMemories(ctx context.Context, notAccessedSince time.Time) ([]MemoryResult, error) {
    // Memvid doesn't track last_accessed_at. This operation returns an error
    // indicating access-based expiration is not available. The Consolidator
    // should skip this step for memvid backends.
    return nil, errors.New("access-based expiration not supported by memvid backend")
}

func (b *MemvidConsolidationBackend) StoreSummary(ctx context.Context, category, content string, metadata map[string]string) (string, error) {
    if metadata == nil {
        metadata = make(map[string]string)
    }
    metadata["category"] = category
    metadata["type"] = "consolidation_summary"
    return b.client.WithZone(b.zone).Store(ctx, content, metadata)
}

func (b *MemvidConsolidationBackend) DeleteByIDs(ctx context.Context, ids []string) error {
    var errs []error
    for _, id := range ids {
        if err := b.client.Delete(ctx, id); err != nil {
            errs = append(errs, err)
        }
    }
    return errors.Join(errs...)
}

func (b *MemvidConsolidationBackend) FindDuplicates(ctx context.Context, threshold float64) ([][]string, error) {
    // Memvid doesn't have FTS5-style similarity search.
    // Use embedding-based similarity when available, or skip.
    return nil, errors.New("duplicate finding not supported by memvid backend")
}
```

**Step 4: Update Consolidator to use the backend interface**

Replace direct SQLite access with the interface:

```go
// In internal/memory/consolidation.go

type Consolidator struct {
    backend ConsolidationBackend
    llm     Chatter
    logger  *slog.Logger
    config  ConsolidatorConfig
    // ... existing fields
}

func NewConsolidator(cfg ConsolidatorConfig) *Consolidator {
    return &Consolidator{
        backend: cfg.Backend,
        llm:     cfg.LLM,
        logger:  cfg.Logger,
        config:  cfg,
    }
}
```

Update `consolidateEpisodic()` to use `c.backend.GetOldMemories()`, `c.backend.StoreSummary()`, `c.backend.DeleteByIDs()`. Update `deduplicateTasks()` to use `c.backend.FindDuplicates()`. Update `runAccessBasedExpiration()` to handle the case where `GetExpiredMemories` returns an error (skip that phase gracefully).

**Step 5: Update Manager to create the appropriate backend**

```go
// In internal/memory/manager.go Initialize()

if m.useMemvid {
    m.consolidator = NewConsolidator(ConsolidatorConfig{
        Backend: NewMemvidConsolidationBackend(m.memvid, "episodic"),
        Logger:  m.logger.With("subsystem", "consolidator"),
        LLM:     m.llm,
    })
} else {
    m.consolidator = NewConsolidator(ConsolidatorConfig{
        Backend: NewSQLiteConsolidationBackend(m.episodic, m.task),
        Logger:  m.logger.With("subsystem", "consolidator"),
        LLM:     m.llm,
    })
}
```

**Step 6: Remove Manager's memvid deletion block**

In `Manager.DeleteByID()` (line 1374), remove the memvid guard since the Consolidator now properly routes through the backend:

```go
// Before:
if useMemvid && m.memvid != nil {
    return errors.New("delete by ID not supported for memvid backend")
}

// After: delegate to the backend via Consolidator or direct memvid client call
```

**Step 7: Handle partial capability gracefully**

The Consolidator should skip operations that the backend doesn't support rather than failing:

```go
func (c *Consolidator) runAccessBasedExpiration(ctx context.Context, cfg ExpirationConfig) (expired, summarized int, err error) {
    memories, err := c.backend.GetExpiredMemories(ctx, cfg.NotAccessedSince)
    if err != nil {
        // Backend doesn't support access-based expiration — skip silently
        c.logger.Debug("skipping access-based expiration", "reason", err.Error())
        return 0, 0, nil
    }
    // ... rest of existing logic
}
```

Similarly for `deduplicateTasks()` — if `FindDuplicates` returns an error, log and skip.

**Step 8: Add tests**

- `TestMemvidConsolidationBackend_StoreSummary`: Verify summary storage through memvid client
- `TestMemvidConsolidationBackend_GetOldMemories`: Verify time-range filtering
- `TestMemvidConsolidationBackend_PartialCapability`: Verify graceful degradation when `GetExpiredMemories` returns an error
- `TestConsolidator_MemvidBackend`: Integration test with mock memvid client, verifying consolidation completes for supported operations
- `TestConsolidator_SkipsUnsupportedOps`: Verify that unsupported operations are logged and skipped, not fatal

#### Files to Create

| File | Purpose |
|------|---------|
| `internal/memory/consolidation_backend.go` | `ConsolidationBackend` interface and `SQLiteConsolidationBackend` |
| `internal/memory/memvid/consolidation_backend.go` | `MemvidConsolidationBackend` implementation |

#### Files to Modify

| File | Changes |
|------|---------|
| `internal/memory/types.go` | Add `ConsolidationBackend` interface |
| `internal/memory/consolidation.go` | Use `ConsolidationBackend` instead of direct SQLite access; skip unsupported ops gracefully |
| `internal/memory/manager.go` | Create appropriate backend based on `useMemvid`; remove memvid deletion block |
| `internal/memory/consolidation_test.go` | Tests for backend abstraction, memvid consolidation, graceful degradation |
| `internal/memory/memvid/client.go` | No changes needed — `Delete()` already exists |

---

### Observation 5: No AST-Level Unit Tests for `CompressCodeAtBoundaries`

**File:** `internal/code/ast/parser.go` (lines 277-430)
**Severity:** Medium

#### Problem

`CompressCodeAtBoundaries` is a 154-line function with significant language-specific branching logic, but has **no dedicated test file** in `internal/code/ast/`. Tests only exist in `internal/agent/executor_code_aware_test.go` which tests the higher-level `compressCodeResult` wrapper, not the AST-level boundary detection directly.

**Untested edge cases:**

1. **Nested functions** (Go: function inside function, Python: nested def)
2. **Anonymous functions / closures** (Go: `func() {}`, JS: arrow functions)
3. **Go interfaces with methods** (method signatures without bodies)
4. **Rust impl blocks** (methods inside impl)
5. **Python class with nested methods** (both class and method bodies should compress)
6. **Code where body ranges overlap or are adjacent**
7. **Empty function bodies** (should not be compressed — no savings)
8. **Syntax errors in source** (should fall back to truncation)
9. **Unicode/multibyte content** in function bodies
10. **Very small `maxChars` values** (0, 1, negative)

Additionally, the helper functions `collectBodyRanges`, `isBodyHolder`, and `findChildByType` have no direct test coverage.

#### Current Test Coverage

| What's Tested | Where |
|--------------|-------|
| `looksLikeCode` | `internal/agent/executor_code_aware_test.go:8-74` |
| `compressCodeResult` (Go) | Same file, lines 76-145 |
| `compressCodeResult` (Python) | Same file, lines 173-217 |
| `compressCodeResult` (short code) | Same file, lines 147-155 |
| `compressCodeResult` (unknown lang) | Same file, lines 157-171 |
| `detectLanguageFromContent` | Same file, lines 219-275 |
| `ToCompressedJSON` (code) | Same file, lines 277-307 |
| `ToCompressedJSON` (non-code) | Same file, lines 309-330 |
| **`CompressCodeAtBoundaries` directly** | **Not tested** |
| **`collectBodyRanges`** | **Not tested** |
| **`isBodyHolder`** | **Not tested** |
| **Edge cases (nested, anonymous, interfaces)** | **Not tested** |

#### Remediation Plan

**Step 1: Create `internal/code/ast/parser_test.go`**

This test file should directly test `CompressCodeAtBoundaries` and its helpers with a comprehensive table of edge cases:

```go
package ast

import (
    "strings"
    "testing"
)

func TestCompressCodeAtBoundaries_Basic(t *testing.T) {
    // Test that basic Go code is compressed correctly
    // Test that short code is returned as-is
    // Test that unknown language falls back to truncation
}

func TestCompressCodeAtBoundaries_Go(t *testing.T) {
    tests := []struct{
        name     string
        source   string
        maxChars int
        check    func(t *testing.T, result string)
    }{
        {
            name: "nested function",
            source: `package main

func outer() {
    inner := func() {
        // long body
        ` + strings.Repeat("fmt.Println(\"hello\")\n", 50) + `
    }
    inner()
}`,
            maxChars: 200,
            check: func(t *testing.T, result string) {
                if !strings.Contains(result, "package main") {
                    t.Error("should preserve package declaration")
                }
                if !strings.Contains(result, "func outer()") {
                    t.Error("should preserve outer function signature")
                }
                if !strings.Contains(result, "...[compressed]") {
                    t.Error("should compress function bodies")
                }
            },
        },
        {
            name: "interface with methods — signatures preserved",
            source: `package main

type Handler interface {
    Handle(msg string) error
    Close() error
}

type Server struct {
    Handler Handler
}

func (s *Server) Start() error {
    ` + strings.Repeat("fmt.Println(\"starting\")\n", 50) + `
    return nil
}`,
            maxChars: 300,
            check: func(t *testing.T, result string) {
                if !strings.Contains(result, "Handler interface") {
                    t.Error("should preserve interface definition")
                }
                if !strings.Contains(result, "Handle(msg string)") {
                    t.Error("should preserve interface method signatures")
                }
                if !strings.Contains(result, "...[compressed]") {
                    t.Error("should compress Server.Start body")
                }
            },
        },
        {
            name: "func literal / closure",
            source: `package main

func process() {
    fn := func(x int) int {
        ` + strings.Repeat("x += 1\n", 100) + `
        return x
    }
    fn(42)
}`,
            maxChars: 200,
            check: func(t *testing.T, result string) {
                if !strings.Contains(result, "...[compressed]") {
                    t.Error("should compress func literal bodies")
                }
            },
        },
        {
            name: "empty function body — no compression marker",
            source: `package main

func empty() {}
func alsoEmpty() { }
func main() {
    empty()
    alsoEmpty()
}`,
            maxChars: 500,
            check: func(t *testing.T, result string) {
                // Empty bodies should not get compression markers
                // (body size > 2 check in collectBodyRanges)
            },
        },
    }
    // ... run tests
}

func TestCompressCodeAtBoundaries_Python(t *testing.T) {
    tests := []struct{ ... }{
        {
            name: "class with nested methods",
            source: `class Processor:
    def __init__(self):
        ` + strings.Repeat("self.data.append(i)\n", 100) + `

    def process(self, items):
        ` + strings.Repeat("result.append(self.transform(item))\n", 100) + `

    def transform(self, item):
        return item * 2
`,
            maxChars: 300,
            check: func(t *testing.T, result string) {
                if !strings.Contains(result, "class Processor") {
                    t.Error("should preserve class definition")
                }
                if !strings.Contains(result, "...[compressed]") {
                    t.Error("should compress method bodies")
                }
            },
        },
        {
            name: "nested function definitions",
            source: ...,
        },
    }
}

func TestCompressCodeAtBoundaries_TypeScript(t *testing.T) {
    // Test arrow functions, class methods, function expressions
}

func TestCompressCodeAtBoundaries_Rust(t *testing.T) {
    // Test fn, impl blocks
}

func TestCompressCodeAtBoundaries_EdgeCases(t *testing.T) {
    tests := []struct{ ... }{
        {
            name:     "zero maxChars",
            source:   "package main\n\nfunc hello() { fmt.Println(\"hi\") }",
            maxChars: 0,
            check: func(t *testing.T, result string) {
                if result != "" {
                    t.Errorf("expected empty string for maxChars=0, got %q", result)
                }
            },
        },
        {
            name:     "negative maxChars",
            source:   "package main",
            maxChars: -1,
            check: func(t *testing.T, result string) {
                if result != "" {
                    t.Errorf("expected empty string for negative maxChars, got %q", result)
                }
            },
        },
        {
            name:     "syntax error — fallback to truncation",
            source:   "package main\n\nfunc (broken syntax { {{ }}}",
            maxChars: 50,
            check: func(t *testing.T, result string) {
                if !strings.Contains(result, "...[truncated") {
                    t.Error("syntax errors should fall back to truncation")
                }
            },
        },
        {
            name:     "source fits in budget — no compression",
            source:   "package main\n\nfunc hello() { return 42 }",
            maxChars: 1000,
            check: func(t *testing.T, result string) {
                if strings.Contains(result, "...[compressed]") {
                    t.Error("should not compress when source fits in budget")
                }
            },
        },
        {
            name: "multiple adjacent function bodies",
            source: `package main
func a() { ` + strings.Repeat("x", 200) + ` }
func b() { ` + strings.Repeat("y", 200) + ` }
func c() { ` + strings.Repeat("z", 200) + ` }`,
            maxChars: 200,
            check: func(t *testing.T, result string) {
                // All three bodies should be compressed
                count := strings.Count(result, "...[compressed]")
                if count < 3 {
                    t.Errorf("expected at least 3 compression markers, got %d", count)
                }
            },
        },
    }
}

func TestIsBodyHolder(t *testing.T) {
    // Test each language's body-holder node types
    // Use tree-sitter to create real nodes and verify isBodyHolder returns correctly
}

func TestCollectBodyRanges(t *testing.T) {
    // Test that ranges are collected in correct order
    // Test that empty bodies are excluded
    // Test that nested function bodies are handled correctly
}
```

**Step 2: Run tests and verify**

```bash
go test ./internal/code/ast/... -v
```

#### Files to Create

| File | Purpose |
|------|---------|
| `internal/code/ast/parser_test.go` | Comprehensive tests for `CompressCodeAtBoundaries`, `collectBodyRanges`, `isBodyHolder`, `findChildByType` |

---

### Observation 6: `compressMapResult` Does Not Use Code-Aware Compression

**File:** `internal/agent/executor.go` (lines 158-188)
**Severity:** Low

#### Problem

`compressMapResult` compresses map values by truncating string values at character boundaries, even if the values contain source code. The `compressCodeResult` function (which uses `CompressCodeAtBoundaries`) is only applied to bare string results, not to string values within maps.

In practice, most tool results that contain code return it as a bare string, not a map with string values. However, some tool results (e.g., structured responses with `"output": "<code>"` or `"stdout": "<code>"`) could benefit from code-aware compression.

#### Remediation Plan

**Step 1: Add code-awareness to `compressMapResult`**

```go
func compressMapResult(m map[string]any, maxChars int) map[string]any {
    compressed := make(map[string]any)
    totalChars := 0

    for k, v := range m {
        if totalChars >= maxChars {
            compressed["_truncated"] = true
            break
        }

        switch val := v.(type) {
        case string:
            remaining := maxChars - totalChars
            if len(val) > remaining {
                if looksLikeCode(val) {
                    compressed[k] = compressCodeResult(val, remaining)
                } else {
                    compressed[k] = truncateWithMarker(val, remaining)
                }
                totalChars = maxChars
            } else {
                compressed[k] = val
                totalChars += len(val)
            }
        default:
            compressed[k] = v
            if data, err := json.Marshal(v); err == nil {
                totalChars += len(data)
            }
        }
    }

    return compressed
}
```

**Step 2: Add test for code-aware map compression**

```go
func TestCompressMapResult_CodeAware(t *testing.T) {
    goCode := "package main\n\nfunc Hello() {\n\t" +
        strings.Repeat("fmt.Println(\"hello\")\n\t", 50) +
        "}\n"

    m := map[string]any{
        "success": true,
        "output":  goCode,
        "count":   42,
    }

    result := compressMapResult(m, 500)

    if truncated, ok := result["_truncated"]; ok && truncated.(bool) {
        // Even with truncation, code values should get AST compression
        if output, ok := result["output"].(string); ok {
            if strings.Contains(output, "...[compressed]") {
                // Good — code was AST-compressed, not just truncated
            }
        }
    }
}
```

#### Files to Modify

| File | Changes |
|------|---------|
| `internal/agent/executor.go` | Add `looksLikeCode` check in `compressMapResult` string case |
| `internal/agent/executor_code_aware_test.go` | Add `TestCompressMapResult_CodeAware` test |

---

## Minor Observations (Not Requiring Remediation)

1. **Token cache: `EnableCaching` not added to `ModelConfig`.** The plan called for per-model cache control, but caching is controlled globally at the daemon level via `cfg.LLM.Cache.Enabled`. This is arguably simpler and sufficient.

---

## Priority Order for All Fixes

| Priority | Item | Effort | Impact |
|----------|------|--------|--------|
| 1 | Observation 2: Wire LLM summarization into compressor for 60-80% band | Medium | **High** — prevents silent information loss during compression |
| 2 | Observation 3: Robust JSON extraction in `ParseSummarizeResponse` | Low | **Medium** — fixes invisible degradation of LLM consolidation |
| 3 | Gap 5.1: Differentiate compression stages 3 vs 4 | Medium | **Medium** — prevents blind context loss |
| 4 | Observation 4: Enable consolidation for memvid backend via interface | Medium | **Medium** — enables consolidation for memvid users |
| 5 | Gap 3.1: Implement semantic clustering | Medium | **Medium** — enables intelligent consolidation |
| 6 | Gap 2.1: Fix L1 eviction to LRU | Low | **Medium** — prevents eviction of hot entries |
| 7 | Gap 2.4: Add HTTP endpoints for invalidate/inspect | Low | **Medium** — menubar app compatibility |
| 8 | Observation 5: Add AST-level unit tests | Medium | **Medium** — prevents regressions in code compression |
| 9 | Gap 2.2: Fix RPC l2_entries hardcoding | Trivial | **Low** — accurate stats display |
| 10 | Gap 2.3: Record eviction metrics | Trivial | **Low** — eviction visibility |
| 11 | Observation 6: Code-aware map compression | Trivial | **Low** — minor edge case improvement |
| 12 | Gap 5.2: Check plan checkboxes | Trivial | **Low** — documentation hygiene |
