# Agent/Model-Agnostic Token Cache Implementation

## Context

**Problem:** Meept currently has no embedded token cache for LLM API responses. The existing caches (`internal/agent/cache.go` for tool results, `internal/code/ast/cache.go` for AST parsing) do not cache LLM prompt→completion pairs, leading to:
- Unnecessary API costs for repeated queries
- Higher latency due to redundant round-trips
- No offline resilience for repeated queries

**Goal:** Implement an agent/model-agnostic token cache that sits below the agent layer, caching all LLM interactions transparently regardless of provider (Anthropic, OpenAI, etc.) or agent type.

**Inspiration:** The `docs/ouroboros-ideas.md` document identifies "Three-Block Prompt Caching" as a technique Ouroboros has but Meept lacks. This plan extends beyond provider-specific caching to a general embedded solution.

---

## Design Requirements

### R1: Agent/Model Agnostic
- Cache must work transparently for all 8 agents (dispatcher, chat, coder, debugger, planner, analyst, committer, scheduler)
- Must work with any LLM provider (Anthropic, OpenAI, OpenAI-compatible)
- Cache layer sits in `internal/llm/` alongside existing client code

### R2: Multi-Level Caching Strategy
Based on analysis of alternatives:

| Level | Strategy | Hit Rate | Latency | Correctness |
|-------|----------|----------|---------|-------------|
| L1 | Exact-match (in-memory) | 5-15% | <1ms | Perfect |
| L2 | Content-hash (SQLite) | 10-25% | 5-10ms | Perfect |
| L3 | AST-aware (optional) | 15-35% | 10-20ms | Excellent |

**Decision:** Implement L1 + L2 initially. L3 (AST-aware) deferred pending L1/L2 validation.

### R3: File-Aware Cache Keys
For code-focused queries, cache key must include file content hashes to avoid serving stale responses when code changes.

### R4: Configurable Invalidation
- TTL-based expiration (configurable per deployment)
- Manual invalidation via CLI
- Size-based eviction (LRU)

### R5: Observability
- Cache hit/miss metrics exposed via existing metrics system
- CLI commands for cache inspection

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Agent Loop                           │
│  (dispatcher, coder, debugger, etc.)                    │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│              Token Cache Coordinator                    │
│         (internal/llm/token_cache.go)                   │
│  ┌───────────────────────────────────────────────────┐  │
│  │ L1: Exact-Match Cache (in-memory, 10k entries)    │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │ L2: Content-Hash Cache (SQLite, file-aware)       │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│              LLM Client Layer                           │
│    (internal/llm/client.go, anthropic.go)               │
└─────────────────────────────────────────────────────────┘
```

---

## Implementation Plan

### Phase 1: Core Cache Infrastructure (Days 1-2)

**Files to create:**
- `internal/llm/token_cache.go` — Cache coordinator interface
- `internal/llm/token_cache_config.go` — Configuration types
- `internal/llm/token_cache_l1.go` — In-memory exact-match cache
- `internal/llm/token_cache_l2.go` — SQLite content-hash cache
- `internal/llm/token_cache_test.go` — Unit tests

**Key types:**
```go
type TokenCache interface {
    Get(ctx context.Context, key CacheKey) (*CacheEntry, bool)
    Put(ctx context.Context, key CacheKey, response *Response)
    Invalidate(ctx context.Context, key CacheKey)
    Stats() CacheStats
}

type CacheKey struct {
    ModelID      string
    PromptHash   string           // SHA256 of full prompt
    FileHashes   map[string]string // path → content hash (for L2)
    AgentID      string           // optional, for analytics
}

type CacheEntry struct {
    Response   *Response
    CreatedAt  time.Time
    HitCount   int
    FileHashes map[string]string // for staleness detection
}
```

**Reuse existing patterns:**
- `internal/agent/cache.go` — Cache entry structure (`CacheEntry` struct), LRU eviction logic (`evictIfNeeded()`), stats tracking (`CacheStats`), background cleanup goroutine (`cleanupExpired()`)
- `internal/memory/ftstore.go` — SQLite connection pool (`sql.DB`), schema initialization pattern, path expansion (`expandPath()`)
- `internal/code/ast/cache.go` — File modification time validation, LRU order tracking

---

### Phase 2: Integration with LLM Client (Days 3-4)

**Files to modify:**
- `internal/llm/client.go` — Wrap `Chat()` and `ChatWithProgress()` with cache
- `internal/llm/anthropic.go` — Wrap `Chat()` and `ChatWithProgress()` with cache
- `internal/llm/models.go` — Add `EnableCaching bool` to `ModelConfig`
- `internal/llm/broker.go` — Pass cache options to `newChatterFor()`

**Changes:**
```go
// Add ClientOption for cache injection (mirrors WithBudget, WithMetricsStore)
func WithTokenCache(cache TokenCache) ClientOption {
    return func(c *Client) {
        c.tokenCache = cache
    }
}

// In Chat() methods, add cache wrapper:
func (c *Client) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
    key := c.buildCacheKey(messages, opts)

    // Check cache first
    if cached, found := c.tokenCache.Get(key); found {
        c.metricsStore.RecordCacheHit()
        return cached, nil
    }

    // Cache miss - proceed with API call
    resp, err := c.doRequest(...)
    if err == nil {
        c.tokenCache.Put(key, resp)
    }
    return resp, err
}
```

**Integration point:** `internal/llm/broker.go:newChatterFor()` — inject cache into both `NewClient()` and `NewAnthropicClient()` calls, ensuring cache is transparent across all providers.

---

### Phase 3: Content-Hash Extension (Days 5-6)

**Files to create:**
- `internal/llm/cache_key_builder.go` — Extract file references from prompts, compute content hashes

**Logic:**
```go
func (b *CacheKeyBuilder) ExtractFileReferences(prompt string) []string {
    // Patterns to match:
    // - "file: /path/to/file.go"
    // - "/path/to/file.go:42"
    // - "@path/to/file.go"
    // - "function at line X in Y"
}

func (b *CacheKeyBuilder) Build(messages []ChatMessage) CacheKey {
    // 1. Compute prompt hash
    // 2. Extract file references
    // 3. Hash file contents
    // 4. Return composite key
}
```

**Reuse:**
- `internal/code/ast/parser.go` — File parsing utilities
- `internal/tools/file_read.go` — File path pattern matching

---

### Phase 4: CLI Interface (Day 7)

**Files to modify:**
- `cmd/meept/main.go` — Add `cache` subcommand
- `cmd/meept/cache.go` — Cache CLI handler (create new file)

**Commands:**
```bash
./bin/meept cache status       # Show hit/miss rates, entry counts
./bin/meept cache clear        # Clear all cache entries
./bin/meept cache invalidate --path /file.go  # Invalidate entries referencing file
./bin/meept cache inspect --hash <prompt-hash>  # View specific entry
```

**Reuse:**
- `cmd/meept/memory.go` — CLI pattern for memory commands
- `internal/memory/ftstore.go` — SQLite query patterns

---

### Phase 5: Metrics & Observability (Day 8)

**Files to modify:**
- `internal/llm/metrics/store.go` — Add cache hit/miss counters
- `internal/metrics/collector.go` — Expose cache metrics to dashboard

**Metrics to track:**
- `cache_hits_total` (counter)
- `cache_misses_total` (counter)
- `cache_evictions_total` (counter)
- `cache_entry_count` (gauge)
- `cache_hit_rate` (computed: hits / (hits + misses))

---

### Phase 6: Documentation (Day 9)

**Files to create:**
- `docs/concepts/token-caching.md` — Conceptual guide
- `docs/reference/token-cache-cli.md` — CLI reference

**Files to update:**
- `docs/concepts/architecture.md` — Add cache layer to architecture diagram
- `CLAUDE.md` — Add cache to architecture overview table
- `mkdocs.yml` — Add new pages to nav

---

## File Summary

### New Files
| File | Purpose |
|------|---------|
| `internal/llm/token_cache.go` | Cache coordinator interface (`TokenCache` trait) |
| `internal/llm/token_cache_config.go` | Configuration types (`CacheConfig`, TTL, eviction policy) |
| `internal/llm/token_cache_l1.go` | In-memory exact-match cache (mirror `internal/agent/cache.go` patterns) |
| `internal/llm/token_cache_l2.go` | SQLite content-hash cache (reuse `internal/memory/ftstore.go` pool management) |
| `internal/llm/token_cache_test.go` | Unit tests (table-driven, mirroring `internal/llm/client_test.go`) |
| `internal/llm/cache_key_builder.go` | File reference extraction, content hashing |
| `cmd/meept/cache.go` | CLI handler (mirror `cmd/meept/memory.go` cobra pattern) |
| `docs/concepts/token-caching.md` | Conceptual guide |
| `docs/reference/token-cache-cli.md` | CLI reference |

### Modified Files
| File | Changes |
|------|---------|
| `internal/llm/client.go` | Wrap `Chat()` and `ChatWithProgress()` with cache; inject `TokenCache` via `ClientOption` |
| `internal/llm/anthropic.go` | Wrap `Chat()` and `ChatWithProgress()` with cache; inject via `AnthropicClientOption` |
| `internal/llm/models.go` | Add `EnableCaching bool` to `ModelConfig` |
| `internal/llm/broker.go` | Pass cache options to `newChatterFor()`; ensure cache is transparent across providers |
| `internal/metrics/store.go` | Add cache-specific tables (`cache_hits`, `cache_misses`, `cache_evictions`) |
| `internal/metrics/collector.go` | Add `RecordCacheHit()`, `RecordCacheMiss()` methods |
| `cmd/meept/main.go` | Add `newCacheCmd()` registration |
| `docs/concepts/architecture.md` | Add cache layer to architecture diagrams |
| `CLAUDE.md` | Add cache to architecture overview table |
| `mkdocs.yml` | Add nav entries |

---

## Verification

### Unit Tests
```bash
go test ./internal/llm/... -run TokenCache -v
```

### Integration Tests
1. Run daemon with caching enabled
2. Send identical prompts twice → verify second is cached
3. Modify referenced file → verify cache invalidation
4. Check metrics: `./bin/meept cache status`

### End-to-End Test
```bash
# Start daemon
./bin/meept-daemon -f

# In another terminal, send same prompt twice
./bin/meept chat "What is the architecture of Meept?"
./bin/meept chat "What is the architecture of Meept?"  # Should be cached

# Check cache stats
./bin/meept cache status
```

---

## Deferred Enhancements (Post-MVP)

1. **L3: AST-Aware Cache** — Use tree-sitter to normalize code patterns before hashing
2. **Semantic Embedding Cache** — Vector similarity for paraphrased queries (requires embedding model)
3. **Distributed Cache** — Redis backend for multi-instance deployments
4. **Per-Agent Cache Policies** — Different TTLs per agent type

---

## Open Questions

1. **Cache key sensitivity:** Should prompts differing only in whitespace be considered identical? (Normalization trade-off: CPU vs. hit rate)

2. **File reference detection:** How aggressively to extract file paths from prompts? Regex patterns vs. Heuristic parsing vs. LLM-assisted extraction

3. **SQLite vs. in-memory only:** L2 cache adds complexity. Is 10k in-memory entries sufficient for most deployments?

4. **Default TTL:** What's a sensible default? (Options: 5min, 30min, 1h, 24h)

---

## Timeline Summary

| Phase | Description | Duration |
|-------|-------------|----------|
| 1 | Core cache infrastructure | 2 days |
| 2 | LLM client integration | 2 days |
| 3 | Content-hash extension | 2 days |
| 4 | CLI interface | 1 day |
| 5 | Metrics & observability | 1 day |
| 6 | Documentation | 1 day |
| **Total** | | **~9 days** |
