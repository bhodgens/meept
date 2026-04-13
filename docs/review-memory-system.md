# Memory Enhancement System Review

## Executive Summary

Meept implements a sophisticated multi-tiered memory system designed to enable AI agents to learn from interactions, retain context across sessions, and enhance their capabilities over time. This review examines how the system learns from user input, retains memories, accesses stored knowledge, and enhances itself—along with identifying strengths, weaknesses, and gaps in the implementation.

---

## System Overview

### Architecture

The memory system consists of five interconnected layers:

1. **Episodic Memory** - Conversation and interaction history (SQLite + FTS5)
2. **Task Memory** - Domain-specific technical knowledge (SQLite + FTS5)
3. **Knowledge Graph** - Relationship tracking with PageRank scoring
4. **Semantic Memory** - Vector embeddings for similarity search
5. **Personality Memory** - User preferences and interaction patterns (Markdown)
6. **Distributed Memory (memvid)** - Shared memory service for multi-agent sync

### Backend Options

- **SQLite (local)**: Default backend with FTS5 full-text search
- **memvid (remote)**: HTTP service for distributed memory sharing

---

## How the System Learns from User Input

### 1. Direct Storage via Tools

Users and agents can explicitly store memories through three tools:

| Tool | Function | Example |
|------|----------|---------|
| `memory_store` | Persist content to long-term memory | Save decisions, learnings, context |
| `memory_search` | Query memories by keyword/BM25 | Find past conversations |
| `memory_get_context` | Smart context retrieval | Get relevant memories for a topic |

**Flow:**
```
User Input → Agent Tool Call → memory.Store() → SQLite/memvid
```

### 2. Automatic Episodic Capture

Every conversation is automatically stored:

```go
// internal/memory/episodic.go
func (e *EpisodicMemory) Store(ctx context.Context, content string, category string, metadata map[string]any) (string, error) {
    id := generateUUID()
    err := e.store.Store(ctx,
        `INSERT INTO episodic_memories (id, content, category, metadata_json, created_at)
         VALUES (?, ?, ?, ?, ?)`,
        id, content, category, metaJSON, nowISO,
    )
}
```

**Attribution metadata** tracks:
- `agent_id` - Which agent created the memory
- `session_id` - Conversation session
- `task_id` - Associated task

### 3. Knowledge Graph Learning

The system automatically creates relationships between memories:

```go
// internal/memory/graph.go
func (g *KnowledgeGraph) CreateTemporalEdges(ctx context.Context, sessionID string, memoryIDs []string) error {
    // Creates edges between consecutive memories in a session
    for i := 0; i < len(memoryIDs)-1; i++ {
        edges = append(edges, MemoryEdge{
            SourceID: memoryIDs[i],
            TargetID: memoryIDs[i+1],
            EdgeType: EdgeTypeTemporal,
            Weight:   0.7,
        })
    }
}
```

**Edge types:**
- `reference` - One memory references another
- `similar` - Content similarity (Jaccard)
- `temporal` - Same session/time proximity
- `co_accessed` - Accessed together
- `causal` - One led to another

### 4. Pattern Learning (Self-Improve System)

The `internal/selfimprove` package implements a RETRIEVE → JUDGE → DISTILL → CONSOLIDATE pipeline:

```go
// internal/selfimprove/learning.go
type LearningPipeline struct {
    patterns    map[string]*LearnedPattern
    trajectories []Trajectory
}

// JUDGE: Evaluate trajectory quality
func (lp *LearningPipeline) Judge(ctx context.Context, trajectory Trajectory) (*JudgmentResult, error) {
    // Uses LLM to evaluate quality, correctness, efficiency, generalizability
}

// DISTILL: Extract reusable patterns
func (lp *LearningPipeline) Distill(ctx context.Context, trajectory Trajectory, judgment *JudgmentResult) ([]*LearnedPattern, error) {
    // Extracts strategies, tactics, anti-patterns, heuristics
}

// CONSOLIDATE: Deduplicate and prune
func (lp *LearningPipeline) Consolidate(ctx context.Context) (*ConsolidationResult, error) {
    // Applies confidence decay, removes duplicates, detects contradictions
}
```

**Pattern types learned:**
- `strategy` - High-level approaches
- `tactic` - Specific techniques
- `anti_pattern` - What NOT to do
- `heuristic` - Rules of thumb

---

## How the System Retains Memory

### Storage Backends

#### SQLite (Local Storage)

**Episodic Memory Schema:**
```sql
CREATE TABLE episodic_memories (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'conversation',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL
)

-- FTS5 virtual table for full-text search
CREATE VIRTUAL TABLE episodic_fts USING fts5(content, category, content='episodic_memories')
```

**Triggers** keep FTS index in sync automatically.

#### memvid (Distributed Storage)

Remote HTTP service with zone-based organization:
- Zones: `episodic`, `task:code`, `task:general`, `personality`
- Cross-zone search capability
- Fallback to SQLite if unavailable

### Memory Lifecycle

#### Consolidation

Old memories don't accumulate indefinitely. The consolidator runs periodically:

```go
// internal/memory/consolidation.go
func (c *Consolidator) Run(ctx context.Context, olderThanHours int) (*ConsolidationReport, error) {
    // 1. Fetch old episodic memories
    // 2. Group by date and topic
    // 3. Create summary memories
    // 4. Archive originals
    // 5. Remove duplicate task memories
}
```

**Process:**
1. Fetch memories older than threshold (default 6 hours)
2. Group by calendar date
3. Create compact summaries
4. Delete original detailed memories
5. Deduplicate task memories by content hash

#### Confidence Decay

Learned patterns decay over time:

```go
// internal/selfimprove/learning.go
daysSinceUpdate := time.Since(p.UpdatedAt).Hours() / 24
decayFactor := 1.0 - (lp.config.ConfidenceDecayRate * daysSinceUpdate)
if decayFactor < 0.5 {
    decayFactor = 0.5 // Floor at 50%
}
p.Confidence *= decayFactor
```

Default decay rate: 5% per day of non-use.

---

## How the System Accesses Memories

### Search Mechanisms

#### 1. Keyword Search (FTS5 BM25)

```go
// Uses SQLite FTS5 MATCH with BM25 ranking
rows, err := db.QueryContext(ctx, `
    SELECT m.id, m.content, m.category, m.metadata_json, m.created_at, f.rank
    FROM episodic_fts f
    JOIN episodic_memories m ON m.rowid = f.rowid
    WHERE episodic_fts MATCH ?
    ORDER BY f.rank
    LIMIT ?
`, safeQuery, limit)
```

**Fallback:** LIKE-based search when FTS5 unavailable.

#### 2. Graph-Enhanced Search

Combines relevance with PageRank importance:

```go
// internal/memory/graph.go
func (g *KnowledgeGraph) RankResults(ctx context.Context, results []MemoryResult, alpha float64) ([]MemoryResult, error) {
    // alpha = 0.3 means 30% PageRank, 70% relevance
    for i := range results {
        pr, _ := g.GetPageRank(ctx, results[i].Memory.ID)
        results[i].RelevanceScore = (1-alpha)*relevance + alpha*pr
    }
}
```

#### 3. Vector Similarity Search

```go
// internal/memory/vector/hybrid.go
type HybridSearcher struct {
    vectorStore *Store  // Embedding-based
    memManager  *Manager // Keyword-based
    alpha       float64 // 0=pure keyword, 1=pure vector
}

func (h *HybridSearcher) Search(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
    // Combines FTS5 BM25 scores with cosine similarity
}
```

#### 4. Context-Aware Retrieval

```go
// internal/memory/manager.go
func (m *Manager) GetRelevantContext(ctx context.Context, query string, maxItems int) ([]MemoryResult, error) {
    // Smart allocation: 50% episodic, 50% task memories
    // Includes recent memories for conversational continuity
    // Applies graph ranking if available
}
```

### Memory Injection into Agent Context

Memories are injected into agent prompts before LLM calls:

```go
// internal/agent/conversation.go
func (c *Conversation) InjectContext(context string) {
    contextMsg := llm.ChatMessage{
        Role:    llm.RoleSystem,
        Content: "# Relevant Context from Memory\n" + context,
    }
    c.messages = append([]llm.ChatMessage{contextMsg}, c.messages...)
}

// internal/agent/prompt.go
func (b *PromptBuilder) WithMemoryContext(context string) *PromptBuilder {
    b.memoryContext = context
    return b
}

func (b *PromptBuilder) Build() string {
    // injects memory context as a section
    if b.memoryContext != "" {
        sections = append(sections, "# Relevant Context from Memory", b.memoryContext)
    }
}
```

---

## How the System Enhances Itself

### 1. Shadow Training

Parallel execution with a "teacher" model generates training data:

```toml
# Configuration
[shadow]
enabled = false
data_dir = "~/.meept/shadow"

[shadow.teacher]
model = "claude-opus-4-5-20251101"
max_daily_cost = 10.0

[shadow.quality]
high_quality_threshold = 0.85
trainable_threshold = 0.6
```

**Process:**
1. Run student model on task
2. Run teacher model in parallel
3. Compare outputs for quality
4. Export high-quality trajectories as JSONL/DPO
5. Train LoRA adapters

### 2. Trajectory Learning Pattern

```
Execution → JUDGE (quality) → DISTILL (patterns) → CONSOLIDATE (merge)
```

**Judgment criteria:**
- Quality (0-1): Overall approach quality
- Correctness (0-1): Was solution correct?
- Efficiency (0-1): Was it done efficiently?
- Generalizability (0-1): Can it be reused?

### 3. Pattern Storage and Retrieval

```go
// Patterns are stored with metadata
type LearnedPattern struct {
    ID          string
    Type        PatternType  // strategy, tactic, anti_pattern, heuristic
    Domain      string       // code, debugging, planning
    Pattern     string       // The actual rule
    Confidence  float64      // 0.0-1.0
    SuccessRate float64      // Historical success rate
    UseCount    int
}

// Retrieved during similar tasks
func (lp *LearningPipeline) Retrieve(ctx context.Context, query string, domain string, k int) ([]*LearnedPattern, error) {
    // Scores by: keyword relevance + confidence + success rate
}
```

### 4. Personality Evolution

```go
// internal/memory/personality.go
func (p *PersonalityMemory) Update(ctx context.Context, interactionSummary string) error {
    p.interactionCount++
    noteLine := fmt.Sprintf("- [%s] %s", nowISO, interactionSummary)
    p.description = appendToSection(p.description, "## Interaction Notes", noteLine)
}
```

Over time, the personality file accumulates:
- Communication style preferences
- Recurring themes
- User preferences
- Interaction history

### 5. Self-Improvement Cycle

The controller runs automated improvement cycles:

```go
// internal/selfimprove/controller.go
func (c *Controller) RunFullCycle(ctx context.Context, interactive bool) (*ImprovementCycle, error) {
    // Phase 1: Detection - scan logs, code for issues
    issues := c.detector.DetectAll(ctx)

    // Phase 2: Analysis - root cause analysis with LLM
    analyses := c.analyzer.Analyze(ctx, issues)

    // Phase 3: Generation - create fixes
    fixes := c.generator.Generate(ctx, analyses, issues)

    // Phase 4: Validation - test in sandbox
    validations := c.validator.Validate(ctx, fixes)

    // Phase 5: Application - apply approved fixes
    applied := c.applier.Apply(ctx, fixes, validations)
}
```

---

## Benefits of the System

### 1. Multi-Modal Memory Access

| Modality | Strength | Use Case |
|----------|----------|----------|
| Keyword (FTS5) | Exact match, fast | Finding specific conversations |
| PageRank | Importance ranking | surfacing critical memories |
| Vector (embeddings) | Semantic similarity | Conceptual queries |
| Graph traversal | Relationship discovery | Connected context |

### 2. Graceful Degradation

- Falls back from memvid → SQLite
- Falls back from FTS5 → LIKE search
- Falls back from LLM judgment → heuristics
- Falls back from vector → keyword search

### 3. Attribution and Traceability

Every memory tracks:
- Which agent created it
- Which session it belongs to
- Which task it relates to

### 4. Automatic Maintenance

- Periodic consolidation prevents unbounded growth
- Confidence decay prunes unused patterns
- Duplicate detection keeps knowledge clean

### 5. Domain Separation

Task memories are separated by domain (`code`, `commands`, `general`), preventing cross-domain pollution.

### 6. Distributed Awareness

The memvid backend enables:
- Multi-agent memory sharing
- Hydration on job claim
- Distillation of important memories to shared storage

---

## Shortcomings and Implementation Gaps

### 1. Security Components Not Integrated

**Issue:** The security components (PromptGuard, OutputMonitor, InputSanitizer) are implemented but **NOT wired into the agent loop**.

**Location:** `internal/memory/manager.go`, `internal/agent/loop.go`

**Impact:** Memory operations may be vulnerable to prompt injection attacks.

### 2. Fail-Open Security Default

**Issue:** `loop.go:474` returns `allow` when security is misconfigured.

```go
// Pseudocode - actual implementation may vary
if security.Check() fails {
    return ALLOW  // Should be DENY
}
```

**Impact:** Security bypass when configuration is incomplete.

### 3. Plan/Execute Type Mismatch

**Issue:** `front.py:154-166` - `plan.steps` called on list (planner returns list, not TaskPlan).

```python
# Legacy Python code - potential AttributeError
steps = plan.steps  # Fails if plan is a list
```

**Impact:** Runtime errors when accessing plan properties.

### 4. Memory Injection Not Automatic

**Issue:** Memory context injection requires explicit calls. There's no automatic "fetch relevant context before every LLM call" mechanism baked into the core loop.

**Location:** `internal/agent/conversation.go:415` (InjectContext is available but not called automatically)

**Impact:** Agents may miss relevant context without explicit tool calls.

### 5. Consolidation Only for SQLite

**Issue:** The consolidator only runs for SQLite backends, not memvid:

```go
// internal/memory/manager.go
if !m.useMemvid {
    m.consolidator = NewConsolidator(...)
}
```

**Impact:** memvid memories accumulate without summarization.

### 6. Pattern Learning Not Integrated with Agent Loop

**Issue:** The `LearningPipeline` exists but has no clear integration point with the main agent execution loop. Trajectories are not automatically captured.

**Location:** `internal/selfimprove/learning.go`

**Impact:** Self-improvement requires explicit invocation, not continuous learning.

### 7. No Embedding Generation Pipeline

**Issue:** Vector memory exists but there's no automatic embedding generation when memories are stored.

**Location:** `internal/memory/vector/store.go`

**Impact:** Vector search requires manual embedding generation.

### 8. Personality Memory Is Append-Only

**Issue:** Personality only appends notes; no summarization or evolution of traits.

```go
// Always appends, never summarizes
p.description = appendToSection(p.description, "## Interaction Notes", noteLine)
```

**Impact:** Personality file grows unbounded; no abstraction of patterns.

### 9. Contradiction Detection Is Simplistic

**Issue:** Only checks anti-patterns vs regular patterns:

```go
// internal/selfimprove/learning.go
for _, anti := range antiPatterns {
    for _, regular := range regularPatterns {
        if lp.similarity(anti.Pattern, regular.Pattern) > 0.7 {
            // Deprecate one
        }
    }
}
```

**Impact:** Misses contradictions between two regular patterns or two strategies.

### 10. No Memory Versioning

**Issue:** Memories can be updated but there's no version history.

**Impact:** Cannot rollback to previous knowledge states; no audit trail for memory changes.

### 11. Graph Edges Are Limited

**Issue:** Edge creation is manual. No automatic entity extraction or relationship inference.

**Impact:** Graph remains sparse unless explicitly populated.

### 12. Self-Improvement Disabled by Default

**Issue:** Configuration defaults to `enabled = false`:

```go
// internal/selfimprove/config.go
Enabled: false, // Disabled by default
```

**Impact:** Most deployments won't benefit from self-improvement without manual configuration.

---

## Recommendations for Improvement

### High Priority

1. **Wire security components** - Integrate PromptGuard and InputSanitizer into the agent loop
2. **Fix fail-open default** - Change to fail-closed when security is misconfigured
3. **Add automatic memory injection** - Fetch relevant context before every LLM call
4. **Integrate learning pipeline** - Capture trajectories automatically during execution

### Medium Priority

5. **Enable automatic embeddings** - Generate embeddings on memory store
6. **Add memory versioning** - Track memory changes with timestamps
7. **Implement entity extraction** - Auto-create graph nodes from memory content
8. **Enable consolidation for memvid** - Don't skip summarization for distributed backends

### Low Priority

9. **Personality summarization** - Periodically summarize personality notes into traits
10. **Better contradiction detection** - Check all pattern combinations, not just anti-patterns
11. **Enable self-improvement by default** - Ship with sensible safe defaults that enable learning

---

## Reimplementation Guide

To reimplement this memory enhancement system elsewhere:

### Core Components Needed

1. **Storage Layer**
   - SQLite with FTS5 for keyword search
   - Optional remote service for distributed storage
   - Connection pooling for concurrency

2. **Memory Types**
   ```
   - Episodic: Conversation history
   - Task: Domain knowledge
   - Graph: Relationships
   - Vector: Embeddings
   - Personality: User preferences
   ```

3. **Search Mechanisms**
   - BM25 keyword search
   - PageRank for importance
   - Cosine similarity for vectors
   - Hybrid scoring (alpha blending)

4. **Lifecycle Management**
   - Periodic consolidation
   - Confidence decay
   - Duplicate detection

5. **Learning Pipeline**
   - Trajectory capture
   - Quality judgment (LLM or heuristic)
   - Pattern distillation
   - Consolidation with deduplication

### Key Design Decisions

| Decision | Meept Choice | Rationale |
|----------|--------------|-----------|
| Storage | SQLite + optional remote | Local-first, distributed optional |
| Search | Hybrid (keyword + vector + graph) | Multiple access patterns |
| Consolidation | Time-based summarization | Prevent unbounded growth |
| Learning | JUDGE → DISTILL → CONSOLIDATE | Separation of concerns |
| Attribution | agent_id, session_id, task_id | Traceability |

### Implementation Order

1. Basic storage (Store/Search/GetRecent)
2. FTS5 integration
3. Knowledge graph
4. Consolidation
5. Vector embeddings
6. Learning pipeline
7. Distributed sync

---

## Conclusion

Meept's memory system is architecturally sophisticated with thoughtful multi-tier design, graceful degradation, and a genuine self-improvement pipeline. However, several critical components remain unintegrated, and the self-improvement features are opt-in rather than automatic.

**Key strengths:** Multi-modal access, attribution tracking, confidence decay, hybrid search, knowledge graph with PageRank.

**Key weaknesses:** Security not wired in, learning not automatic, embeddings manual, personality append-only, consolidation SQLite-only.

The system is well-designed for an AI agent that needs persistent, queryable, evolving memory—but realizing its full potential requires addressing the integration gaps identified above.
