# Plan: Dynamic Lightweight Skill Loading for All Agents

## Problem Statement

The current implementation has:
1. **Hardcoded keyword mappings** in `capabilities_builder.go` - not derived from skill/agent files
2. **Dispatcher-only capability matching** - other agents don't benefit from lightweight loading
3. **Static routing** - adding new skills requires code changes to routing logic

## Vision

All agents should:
1. Have access to a **dynamically-generated capability index** derived from skill metadata
2. Load skill bodies **on-demand** only when needed for execution
3. Make routing/selection decisions based on **actual skill metadata** (description, tags, examples)
4. Work with new skills automatically - no code changes required

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Startup (Once)                              │
├─────────────────────────────────────────────────────────────────┤
│  1. Scan skill directories (3-tier + clawskills)                │
│  2. Parse YAML frontmatter only (no bodies)                     │
│  3. Build SkillIndex with metadata                              │
│  4. Generate CapabilityIndex from metadata                      │
│     - Extract keywords from: description, examples, tags        │
│     - Build inverted index: keyword → [skill entries]           │
│  5. Share CapabilityIndex with all agents via AgentRegistry     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Runtime (Per Request)                         │
├─────────────────────────────────────────────────────────────────┤
│  Agent receives input                                            │
│       │                                                          │
│       ▼                                                          │
│  CapabilityIndex.Match(input) → [SkillIndexEntry...]            │
│       │                                                          │
│       ▼                                                          │
│  If confident match found:                                       │
│    → LazySkillLoader.Load(skill_name) → full Skill with body    │
│    → Inject skill body into agent context                        │
│    → Execute with skill instructions                             │
│       │                                                          │
│       ▼                                                          │
│  If no match: proceed without skill augmentation                 │
└─────────────────────────────────────────────────────────────────┘
```

## Recommendation: Remove Hardcoded Keyword Mappings

**Yes, remove them.** Here's why:

| Aspect | Hardcoded Keywords | Metadata-Derived |
|--------|-------------------|------------------|
| Maintenance | Must update code for new skills | Automatic from files |
| Flexibility | Rigid, developer-controlled | Dynamic, skill-author-controlled |
| Coverage | Only covers anticipated patterns | Covers whatever skill defines |
| Redundancy | Duplicates info already in skill files | Single source of truth |

The skill's `examples`, `description`, and `tags` fields already contain routing signals. Extracting them programmatically is more robust than maintaining parallel keyword lists.

**Keep minimal intent patterns** only for platform-level routing (e.g., "what can you do" → platform introspection) that aren't skill-based.

---

## Implementation Plan

### Phase 1: Refactor CapabilityIndex to be Metadata-Driven

**File: `internal/skills/capability_index.go`** (new)

```go
// CapabilityIndex provides fast lookup of skills by semantic signals.
// Built entirely from skill metadata - no hardcoded mappings.
type CapabilityIndex struct {
    mu sync.RWMutex

    // Inverted indices for fast lookup
    byKeyword     map[string][]*ScoredEntry  // keyword → skills with scores
    byTag         map[string][]*SkillIndexEntry
    byCapability  map[string][]*SkillIndexEntry

    // TF-IDF or similar weights for better matching
    keywordWeights map[string]float64

    // Source index
    skillIndex *SkillIndex
}

type ScoredEntry struct {
    Entry  *SkillIndexEntry
    Score  float64  // Weight based on where keyword appeared
    Source string   // "name", "description", "example", "tag"
}

// BuildFromIndex constructs capability index from skill metadata
func BuildCapabilityIndex(idx *SkillIndex) *CapabilityIndex

// Match returns skills ranked by relevance to input
func (ci *CapabilityIndex) Match(input string, limit int) []*MatchResult
```

**Keyword extraction sources and weights:**

| Source | Weight | Rationale |
|--------|--------|-----------|
| Skill name | 1.0 | Direct identifier |
| Tags | 0.9 | Explicit categorization |
| Examples | 0.8 | Author-provided triggers |
| Description (nouns/verbs) | 0.5 | Semantic hints |

### Phase 2: Agent-Level Skill Discovery

**Modify: `internal/agent/loop.go`**

Add capability index to AgentLoop:

```go
type AgentLoop struct {
    // existing fields...

    capabilityIndex *skills.CapabilityIndex
    skillLoader     *skills.LazySkillLoader
}

// discoverRelevantSkills finds skills that might help with the current input
func (l *AgentLoop) discoverRelevantSkills(input string) []*skills.SkillIndexEntry {
    if l.capabilityIndex == nil {
        return nil
    }

    matches := l.capabilityIndex.Match(input, 3) // top 3 candidates

    var relevant []*skills.SkillIndexEntry
    for _, m := range matches {
        if m.Confidence >= 0.5 {
            relevant = append(relevant, m.Entry)
        }
    }
    return relevant
}

// loadSkillContext loads skill body and injects into system prompt
func (l *AgentLoop) loadSkillContext(skillName string) (string, error) {
    skill, err := l.skillLoader.Load(context.Background(), skillName)
    if err != nil {
        return "", err
    }
    return skill.Body, nil
}
```

**Modify agent execution flow:**

```go
func (l *AgentLoop) RunOnce(ctx context.Context, input string, conversationID string) (string, error) {
    // 1. Discover potentially relevant skills (lightweight - metadata only)
    relevantSkills := l.discoverRelevantSkills(input)

    // 2. If strong match, load skill body into context
    var skillContext string
    if len(relevantSkills) > 0 && relevantSkills[0].Confidence >= 0.7 {
        body, err := l.loadSkillContext(relevantSkills[0].Name)
        if err == nil {
            skillContext = body
        }
    }

    // 3. Build system prompt with optional skill injection
    systemPrompt := l.buildSystemPrompt(skillContext)

    // 4. Continue with normal execution...
}
```

### Phase 3: Remove Hardcoded Mappings

**Delete from `internal/agent/capabilities_builder.go`:**
- `var intentTypeMapping`
- `var keywordMapping`

**Modify `CapabilitiesBuilder.buildAgentCapabilities()`:**
- Derive intent types from agent's `AvailableSkills` metadata
- Derive keywords from skill examples and descriptions
- No hardcoded fallbacks

**Keep in `internal/agent/capability_matcher.go`:**
- Platform introspection patterns (minimal, not skill-based)
- Fallback to "chat" for unmatched input

### Phase 4: Dispatcher Uses Same Infrastructure

**Modify `internal/agent/dispatcher.go`:**

The dispatcher becomes a consumer of the same CapabilityIndex:

```go
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, ...) (*Intent, error) {
    // 1. Query capability index for skill matches
    matches := d.capabilityIndex.Match(input, 5)

    // 2. If skill match found, determine which agent has that skill
    if len(matches) > 0 && matches[0].Confidence >= 0.7 {
        agentID := d.findAgentForSkill(matches[0].Entry.Name)
        return &Intent{
            Type:       "skill",
            AgentType:  agentID,
            SkillHint:  matches[0].Entry.Name,
            Confidence: matches[0].Confidence,
        }, nil
    }

    // 3. Fallback to LLM classification or keyword patterns
    // ...
}
```

### Phase 5: Wire Up in Daemon

**Modify `internal/daemon/components.go`:**

```go
func (c *Components) initializeSkills(...) {
    // 1. Discover metadata only
    indexEntries, _ := discovery.DiscoverMetadataOnly()
    c.SkillIndex = skills.NewSkillIndex()
    c.SkillIndex.IndexAll(indexEntries)

    // 2. Build capability index from metadata (NEW)
    c.CapabilityIndex = skills.BuildCapabilityIndex(c.SkillIndex)

    // 3. Create lazy loader
    c.SkillLoader = skills.NewLazySkillLoader(c.SkillIndex, ...)

    // 4. Share with agent registry for all agents
    // ...
}

// In agent registry setup:
c.AgentRegistry = agent.NewAgentRegistry(agent.RegistryConfig{
    // existing...
    CapabilityIndex: c.CapabilityIndex,
    SkillLoader:     c.SkillLoader,
})
```

---

## Files to Create

| File | Purpose |
|------|---------|
| `internal/skills/capability_index.go` | Metadata-driven capability index |
| `internal/skills/keyword_extractor.go` | Extract keywords from descriptions/examples |
| `internal/skills/capability_index_test.go` | Tests |

## Files to Modify

| File | Changes |
|------|---------|
| `internal/agent/loop.go` | Add skill discovery and context injection |
| `internal/agent/registry.go` | Share CapabilityIndex with all agents |
| `internal/agent/capabilities_builder.go` | Remove hardcoded mappings, derive from metadata |
| `internal/agent/capability_matcher.go` | Use CapabilityIndex, keep only minimal platform patterns |
| `internal/agent/dispatcher.go` | Use CapabilityIndex for routing decisions |
| `internal/daemon/components.go` | Build and share CapabilityIndex |

## Files to Delete/Deprecate

None deleted, but significant code removal:
- `keywordMapping` variable
- `intentTypeMapping` variable
- Most of `matchByIntentPattern()` (keep only platform introspection)

---

## Benefits

1. **Zero-code skill addition**: Add skill file with good metadata → routing works
2. **All agents benefit**: Not just dispatcher - every agent can discover relevant skills
3. **Memory efficient**: Skill bodies only loaded when needed
4. **Single source of truth**: Routing derived from skill files, not duplicated in code
5. **Extensible**: Add TF-IDF, embeddings, or other matching strategies later

## Migration Strategy

1. Implement CapabilityIndex alongside existing system
2. Run both in parallel, log discrepancies
3. Once confident, remove hardcoded mappings
4. Monitor for regressions

## Success Metrics

- Skill bodies loaded only on execution (measure with stats)
- New skills routed correctly without code changes
- No regression in routing accuracy
- Reduced memory footprint at startup
