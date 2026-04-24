# Phase 4: Semantic/Embedding Matching

**Status:** Completed
**Priority:** Medium (requires Phase 1)
**Estimated Effort:** 1 sprint
**Completed:** 2026-04-24

---

## Summary

All implementation steps completed:

1. **EmbeddingClient interface created** - Abstraction for embedding providers in `embedding.go`
2. **SnowflakeEmbedClient implemented** - Full HTTP client for Snowflake Arctic Embed API
3. **CosineSimilarity function added** - Vector similarity computation
4. **SemanticIndex created** - Pre-computed intent embeddings in `intent_index.go`
5. **Semantic classifier integrated** - Added to classifyIntent fallback chain between keyword and fallback
6. **DispatcherConfig updated** - Added EmbeddingClient field for optional semantic matching

**Files Created:**
- `internal/agent/embedding.go` - Embedding client interface and Snowflake implementation
- `internal/agent/intent_index.go` - Semantic index with BuildIndex() and Match()

**Files Modified:**
- `internal/agent/dispatcher.go` - Added semanticIndex field, config, initialization, and matching

All tests pass.

---

## Overview

Keyword matching is brittle: "The authentication is completely broken, users can't log in" may miss debug patterns because no exact keyword matches. This phase adds a semantic layer using sentence embeddings to classify intents based on meaning, not substring matches.

**Current State (verified 2026-04-24):**
- Keyword classifier uses `strings.Contains()` at `dispatcher.go:540`
- LLM classifier exists but adds latency
- No embedding-based classification

---

## Problem Statement

### Current Keyword Matching

```
Input:  "The authentication is completely broken, users can't log in"
Keywords matched: "broken" → debug (confidence: 0.4)
Result: Falls through to LLM or chat
```

### Desired Semantic Matching

```
Input:  "The authentication is completely broken, users can't log in"
Semantic match: "debug" (cosine similarity: 0.89)
Result: Routes to debugger with high confidence
```

---

## Objectives

1. **Add embedding client** - Interface with local or hosted embedding model
2. **Build intent embedding index** - Pre-compute embeddings for all intent definitions
3. **Semantic classifier** - Classify by embedding similarity
4. **Hybrid classification** - Combine keyword + LLM + semantic scores

---

## Implementation Steps

### Step 1: Add Embedding Client

**File:** `internal/agent/embedding.go` (NEW)

```go
package agent

import (
    "context"
    "net/http"
    "encoding/json"
)

// EmbeddingClient generates vector embeddings for text.
type EmbeddingClient interface {
    Embed(ctx context.Context, text string) ([]float64, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
    Dimension() int
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float64) float64 {
    if len(a) != len(b) {
        return 0
    }
    var dot, normA, normB float64
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dot / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float64) float64 {
    // Use math.Sqrt
}
```

**Optional: Snowflake Arctic Embed implementation**

```go
// SnowflakeEmbedClient implements EmbeddingClient using Snowflake Arctic.
type SnowflakeEmbedClient struct {
    apiKey    string
    baseURL   string  // "https://api.snowflake.ai/v1/embeddings"
    dimension int     // 1024 for Arctic Embed
}

func NewSnowflakeEmbedClient(apiKey string) *SnowflakeEmbedClient {
    return &SnowflakeEmbedClient{
        apiKey:    apiKey,
        baseURL:   "https://api.snowflake.ai/v1/embeddings",
        dimension: 1024,
    }
}

func (c *SnowflakeEmbedClient) Embed(ctx context.Context, text string) ([]float64, error) {
    req := map[string]any{
        "input": text,
        "model": "snowflake-arctic-embed-m-v1.5",
    }
    // HTTP POST to baseURL
    // Parse response: {"embedding": [0.1, 0.2, ...]}
}
```

### Step 2: Build Intent Embedding Index

**File:** `internal/agent/intent_index.go` (NEW)

```go
package agent

import (
    "context"
    "sync"
)

// IntentEntry represents an intent definition for indexing.
type IntentEntry struct {
    IntentType  IntentType
    Description string
    Keywords    []string
}

// SemanticIndex provides embedding-based intent matching.
type SemanticIndex struct {
    mu       sync.RWMutex
    client   EmbeddingClient
    entries  []IntentEntry
    vectors  [][]float64  // Pre-computed embeddings
    ready    bool
}

// NewSemanticIndex creates a new semantic index.
func NewSemanticIndex(client EmbeddingClient) *SemanticIndex {
    return &SemanticIndex{
        client:  client,
        entries: make([]IntentEntry, 0),
        vectors: make([][]float64, 0),
    }
}

// BuildIndex pre-computes embeddings for all intent definitions.
func (idx *SemanticIndex) BuildIndex(ctx context.Context) error {
    idx.mu.Lock()
    defer idx.mu.Unlock()

    // Build entry texts from IntentRegistry
    for intentType := range IntentRegistry {
        text := buildIntentText(intentType)
        idx.entries = append(idx.entries, IntentEntry{
            IntentType: intentType,
            Description: text,
            Keywords: intentType.Keywords(),
        })
    }

    // Compute embeddings
    texts := make([]string, len(idx.entries))
    for i, e := range idx.entries {
        texts[i] = e.Description
    }

    vectors, err := idx.client.EmbedBatch(ctx, texts)
    if err != nil {
        return err
    }

    idx.vectors = vectors
    idx.ready = true
    return nil
}

func buildIntentText(t IntentType) string {
    return fmt.Sprintf("Intent %s: %s. Keywords: %s",
        string(t),
        t.DefaultAgent(),
        strings.Join(t.Keywords(), ", "))
}

// Match finds the best matching intent by semantic similarity.
func (idx *SemanticIndex) Match(input string, minConfidence float64) *SemanticMatch {
    if !idx.ready {
        return nil
    }

    vector, err := idx.client.Embed(context.Background(), input)
    if err != nil {
        return nil
    }

    var bestMatch *SemanticMatch
    bestSimilarity := 0.0

    for i, intentVector := range idx.vectors {
        sim := CosineSimilarity(vector, intentVector)
        if sim > bestSimilarity {
            bestSimilarity = sim
            bestMatch = &SemanticMatch{
                IntentType: idx.entries[i].IntentType,
                Confidence: sim,
            }
        }
    }

    if bestSimilarity >= minConfidence {
        return bestMatch
    }
    return nil
}

// SemanticMatch holds a semantic matching result.
type SemanticMatch struct {
    IntentType IntentType `json:"intent_type"`
    Confidence float64    `json:"confidence"`
}
```

### Step 3: Integrate Semantic Classifier into Dispatcher

**File:** `internal/agent/dispatcher.go`

**Add to Dispatcher struct:**
```go
type Dispatcher struct {
    // ... existing fields ...
    semanticIndex *SemanticIndex
}
```

**Add to NewDispatcher:**
```go
// After keyword classifier setup:
if embedClient != nil {
    d.semanticIndex = NewSemanticIndex(embedClient)
    if err := d.semanticIndex.BuildIndex(ctx); err != nil {
        logger.Warn("Failed to build semantic index", "error", err)
    }
}
```

**Update classifyIntent (add after keyword classifier):**
```go
// Step 3.5: Semantic matching (before fallback)
if d.semanticIndex != nil && d.semanticIndex.ready {
    match := d.semanticIndex.Match(input, 0.6)
    if match != nil {
        d.stats.recordMethod("semantic")
        d.recordAgent(match.IntentType.DefaultAgent())
        d.recordIntent(string(match.IntentType))

        return &Intent{
            Type: string(match.IntentType),
            Confidence: match.Confidence,
            AgentType: match.IntentType.DefaultAgent(),
            Summary: extractSummary(input),
        }, nil
    }
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `EmbeddingClient` interface | Abstraction for embedding providers |
| `SemanticIndex` | Pre-computed intent embeddings |
| Semantic classifier | Integrated into fallback chain |

---

## Success Criteria

1. Semantic matching catches intents that keyword misses
2. Embedding index builds in < 1 second
3. Per-request latency increase < 100ms (with caching)

---

## Dependencies

- **Phase 1**: Need stats to track semantic match rate

---

## Configuration

Add to `config/meept.toml`:

```toml
[dispatcher]
embedding_provider = "snowflake"  # or "local"
embedding_api_key = "${SNOWFLAKE_API_KEY}"
semantic_threshold = 0.6
```

---

## Next Phase

→ **Phase 5: Context-Aware Classification**
