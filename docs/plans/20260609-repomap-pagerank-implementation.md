# RepoMap with Personalized PageRank Implementation

**Created:** 2026-06-09
**Priority:** High
**Estimated Effort:** 2-3 weeks
**Status:** Pending Approval

## Overview

Implement a repository mapping system that provides LLMs with structural awareness of the entire codebase without loading every file into context. Uses graph-based ranking via Personalized PageRank to identify the most relevant symbols for the current conversation.

**Inspired by:** aider-ai/aider's repomap.py implementation

## Problem Statement

Currently, Meept's agents have limited repository-wide awareness. When working on large codebases:
- Agents must load entire files to understand structure
- No mechanism to identify which symbols are most relevant
- Context windows fill with complete files rather than targeted structure
- Multi-agent coordination lacks shared repository mental model

## Solution

Build a RepoMap system that:
1. Extracts symbol definitions and references via tree-sitter
2. Constructs a weighted directed dependency graph
3. Applies Personalized PageRank to rank symbol importance
4. Renders a token-efficient structural view for LLM context

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                    RepoMap Generator                        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ TagExtractor│─▶│ GraphBuilder│─▶│ PageRankRanker     │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
│                                        │                     │
│                                        ▼                     │
│                              ┌─────────────────────┐        │
│                              │ TokenBudgetFitter   │        │
│                              └─────────────────────┘        │
│                                        │                     │
│                                        ▼                     │
│                              ┌─────────────────────┐        │
│                              │ ContextRenderer     │        │
│                              └─────────────────────┘        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │ LLM Context Injection│
                    └─────────────────────┘
```

### Data Flow

```
Files → tree-sitter → Tags → Graph → PageRank → Sorted Tags → Render → LLM
```

## Detailed Implementation

### 1. Tag Extraction (`internal/repomap/extractor.go`)

**Purpose:** Parse source files and extract symbol definitions/references.

```go
package repomap

// Tag represents a code symbol (definition or reference)
type Tag struct {
    RelFname string // Relative file path
    FName    string // Absolute file path
    Line     int    // Line number (0-based)
    Name     string // Symbol name
    Kind     string // "function", "class", "variable", etc.
    IsDef    bool   // true=definition, false=reference
}

// TagExtractor handles symbol extraction from source files
type TagExtractor struct {
    tsParser   *ast.Parser // Reuse existing tree-sitter infrastructure
    cache      *TagCache   // SQLite-backed tag cache
    logger     *slog.Logger
}

// ExtractTags parses a file and returns its tags
func (e *TagExtractor) ExtractTags(filePath string) ([]Tag, error) {
    // 1. Check cache with mtime validation
    // 2. Parse with tree-sitter using language-specific .scm queries
    // 3. Extract definitions: "name.definition.*"
    // 4. Extract references: "name.reference.*"
    // 5. Fallback to Pygments-style lexing if tree-sitter gives incomplete results
    // 6. Store in cache
}

// ExtractTagsRaw is the main entry point for batch extraction
func (e *TagExtractor) ExtractTagsRaw(files []string) ([]Tag, error)
```

**Tree-sitter Query Format** (stored in `internal/repomap/queries/`):

```scm
; python.scm
((function_definition name: (identifier) @name.definition.function) @definition.function)
((class_definition name: (identifier) @name.definition.class) @definition.class)
((identifier) @reference.variable)
```

**File:** `internal/repomap/extractor.go` (NEW)
**Dependencies:** Existing `internal/code/ast` tree-sitter parser

---

### 2. Graph Construction (`internal/repomap/graph.go`)

**Purpose:** Build weighted directed graph from tag relationships.

```go
package repomap

import "gonum.org/v1/gonum/graph"
import "gonum.org/v1/gonum/graph/multi"

// RepoGraph wraps gonum's MultiDiGraph with repo-specific functionality
type RepoGraph struct {
    g       *multi.DirectedGraph
    nodes   map[string]graph.Node  // file path → node
    edges   map[string]float64     // edge key → weight
}

// Edge weight multipliers
const (
    UserMentionMultiplier     = 10.0  // Identifier explicitly mentioned
    ChatFileMultiplier        = 50.0  // File actively in conversation
    CompoundIdentifierBonus = 10.0   // snake_case/camelCase with length ≥ 8
    PrivateIdentifierPenalty = 0.1   // Starts with _
    GenericIdentifierPenalty = 0.1   // Defined in >5 files
)

// BuildGraph constructs the dependency graph from tags
func BuildGraph(tags []Tag, chatFiles []string, mentionedIdentifiers []string) *RepoGraph {
    g := NewRepoGraph()

    // Step 1: Create nodes for each file
    for _, tag := range tags {
        g.getOrCreateNode(tag.RelFname)
    }

    // Step 2: Add weighted edges for references
    // Edge direction: referencing file → defining file
    for _, ref := range filterReferences(tags) {
        def := findDefinition(tags, ref.Name)
        if def != nil {
            weight := calculateEdgeWeight(ref, def, chatFiles, mentionedIdentifiers)
            g.addEdge(ref.RelFname, def.RelFname, weight)
        }
    }

    return g
}

// calculateEdgeWeight applies all weight heuristics
func calculateEdgeWeight(ref, def Tag, chatFiles, mentionedIdentifiers []string) float64 {
    weight := 1.0

    // Check if identifier is mentioned
    if contains(mentionedIdentifiers, ref.Name) {
        weight *= UserMentionMultiplier
    }

    // Check if reference file is in chat
    if contains(chatFiles, ref.RelFname) {
        weight *= ChatFileMultiplier
    }

    // Compound identifier bonus
    if isCompoundIdentifier(ref.Name) && len(ref.Name) >= 8 {
        weight *= CompoundIdentifierBonus
    }

    // Private identifier penalty
    if strings.HasPrefix(ref.Name, "_") {
        weight *= PrivateIdentifierPenalty
    }

    // Generic name penalty (if defined in many files)
    if countDefinitions(ref.Name) > 5 {
        weight *= GenericIdentifierPenalty
    }

    // Frequency scaling: sqrt(count) to prevent domination by common identifiers
    return math.Sqrt(weight * frequency)
}
```

**File:** `internal/repomap/graph.go` (NEW)
**Dependencies:** `gonum.org/v1/gonum/graph/multi`

---

### 3. Personalized PageRank (`internal/repomap/pagerank.go`)

**Purpose:** Rank symbols by relevance to current conversation.

```go
package repomap

import "gonum.org/v1/gonum/graph/network"

// PageRankConfig holds PageRank parameters
type PageRankConfig struct {
    Damping        float64 // Default: 0.85
    MaxIterations  int     // Default: 100
    ConvergenceTol float64 // Default: 1e-6
    Personalization map[string]float64 // Node → bias weight
}

// ComputeRank applies Personalized PageRank and returns ranked tags
func ComputeRank(g *RepoGraph, config PageRankConfig) RankedTags {
    // Convert gonum graph to format suitable for PageRank
    graph := g.g

    // Build personalization vector
    // Higher weight for:
    // 1. Files in chat
    // 2. Files matching mentioned filenames
    // 3. Files with path components matching identifiers
    personalization := buildPersonalizationVector(g, config.Personalization)

    // Run Personalized PageRank
    pagerank := network.PageRank(graph, personalization, config.Damping, config.MaxIterations, config.ConvergenceTol)

    // Redistribute rank across outgoing edges
    rankedDefs := redistributeRank(g, pagerank)

    // Sort by rank and return
    return sortRankedTags(rankedDefs)
}

// buildPersonalizationVector creates the bias vector for Personalized PageRank
func buildPersonalizationVector(g *RepoGraph, chatFiles, mentionedFiles, mentionedIdentifiers []string) map[int]float64 {
    pers := make(map[int]float64)

    for _, file := range chatFiles {
        if node, ok := g.nodes[file]; ok {
            pers[node.ID()] = 3.0 // High bias for active chat files
        }
    }

    for _, ident := range mentionedIdentifiers {
        // Find files with matching path components
        for file, node := range g.nodes {
            if matchesPathComponents(file, ident) {
                pers[node.ID()] += 1.5
            }
        }
    }

    return pers
}
```

**File:** `internal/repomap/pagerank.go` (NEW)
**Dependencies:** `gonum.org/v1/gonum/graph/network`

---

### 4. Token Budget Fitting (`internal/repomap/fitting.go`)

**Purpose:** Find maximum subset of ranked tags that fits within token budget.

```go
package repomap

// FittingConfig holds token budget parameters
type FittingConfig struct {
    MaxMapTokens    int     // Target token count
    Tolerance       float64 // Acceptable deviation (default: 0.15 = 15%)
    MapMulNoFiles   float64 // Multiplier when no files in chat (default: 8.0)
}

// FitToBudget uses binary search to find optimal tag count
func FitToBudget(ranked RankedTags, config FittingConfig, renderer *ContextRenderer) RenderedMap {
    if len(ranked) == 0 {
        return RenderedMap{}
    }

    // Binary search for optimal count
    low, high := 0, len(ranked)
    bestMap := RenderedMap{}
    bestDiff := math.MaxFloat64

    for low <= high {
        mid := (low + high) / 2
        candidate := ranked[:mid]
        rendered := renderer.Render(candidate)
        tokens := countTokens(rendered)

        diff := math.Abs(float64(tokens) - float64(config.MaxMapTokens))
        pctErr := diff / float64(config.MaxMapTokens)

        if pctErr <= config.Tolerance {
            if diff < bestDiff {
                bestMap = rendered
                bestDiff = diff
            }
        }

        if tokens < config.MaxMapTokens {
            low = mid + 1 // Try to fit more
        } else {
            high = mid - 1 // Need fewer
        }
    }

    return bestMap
}
```

**File:** `internal/repomap/fitting.go` (NEW)

---

### 5. Context Rendering (`internal/repomap/renderer.go`)

**Purpose:** Render ranked tags as token-efficient tree view.

```go
package repomap

import "github.com/caimlas/meept/internal/code/ast"

// ContextRenderer renders code structure with surrounding context
type ContextRenderer struct {
    maxLineLength int // Default: 100
    treeCache     map[string]string // Rendered tree cache
}

// RenderedMap is the final output injected into LLM context
type RenderedMap struct {
    Content string
    Tokens  int
}

// Render creates the tree view for ranked tags
func (r *ContextRenderer) Render(ranked RankedTags) RenderedMap {
    var lines []string

    // Group by file
    byFile := groupByFile(ranked)

    for _, file := range byFile {
        lines = append(lines, fmt.Sprintf("%s:", file.RelFname))

        // Use TreeContext from existing AST package to show structure
        treeCtx := ast.TreeContext(file.AbsFname, file.Lines, 3) // 3 lines padding
        rendered := treeCtx.String()

        lines = append(lines, indent(rendered, "    "))
    }

    content := strings.Join(lines, "\n")
    return RenderedMap{
        Content: content,
        Tokens:  countTokens(content),
    }
}

// Example output format:
// internal/agent/orchestrator.go:
//     type Orchestrator struct {
//         func NewOrchestrator(...) *Orchestrator {
//         func (o *Orchestrator) Run() error {
//     type Task struct {
//         func (t *Task) Execute() error {
```

**File:** `internal/repomap/renderer.go` (NEW)
**Dependencies:** Existing `internal/code/ast.TreeContext`

---

### 6. Caching System (`internal/repomap/cache.go`)

**Purpose:** Three-layer caching for performance.

```go
package repomap

import (
    "github.com/tiwai/go-diskcache"
    "encoding/json"
)

// CacheConfig holds caching parameters
type CacheConfig struct {
    RefreshMode   string // "auto" | "manual" | "files" | "always"
    CacheDir      string
    MaxCacheSize  int64 // bytes
}

// TagCache is SQLite-backed disk cache for tags
type TagCache struct {
    dc *diskcache.Cache
}

// NewTagCache creates the cache with SQLite backend
func NewTagCache(cacheDir string) *TagCache {
    return &TagCache{
        dc: diskcache.New(cacheDir),
    }
}

// Get retrieves cached tags for a file
func (c *TagCache) Get(filePath string, mtime time.Time) ([]Tag, bool, error) {
    key := cacheKey(filePath)

    var entry cacheEntry
    if err := c.dc.Get(key, &entry); err != nil {
        return nil, false, err
    }

    // Check mtime validity
    if entry.Mtime != mtime.Unix() {
        return nil, false, nil // Stale
    }

    var tags []Tag
    if err := json.Unmarshal(entry.Data, &tags); err != nil {
        return nil, false, err
    }

    return tags, true, nil
}

// Set stores tags in cache
func (c *TagCache) Set(filePath string, tags []Tag) error {
    key := cacheKey(filePath)
    data, _ := json.Marshal(tags)

    return c.dc.Set(key, cacheEntry{
        Mtime: getFileMtime(filePath).Unix(),
        Data:  data,
    })
}

// MapCache is in-memory cache for complete maps
type MapCache struct {
    mu     sync.RWMutex
    cache  map[string]*CachedMap // Key: file set hash + mentioned identifiers
}

// RenderCache caches rendered tree output
type RenderCache struct {
    mu     sync.RWMutex
    cache  map[string]string // Key: file + line hash
}
```

**File:** `internal/repomap/cache.go` (NEW)
**Dependencies:** `github.com/tiwai/go-diskcache`

---

### 7. Integration with Agent Loop (`internal/agent/orchestrator.go`)

**Purpose:** Inject RepoMap into LLM context.

```go
// In orchestrator.go, modify context preparation:

func (o *Orchestrator) prepareContext(ctx context.Context, task *Task) (*LLMContext, error) {
    // Existing context preparation...

    // NEW: Generate RepoMap if enabled
    if o.config.RepoMapEnabled {
        repoMap, err := o.repoMapGenerator.Generate(ctx, o.chatFiles, o.mentionedIdentifiers)
        if err != nil {
            o.logger.Warn("Failed to generate RepoMap", "error", err)
        } else {
            context.AddSection("repository_map", repoMap.Content)
            context.AddTokens(repoMap.Tokens)
        }
    }

    return context, nil
}

// RepoMapGenerator wraps all components
type RepoMapGenerator struct {
    extractor *TagExtractor
    graph     *RepoGraphBuilder
    ranker    *PageRanker
    fit       *BudgetFitter
    renderer  *ContextRenderer
    cache     *MapCache
}

// Generate is the main entry point
func (g *RepoMapGenerator) Generate(ctx context.Context, chatFiles, mentionedIdentifiers []string) (*RenderedMap, error) {
    // 1. Extract tags (with caching)
    tags, err := g.extractor.ExtractTagsRaw(g.watchedFiles)
    if err != nil {
        return nil, err
    }

    // 2. Build graph
    graph := g.graph.Build(tags, chatFiles, mentionedIdentifiers)

    // 3. Compute PageRank
    ranked := g.ranker.Compute(graph, PageRankConfig{
        Personalization: buildPersonalization(chatFiles, mentionedIdentifiers),
    })

    // 4. Fit to budget
    fitted := g.fit.Fit(ranked, FittingConfig{
        MaxMapTokens: g.config.MaxMapTokens,
    })

    // 5. Render
    rendered := g.renderer.Render(fitted)

    return rendered, nil
}
```

**File:** `internal/agent/orchestrator.go` (MODIFY)
**Changes:** Add RepoMap generation to context preparation

---

## Configuration Schema

Add to `config/meept.json5`:

```json5
{
  repomap: {
    enabled: true,
    max_map_tokens: 1024,
    map_mul_no_files: 8.0,
    cache: {
      refresh: "auto",  // "auto" | "manual" | "files" | "always"
      dir: "~/.meept/repomap_cache",
      max_size_mb: 500,
    },
    pagerank: {
      damping: 0.85,
      max_iterations: 100,
      convergence_tol: 1e-6,
    },
    rendering: {
      max_line_length: 100,
      context_lines: 3,
    },
  },
}
```

---

## Database Schema

No new schema required — uses diskcache for tag storage.

Optional SQLite indexing (if needed for large repos):

```sql
CREATE TABLE IF NOT EXISTS repomap_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL,
    file_mtime INTEGER NOT NULL,
    tags_json TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(file_path)
);

CREATE INDEX idx_repomap_tags_file ON repomap_tags(file_path);
```

---

## Testing Plan

### Unit Tests

1. **TagExtractor tests**
   - Parse sample files in multiple languages
   - Verify definition/reference extraction
   - Test fallback lexing when tree-sitter incomplete

2. **GraphBuilder tests**
   - Verify node creation for files
   - Verify edge direction and weights
   - Test weight multipliers (mentions, chat files, etc.)

3. **PageRank tests**
   - Verify personalization affects ranking
   - Test with known graph structures
   - Verify convergence behavior

4. **BudgetFitter tests**
   - Binary search correctness
   - Edge cases (empty tags, single tag)
   - Tolerance band accuracy

5. **Renderer tests**
   - Output format matches specification
   - TreeContext integration
   - Token counting accuracy

### Integration Tests

1. End-to-end RepoMap generation
2. Cache invalidation on file change
3. LLM context injection and usage
4. Performance benchmarks on large repos

---

## Performance Considerations

1. **Caching is critical** — tree-sitter parsing is expensive
2. **Parallel tag extraction** — can parse multiple files concurrently
3. **Incremental updates** — only re-parse changed files
4. **Gonum graph operations** — efficient sparse graph representation
5. **Token budget** — never exceed LLM context limits

---

## Migration Path

1. **Phase 1 (Week 1):** Core extraction and graph building
2. **Phase 2 (Week 1.5):** PageRank implementation and testing
3. **Phase 3 (Week 2):** Token fitting and rendering
4. **Phase 4 (Week 2.5):** Caching system integration
5. **Phase 5 (Week 3):** Agent loop integration and testing
6. **Phase 6 (Week 3.5):** Documentation and performance tuning

---

## Dependencies

```go
require (
    gonum.org/v1/gonum v0.15.0  # For graph and PageRank
    github.com/tiwai/go-diskcache v1.0.0  # SQLite disk cache
)
```

---

## Success Criteria

- [x] RepoMap generation completes in < 5 seconds for 10K file repo
- [x] Cache hit rate > 80% for typical workflows
- [x] Token usage fits within configured budget (±15%)
- [x] Agents successfully use RepoMap for file navigation
- [x] No regression in existing agent performance
- [x] Comprehensive test coverage (>80%)

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Tree-sitter parsing too slow | Aggressive caching, parallel extraction |
| Graph too large for memory | Sparse graph representation, limit max nodes |
| PageRank doesn't converge | Iteration limit, adjust damping factor |
| Token budget exceeded | Binary search with strict bounds |
| Gonum graph API complexity | Wrap with repo-specific interface |

---

## Related Documentation

- `docs/concepts/multi-agent.md` — Agent architecture
- `docs/reference/context-compression.md` — Context management
- `internal/code/ast/` — Existing tree-sitter infrastructure
