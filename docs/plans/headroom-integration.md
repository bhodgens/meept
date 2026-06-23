# Headroom Integration — Go-Native Context Compression for Meept

**Created:** 2026-06-20
**Status:** ✅ **COMPLETE** — All phases implemented and verified
**Source Analysis:** `/tmp/headroom-review/` (Headroom v0.5.x)

---

## Completed Work

### Configuration Schema (Done)
- [x] Added `AgentCompressionConfig` struct to `internal/config/schema.go:693-734`
- [x] Added `Compression` field to `AgentConfig`
- [x] Added default values in schema defaults
- [x] Config compiles and validates

### Implementation Summary (All Phases Complete)

| Phase | Component | Status | Files |
|-------|-----------|--------|-------|
| Phase 1 | CCR Store Foundation | ✅ Complete | ccr_store.go, ccr_store_sqlite.go, ccr_hash.go, types.go |
| Phase 2 | Compression Algorithms | ✅ Complete | smart_crusher.go, code_compress.go, log_compress.go, search_compress.go |
| Phase 3 | Router & Pipeline | ✅ Complete | router.go, pipeline.go |
| Phase 4 | Configuration | ✅ Complete | schema.go, sections_compression.go, sections.go |
| Phase 5 | MCP Tools | ✅ Complete | compression.go, compression_test.go, manager.go |
| Phase 6 | Agent Loop | ✅ Complete | loop.go (compressionPipeline, CompressToolResult) |
| Phase 7 | Observability | ✅ Complete | collector.go, api_handlers.go, server.go |

**Tests:** All compress package tests pass (`go test ./internal/compress/...`)

---

## Executive Summary

Implement a Go-native context compression layer for Meept, inspired by Headroom's architecture but designed specifically for Meept's existing infrastructure. This integration will provide **60-90% token reduction** on tool outputs, conversation history, and inter-agent messages while maintaining full reversibility via CCR (Compress-Cache-Retrieve).

### Key Design Decisions

1. **Go-native implementation** — No porting Python/Rust code; use idiomatic Go patterns
2. **Leverage existing infrastructure** — Reuse Meept's tree-sitter parsers, SQLite pools, message bus
3. **Feature-flagged rollout** — `use_prompt_compression` config flag for gradual adoption
4. **MCP-first integration** — Start with standalone tools, then wire into agent loop

---

## Architecture Overview

### Target State

```
┌─────────────────────────────────────────────────────────────────────┐
│  Meept Agent Loop                                                   │
│                                                                     │
│  Tool Execution → Compression Pipeline → LLM Request                │
│       │              │                    │                         │
│       │              ├─ ContentRouter     ├─ CacheAligner           │
│       │              ├─ SmartCrusher      ├─ CCR Store              │
│       │              ├─ CodeCompressor    └─ Token Counter          │
│       │              └─ Log/Search Compr.                           │
│       │                                                             │
│       └──────────────→ Compression MCP Tools                        │
│                      - mcc_compress                                 │
│                      - mcc_retrieve                                 │
│                      - mcc_stats                                    │
└─────────────────────────────────────────────────────────────────────┘
```

### Compression Pipeline (Go-Native)

```
Input Messages
      │
      ▼
┌─────────────────┐
│ ContentRouter   │ → Detects: JSON, code, logs, diffs, plain text
└────────┬────────┘
         │
    ┌────┴────┬────────────┬────────────┐
    ▼         ▼            ▼            ▼
┌────────┐ ┌────────┐ ┌────────┐ ┌────────────┐
│ Smart  │ │ Code   │ │ Log/   │ │  Kompress  │
│ Crusher│ │ Compr. │ │ Search │ │  (optional)│
│ (JSON) │ │(tree-s)│ │ Comprs │ │  ML model  │
└────┬───┘ └────┬───┘ └────┬───┘ └─────┬──────┘
     │          │          │           │
     └──────────┴────┬─────┴───────────┘
                     │
                     ▼
            ┌────────────────┐
            │  CCR Store     │ → SQLite with BLAKE3 hashing
            │  + Markers     │ → `<<ccr:HASH>>` injection
            └────────────────┘
```

---

## Implementation Plan

### Phase 1: CCR Store Foundation (Days 1-3)

**Goal:** Implement reversible compression storage layer

#### 1.1 Create `internal/compress/` package structure

```
internal/compress/
├── ccr_store.go          # Interface + shared types
├── ccr_store_sqlite.go   # SQLite backend (reuse memory/ftstore.go patterns)
├── ccr_hash.go           # BLAKE3/SHA-256 content addressing
├── ccr_marker.go         # Marker format: `<<ccr:HASH>>`
├── ccr_marker_test.go    # Marker parsing/generation tests
└── types.go              # CompressionResult, CompressionStats
```

#### 1.2 Implement `CCRStore` interface

```go
// internal/compress/ccr_store.go
type CCRStore interface {
    // Store compressed content with TTL
    Store(ctx context.Context, entry CCREntry) (string, error)  // returns hash

    // Retrieve full original content
    Retrieve(ctx context.Context, hash string) (*CCREntry, error)

    // Search within compressed content (for SmartCrusher results)
    Search(ctx context.Context, hash, query string) ([]CCRSearchResult, error)

    // Check if entry exists (without loading full content)
    Exists(ctx context.Context, hash string) bool

    // Statistics for dashboard
    Stats() CCRStats
}

type CCREntry struct {
    Hash              string
    OriginalContent   string    // full uncompressed content
    CompressedContent string    // compressed version with markers
    OriginalTokens    int
    CompressedTokens  int
    Strategy          string    // "smart_crusher", "code", "log", "search"
    ToolName          string    // which tool produced this
    CreatedAt         time.Time
    TTL               time.Duration
    ExpiresAt         time.Time
    RetrievalCount    int64
}
```

#### 1.3 SQLite schema (reuse existing pool patterns)

```go
// internal/compress/ccr_store_sqlite.go
const ccrSchema = `
CREATE TABLE IF NOT EXISTS ccr_entries (
    hash TEXT PRIMARY KEY,
    original_content TEXT NOT NULL,
    compressed_content TEXT NOT NULL,
    original_tokens INTEGER,
    compressed_tokens INTEGER,
    strategy TEXT,
    tool_name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    retrieval_count INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_ccr_expires ON ccr_entries(expires_at);
CREATE INDEX IF NOT EXISTS idx_ccr_tool ON ccr_entries(tool_name);
`
```

#### 1.4 Content hashing

```go
// internal/compress/ccr_hash.go
// Use BLAKE3 for speed (faster than SHA-256, same collision resistance for this use case)
// Or SHA-256 if we want to avoid new dependencies

func HashContent(content string) string {
    // Return hex-encoded hash, truncated to 24 chars for markers
    // Format: "<<ccr:abc123def456...>>"
}
```

---

### Phase 2: Compression Algorithms (Days 4-12)

**Goal:** Implement Go-native compressors for each content type

#### 2.1 SmartCrusher for JSON/tool outputs

```
internal/compress/
├── smart_crusher.go      # Main JSON compressor
├── smart_crusher_test.go # Parity tests with fixtures
├── array_dedup.go        # Array deduplication logic
├── anomaly_detection.go  # Keep errors, outliers, unique items
└── relevance_scorer.go   # BM25-based relevance (reuse memory/bm25.go)
```

**Key Algorithm:**
```go
// SmartCrusher preserves:
// 1. Array structure and field names
// 2. Error responses and anomalies
// 3. First occurrence of repeated objects
// 4. Items matching user query (relevance scoring)

// SmartCrusher removes:
// 1. Duplicate array elements (by JSON hash)
// 2. Repeated nested objects (replace with reference)
// 3. Low-relevance items (when query provided)
// 4. Whitespace/formatting (canonical JSON)
```

**Implementation approach:**
- Parse JSON with `encoding/json`
- Build hash map of seen objects
- Assign relevance scores based on:
  - Position (first items more important)
  - Anomaly detection (errors, unusual values)
  - Keyword match with user query
- Output compressed JSON with summary header

#### 2.2 Code Compressor (reuse tree-sitter)

```
internal/compress/
├── code_compress.go      # Code compression using tree-sitter
├── code_compress_test.go # Test fixtures per language
└── langs/                # Language-specific compression rules
    ├── go.go
    ├── typescript.go
    ├── python.go
    └── rust.go
```

**Leverage existing code:**
- `internal/code/ast/parser.go` — tree-sitter parser
- `internal/code/ast/symbols.go` — symbol extraction

**Compression strategy:**
```go
// Preserve:
// - Import statements
// - Type definitions
// - Function signatures (name, params, return type)
// - Exported symbols

// Compress:
// - Function bodies (replace with summary comment)
// - Variable initializers
// - Long string literals
```

**Example output:**
```go
// Before: 500 lines
func ProcessData(items []Item) ([]Result, error) {
    // ... 400 lines of implementation
}

// After: 50 lines
func ProcessData(items []Item) ([]Result, error) {
    // [CODE_COMPRESSED: 400→50 lines, 95% saved]
    // Iterates items, validates, transforms to Result.
    // Handles edge cases: nil input, empty slice, invalid items.
    // Returns error on validation failure.
}
```

#### 2.3 Log and Search Result Compressors

```
internal/compress/
├── log_compress.go       # Log compression
├── search_compress.go    # Search/grep results
└── detection/
    ├── log_detector.go   # Detect log file format
    └── diff_detector.go  # Detect unified diff format
```

**Log compression:**
- Keep ERROR, WARN, FATAL lines
- Keep first/last N lines of repetitive blocks
- Replace repeated stack traces with summary
- Preserve timestamps for context

**Search result compression:**
- Group by file
- Show matching lines with context
- Replace non-matching blocks with line ranges

---

### Phase 3: Content Router & Pipeline (Days 13-16)

**Goal:** Wire compressors into unified pipeline

#### 3.1 Content Router

```go
// internal/compress/router.go
type ContentType string

const (
    ContentJSON     ContentType = "json"
    ContentCode     ContentType = "code"
    ContentLogs     ContentType = "logs"
    ContentSearch   ContentType = "search"
    ContentDiff     ContentType = "diff"
    ContentText     ContentType = "text"
    ContentUnknown  ContentType = "unknown"
)

type ContentRouter struct {
    jsonCompressor     *SmartCrusher
    codeCompressor     *CodeCompressor
    logCompressor      *LogCompressor
    searchCompressor   *SearchCompressor
    textCompressor     *TextCompressor  // optional: passthrough or ML
}

func (r *ContentRouter) DetectType(content string) ContentType
func (r *ContentRouter) Compress(ctx context.Context, content string, cfg CompressConfig) (*CompressionResult, error)
```

**Detection heuristics:**
```go
// JSON: Try parsing, check for `{`/`[` at start
// Code: File extension + tree-sitter language detection
// Logs: Regex for timestamp/level patterns
// Search: Grep-style `filename:line:content` format
// Diff: Lines starting with `+++`/`---`/`@@`
// Text: Fallback
```

#### 3.2 Pipeline Orchestration

```go
// internal/compress/pipeline.go
type Pipeline struct {
    router     *ContentRouter
    ccrStore   CCRStore
    aligner    *CacheAligner  // Phase 4
}

type CompressConfig struct {
    MinTokensToCompress int
    ProtectRecent       int
    TargetRatio         float64  // optional: for lossy compressors
    CompressUserMessages bool  // default: false for coding agents
}

func (p *Pipeline) Compress(ctx context.Context, messages []Message, cfg CompressConfig) (*PipelineResult, error)
```

---

### Phase 4: Configuration & Feature Flag (Day 17)

**Goal:** Add `use_prompt_compression` configuration

#### 4.1 Schema update

```go
// internal/config/schema.go — Add to AgentConfig struct

// Compression holds prompt compression settings
Compression AgentCompressionConfig `json:"compression" toml:"compression"`
```

```go
// New struct definition
type AgentCompressionConfig struct {
    // Enabled turns on prompt compression for tool outputs and conversation
    Enabled bool `json:"enabled" toml:"enabled"`
    // MinTokensToCompress is the minimum token count for compression
    MinTokensToCompress int `json:"min_tokens_to_compress" toml:"min_tokens_to_compress"`
    // Strategy is the compression strategy: "smart_crusher", "code", "aggressive"
    Strategy string `json:"strategy" toml:"strategy"`
    // TTL is how long compressed originals are retained
    TTL time.Duration `json:"ttl" toml:"ttl"`
    // LogCompression enables compression for log tool outputs
    LogCompression bool `json:"log_compression" toml:"log_compression"`
    // CodeCompression enables AST-aware code compression
    CodeCompression bool `json:"code_compression" toml:"code_compression"`
    // SearchCompression enables grep/search result compression
    SearchCompression bool `json:"search_compression" toml:"search_compression"`
}
```

#### 4.2 Default config (config/meept.json5)

```json5
{
  agent: {
    compression: {
      enabled: false,  // OFF BY DEFAULT for initial rollout
      min_tokens_to_compress: 500,
      strategy: "smart_crusher",
      ttl: "1h",
      log_compression: true,
      code_compression: true,
      search_compression: true,
    },
  },
}
```

#### 4.3 Config TUI section

```go
// cmd/meept/config_tui.go — Add "compression" section
// Accessible via: meept config compression
```

---

### Phase 5: MCP Tool Integration (Days 18-21)

**Goal:** Expose compression as MCP tools (like Headroom does)

#### 5.1 Implement MCP compression handler

```go
// internal/tools/mcp/compression.go
package mcp

import "github.com/caimlas/meept/internal/compress"

type CompressionHandler struct {
    pipeline *compress.Pipeline
    ccrStore *compress.CCRStore
}

// Register MCP tools
func (h *CompressionHandler) RegisterTools(reg *Registry) {
    reg.AddTool(Tool{
        Name:        "mcc_compress",
        Description: "Compress content to save context tokens",
        Handler:     h.compress,
    })

    reg.AddTool(Tool{
        Name:        "mcc_retrieve",
        Description: "Retrieve original content by hash",
        Handler:     h.retrieve,
    })

    reg.AddTool(Tool{
        Name:        "mcc_stats",
        Description: "Show compression statistics",
        Handler:     h.stats,
    })
}

func (h *CompressionHandler) compress(args json.RawMessage) (string, error)
func (h *CompressionHandler) retrieve(args json.RawMessage) (string, error)
func (h *CompressionHandler) stats(args json.RawMessage) (string, error)
```

#### 5.2 Wire into MCP manager

```go
// internal/tools/mcp/manager.go
// Add CompressionHandler to manager initialization
// Feature-gated by config.Compression.Enabled
```

---

### Phase 6: Agent Loop Integration (Days 22-28)

**Goal:** Automatic compression in agent request flow

#### 6.1 Hook into tool result handling

```go
// internal/agent/loop.go — Find where tool results are added to conversation
// Wire compression before messages are sent to LLM

// Pseudo-code location (find actual tool result handling):
func (a *Agent) handleToolResult(ctx context.Context, result ToolResult) error {
    // ... existing code ...

    // NEW: Compress large tool results
    if a.config.Compression.Enabled && len(result.Output) > a.config.Compression.MinTokensToCompress {
        compressed, err := a.compressionPipeline.Compress(ctx, []Message{
            {Role: "tool", Content: result.Output},
        }, a.config.Compression)
        if err == nil && compressed.TokensSaved > 0 {
            // Replace result.Output with compressed version
            result.Output = compressed.CompressedContent
            // Log compression for metrics
            a.metrics.RecordCompression(compressed.TokensSaved)
        }
    }

    // ... rest of handling ...
}
```

#### 6.2 Pre-request compression

```go
// internal/agent/loop.go — Before LLM call
func (a *Agent) callLLM(ctx context.Context, messages []Message) (*Response, error) {
    // Compress messages if enabled
    if a.config.Compression.Enabled {
        compressed, err := a.compressionPipeline.Compress(ctx, messages, a.config.Compression)
        if err != nil {
            // Fall back to uncompressed — never break on compression failure
            slog.Warn("Compression failed, using uncompressed messages", "error", err)
        } else {
            messages = compressed.Messages
            a.metrics.RecordCompression(compressed.TokensSaved)
        }
    }

    // Proceed with LLM call as normal
    return a.llmClient.Call(ctx, messages)
}
```

#### 6.3 Teach agents to use retrieval

Add to system prompt when compression is enabled:

```
CONTEXT COMPRESSION ACTIVE:
- Large tool outputs are compressed to save context space
- Compressed content shows: `[N items compressed to X tokens, hash=abc123]`
- To retrieve full content, use: mcc_retrieve(hash="abc123")
- Originals are retained for 1 hour
```

---

### Phase 7: Observability & Metrics (Days 29-31)

**Goal:** Track compression effectiveness

#### 7.1 Metrics integration

```go
// internal/metrics/collector.go — Add compression metrics
type CompressionMetrics struct {
    TotalCompressions   int64
    TotalTokensSaved    int64
    AvgCompressionRatio float64
    ByStrategy         map[string]*StrategyMetrics
}
```

#### 7.2 Dashboard endpoint

```go
// internal/comm/http/api_handlers.go
// GET /api/v1/compression/stats
func (s *Server) handleCompressionStats(w http.ResponseWriter, r *http.Request) {
    stats := s.compressionPipeline.Stats()
    json.NewEncoder(w).Encode(CompressionStatsResponse{
        Session: stats,
        Store:   s.ccrStore.Stats(),
    })
}
```

---

## File Creation Checklist

### Phase 1 (CCR Store)
- [ ] `internal/compress/ccr_store.go`
- [ ] `internal/compress/ccr_store_sqlite.go`
- [ ] `internal/compress/ccr_hash.go`
- [ ] `internal/compress/ccr_marker.go`
- [ ] `internal/compress/ccr_marker_test.go`
- [ ] `internal/compress/types.go`
- [ ] `internal/config/schema.go` — Add `AgentCompressionConfig`
- [ ] `config/meept.json5` — Add default compression config

### Phase 2 (Compressors)
- [ ] `internal/compress/smart_crusher.go`
- [ ] `internal/compress/smart_crusher_test.go`
- [ ] `internal/compress/array_dedup.go`
- [ ] `internal/compress/anomaly_detection.go`
- [ ] `internal/compress/code_compress.go`
- [ ] `internal/compress/code_compress_test.go`
- [ ] `internal/compress/log_compress.go`
- [ ] `internal/compress/search_compress.go`
- [ ] `internal/compress/detection/log_detector.go`
- [ ] `internal/compress/detection/diff_detector.go`

### Phase 3 (Router & Pipeline)
- [ ] `internal/compress/router.go`
- [ ] `internal/compress/pipeline.go`
- [ ] `internal/compress/pipeline_test.go`

### Phase 4 (Config)
- [ ] `cmd/meept/config_compression.go` — Config TUI section

### Phase 5 (MCP Tools)
- [ ] `internal/tools/mcp/compression.go`
- [ ] `internal/tools/mcp/compression_test.go`

### Phase 6 (Agent Integration)
- [ ] `internal/agent/loop.go` — Wire compression (location TBD)
- [ ] `internal/agent/compression_integration_test.go`

### Phase 7 (Observability)
- [x] `internal/metrics/collector.go` (`RecordCompression`, `recordCompression`) — compression metrics recorded inline
- [x] `internal/comm/http/compression_handlers.go`

> **Implementation note:** Compression metrics are recorded inline in `collector.go` (`RecordCompression`, `recordCompression` methods at lines 296 and 306) rather than a dedicated `internal/metrics/compression_metrics.go` file. This is intentional — compression events flow through the same collector pipeline as other metrics (LLM calls, tool calls, reviews), and the bus message handler at line 252 dispatches `compress.saved` topics to `recordCompression`. Splitting them into a separate file would add import churn without functional benefit since the `Collector` struct owns the `store` and `logger` dependencies that compression recording requires. The `CompressionMetrics` struct shown in §7.1 is illustrative of the metric dimensions tracked; the actual implementation records per-event tags (`strategy`, `tool_name`, `tokens_before/after/saved`, `hash`) directly via `store.Record` and `store.RecordEvent`.

---

## Testing Strategy

### Unit Tests
- Each compressor with fixture files
- CCR store CRUD operations
- Marker generation/parsing
- Content router detection

### Integration Tests
- End-to-end compression in agent loop
- MCP tool invocation
- Config flag toggling

### Parity Tests (borrowed from Headroom pattern)
```go
// Record expected outputs with test fixtures
// Compare Go output against expected JSON
func TestSmartCrusherParity(t *testing.T) {
    fixtures := loadFixtures("testdata/smart_crusher/*.json")
    for _, f := range fixtures {
        got := smartCrusher.Crush(f.Input)
        assert.JSONEq(t, f.ExpectedOutput, got)
    }
}
```

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Compression loses important context | Inflation guard: if compressed > original, revert |
| CCR store memory leak | TTL-based expiry + periodic cleanup goroutine |
| Agent doesn't know how to retrieve | System prompt injection teaches `mcc_retrieve` |
| Performance overhead | Threshold: don't compress under 500 tokens |
| Silent failures | Feature-flagged rollout, extensive logging |

---

## Success Metrics

1. **Token Savings:** 60%+ reduction on tool outputs
2. **No Regressions:** Zero test failures from compression
3. **Adoption:** Enabled in 50%+ of sessions within 30 days
4. **Retrieval Rate:** <5% of compressed entries retrieved (means compression worked)

---

## Appendix A: Headroom Concepts Mapped to Meept

| Headroom | Meept Equivalent |
|----------|------------------|
| `CompressionStore` | `memory/ftstore.go` patterns |
| `SmartCrusher` | NEW — `compress/smart_crusher.go` |
| `CodeCompressor` | Reuse `code/ast/parser.go` |
| `ContentRouter` | NEW — `compress/router.go` |
| `CCR` (Compress-Cache-Retrieve) | NEW — `compress/ccr_store.go` |
| `CacheAligner` | Defer to Phase 4 |
| `Kompress` (ML model) | Optional — start with rule-based |
| MCP Tools | `tools/mcp/` patterns |
| `headroom learn` | Reuse `memory/` + `selfimprove/` |

---

## Appendix B: Compression Marker Format

```
<<ccr:HASH>>                    # Simple marker (after compressed block)
[N items compressed to X tokens, hash=HASH]     # Verbose marker (tool output)
```

**HASH format:**
- 24 hex characters (BLAKE3 or SHA-256 truncated)
- Example: `abc123def456789012345678`

---

## Appendix C: Config Examples

### Minimal (just enable)
```json5
{
  agent: {
    compression: {
      enabled: true,
    },
  },
}
```

### Conservative (large items only)
```json5
{
  agent: {
    compression: {
      enabled: true,
      min_tokens_to_compress: 1000,
      log_compression: true,
      search_compression: true,
    },
  },
}
```

### Aggressive (maximum savings)
```json5
{
  agent: {
    compression: {
      enabled: true,
      min_tokens_to_compress: 250,
      code_compression: true,
      log_compression: true,
      search_compression: true,
      strategy: "aggressive",
      ttl: "2h",
    },
  },
}
```
