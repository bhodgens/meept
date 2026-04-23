# Phase 4: Semantic/Embedding Matching

**Status:** Not started
**Priority:** Medium (requires Phase 1)
**Estimated Effort:** 2-3 sprints

---

## Overview

Keyword matching is brittle: "Implement a fix for the broken function that doesn't work right" may miss debug patterns because no exact keyword matches. This phase adds a semantic layer using sentence embeddings to classify intents based on meaning, not substring matches.

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
Semantic match: "debug" (cosine similarity: 0.89 with "fix bugs, diagnose issues")
Result: Routes to debugger with high confidence
```

---

## Objectives

1. **Add embedding client** - Interface with local or hosted embedding model
2. **Build intent embedding index** - Pre-compute embeddings for all intent definitions
3. **Semantic classifier** - Classify by embedding similarity
4. **Hybrid classification** - Combine keyword + LLM + semantic scores
5. **Fallback improvement** - Catch what keyword/LLM miss

---

## Implementation Steps

### Step 1: Add Embedding Client

**Option A: Local embedding (recommended for privacy)**

Use `github.com/tmc/langchaingo/llms/huggingface` for local embeddings.

**Option B: API-based embedding**

Use Snowflake Arctic, OpenAI, or Voyage AI embeddings.

**File:** `internal/agent/embedding.go` (NEW)

```go
package agent

import (
    "context"
    "fmt"
)

// EmbeddingClient generates vector embeddings for text.
type EmbeddingClient interface {
    // Embed returns a vector embedding for the input text.
    Embed(ctx context.Context, text string) ([]float64, error)

    // EmbedBatch returns embeddings for multiple texts.
    EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)

    // Dimension returns the embedding dimension.
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
    // ... or use math.Sqrt
}
```

**Implementation with Snowflake Arctic Embed (via API):**

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
    // Return embedding
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
    Description string  // Full text to embed
    Keywords    []string
    Examples    []string  // Example user queries
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
        client: client,
        entries: make([]IntentEntry, 0),
        vectors: make([][]float64, 0),
    }
}

// BuildIndex pre-computes embeddings for all intent definitions.
func (idx *SemanticIndex) BuildIndex(ctx context.Context) error {
    idx.mu.Lock()
    defer idx.mu.Unlock()

    // Build entry texts from IntentRegistry
    for intentType, def := range IntentRegistry {
        // Combine description + keywords + examples
        text := buildIntentText(intentType, def)
        idx.entries = append(idx.entries, IntentEntry{
            IntentType: intentType,
            Description: text,
            Keywords: def.Keywords,
        })
    }

    // Compute embeddings
    texts := make([]string, len(idx.entries))
    for i, e := range idx.entries {
        texts[i] = e.Description
    }

    vectors, err := idx.client.EmbedBatch(ctx, texts)
    if err != nil {
        return fmt.Errorf("failed to build intent embeddings: %w", err)
    }

    idx.vectors = vectors
    idx.ready = true

    return nil
}

// buildIntentText creates indexable text from intent definition.
func buildIntentText(t IntentType, def IntentDefinition) string {
    // Example:
    // "Intent Code: Write, modify, or create code. Keywords: implement, create function, add feature, refactor"
    return fmt.Sprintf("Intent %s: %s. Keywords: %s",
        string(t),
        def.Description,
        strings.Join(def.Keywords, ", "))
}

// Match finds the best matching intent by semantic similarity.
func (idx *SemanticIndex) Match(ctx context.Context, input string, minConfidence float64) *SemanticMatch {
    if !idx.ready {
        return nil
    }

    // Compute input embedding
    vector, err := idx.client.Embed(ctx, input)
    if err != nil {
        return nil
    }

    // Compare against all intent embeddings
    var bestMatch *SemanticMatch
    bestSimilarity := 0.0

    for i, intentVector := range idx.vectors {
        sim := CosineSimilarity(input, intentVector)
        if sim > bestSimilarity {
            bestSimilarity = sim
            bestMatch = &SemanticMatch{
                IntentType: idx.entries[i].IntentType,
                Confidence: sim,
                Keywords: idx.entries[i].Keywords,
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
    Keywords   []string   `json:"keywords,omitempty"`
}

// MatchAll returns all matches above threshold, sorted by confidence.
func (idx *SemanticIndex) MatchAll(ctx context.Context, input string, limit int) []*SemanticMatch {
    if !idx.ready {
        return nil
    }

    vector, _ := idx.client.Embed(ctx, input)
    matches := make([]*SemanticMatch, 0)

    for i, intentVector := range idx.vectors {
        sim := CosineSimilarity(vector, intentVector)
        if sim >= 0.3 {  // Lower threshold for ranking
            matches = append(matches, &SemanticMatch{
                IntentType: idx.entries[i].IntentType,
                Confidence: sim,
                Keywords: idx.entries[i].Keywords,
            })
        }
    }

    // Sort by confidence descending
    sort.Slice(matches, func(i, j int) bool {
        return matches[i].Confidence > matches[j].Confidence
    })

    if limit > 0 && len(matches) > limit {
        matches = matches[:limit]
    }

    return matches
}
```

### Step 3: Integrate Semantic Classifier into Dispatcher

**File:** `internal/agent/dispatcher.go`

**Changes:**

```go
// Add to Dispatcher struct:
semanticIndex *SemanticIndex

// In NewDispatcher():
d.semanticIndex = NewSemanticIndex(embeddingClient)
if err := d.semanticIndex.BuildIndex(ctx); err != nil {
    logger.Warn("Failed to build semantic index", "error", err)
    // Continue without semantic matching
}

// In classifyIntent(), add semantic matching:
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error) {
    // ... existing capability matcher, LLM, keyword ...

    // Step 3.5: Semantic matching (before fallback)
    if d.semanticIndex != nil && d.semanticIndex.IsReady() {
        match := d.semanticIndex.Match(ctx, input, 0.6)
        if match != nil {
            d.stats.recordMethod("semantic")
            d.stats.recordAgent(match.IntentType.DefaultAgent())
            d.stats.recordIntent(string(match.IntentType))

            return &Intent{
                Type: string(match.IntentType),
                Confidence: match.Confidence,
                AgentType: match.IntentType.DefaultAgent(),
                Summary: extractSummary(input),
            }, nil
        }
        d.stats.recordMethodAttempt("semantic")
    }

    // Step 4: Fallback
    // ...
}
```

### Step 4: Hybrid Classification (Score Fusion)

**File:** `internal/agent/dispatcher.go` (NEW)

```go
// HybridClassifier combines multiple classifiers with score fusion.
type HybridClassifier struct {
    keywordClassifier   *KeywordClassifier
    llmClassifier       *LLMClassifier
    semanticIndex       *SemanticIndex
    capabilityMatcher   *CapabilityMatcher

    // Weights for score fusion (should sum to 1.0)
    KeywordWeight   float64  // 0.2
    LLMWeight       float64  // 0.4
    SemanticWeight  float64  // 0.3
    CapabilityWeight float64 // 0.1
}

// Classify fuses scores from all classifiers.
func (c *HybridClassifier) Classify(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error) {
    scores := make(map[IntentType]ScoredIntent)

    // Run capability matcher
    if capResult := c.capabilityMatcher.Match(input); capResult != nil {
        intentType := IntentType(capResult.IntentType)
        scores[intentType] = ScoredIntent{
            Intent: &Intent{
                Type: string(intentType),
                Confidence: capResult.Confidence,
                AgentType: capResult.AgentID,
            },
            Method: "capability",
            Score: capResult.Confidence * c.CapabilityWeight,
        }
    }

    // Run LLM classifier
    if llmIntent, _ := c.llmClassifier.Classify(ctx, input, context); llmIntent != nil {
        intentType := IntentType(llmIntent.Type)
        scores[intentType] = mergeScore(scores[intentType], ScoredIntent{
            Intent: llmIntent,
            Method: "llm",
            Score: llmIntent.Confidence * c.LLMWeight,
        })
    }

    // Run semantic matching
    if semMatches := c.semanticIndex.MatchAll(ctx, input, 3); len(semMatches) > 0 {
        for _, match := range semMatches {
            intentType := match.IntentType
            existing := scores[intentType]
            merged := ScoredIntent{
                Intent: &Intent{
                    Type: string(intentType),
                    Confidence: match.Confidence,
                    AgentType: intentType.DefaultAgent(),
                },
                Method: "semantic",
                Score: match.Confidence * c.SemanticWeight,
            }
            scores[intentType] = mergeScore(existing, merged)
        }
    }

    // Keyword matching
    if kwIntent, _ := c.keywordClassifier.Classify(ctx, input, context); kwIntent != nil {
        intentType := IntentType(kwIntent.Type)
        scores[intentType] = mergeScore(scores[intentType], ScoredIntent{
            Intent: kwIntent,
            Method: "keyword",
            Score: kwIntent.Confidence * c.KeywordWeight,
        })
    }

    // Find highest scored intent
    var best ScoredIntent
    for _, scored := range scores {
        if scored.Score > best.Score {
            best = scored
        }
    }

    if best.Score >= 0.5 {  // Minimum fusion threshold
        return best.Intent, nil
    }

    return nil, fmt.Errorf("no confident match from hybrid classifier")
}

// ScoredIntent holds an intent with its classification score.
type ScoredIntent struct {
    Intent *Intent
    Method string
    Score  float64
}

// mergeScore combines scores for the same intent (normalized).
func mergeScore(existing, new ScoredIntent) ScoredIntent {
    if existing.Intent == nil {
        return new
    }
    // Weighted average
    combinedScore := (existing.Score + new.Score) / 2
    return ScoredIntent{
        Intent: existing.Intent,  // Keep first
        Method: existing.Method + "+" + new.Method,
        Score: combinedScore,
    }
}
```

### Step 5: Add Embedding Configuration

**File:** `config/meept.toml`

```toml
[dispatcher]
# Semantic matching configuration
embedding_provider = "snowflake"  # "snowflake", "openai", "voyage", "local"
embedding_model = "snowflake-arctic-embed-m-v1.5"
embedding_api_key = "${SNOWFLAKE_API_KEY}"  # Or omit for local
embedding_cache_size = 1000  # Cache recent embeddings
semantic_threshold = 0.6  # Minimum confidence for semantic match
```

### Step 6: Add Stats for Semantic Matching

**File:** `internal/agent/dispatcher.go`

```go
// In DispatcherStats:
SemanticMatches int `json:"semantic_matches"`
SemanticFallbacks int `json:"semantic_fallbacks"`

func (d *Dispatcher) stats.recordSemanticMatch() {
    d.stats.mu.Lock()
    d.stats.SemanticMatches++
    d.stats.mu.Unlock()
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `EmbeddingClient` interface | Abstraction for embedding providers |
| `SemanticIndex` | Pre-computed intent embeddings |
| Semantic classifier | Integrated into fallback chain |
| `HybridClassifier` | Score fusion from all methods |
| Config options | Embedding provider selection |

---

## Success Criteria

1. ✅ Semantic matching catches intents that keyword misses
2. ✅ Hybrid classifier improves overall accuracy
3. ✅ Embedding index builds in < 1 second
4. ✅ Per-request latency increase < 100ms (with caching)

---

## Testing

### Unit Tests

```go
func TestSemanticMatch(t *testing.T) {
    idx := buildTestIndex()
    match := idx.Match(ctx, "The login is broken", 0.5)
    assert.Equal(t, IntentDebug, match.IntentType)
    assert.Greater(t, match.Confidence, 0.6)
}
```

### Integration Tests

```bash
# Test fallback improvement
./bin/meept chat "The authentication flow is completely broken"
# Should route to debugger via semantic matching
```

---

## Dependencies

- **Phase 1**: Need stats to track semantic match rate

---

## Risks

| Risk | Mitigation |
|------|------------|
| Embedding API latency/cost | Cache embeddings, use local model |
| Privacy concerns with API | Default to local embeddings |
| Index staleness | Rebuild on intent definition changes |

---

## Next Phase

→ **Phase 5: Context-Aware Classification**

With semantic matching in place, add memory context weighting for even smarter routing.
