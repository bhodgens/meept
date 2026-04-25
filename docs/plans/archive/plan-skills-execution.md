# Plan: Skills Execution Integration

**Status:** Complete
**Priority:** Medium
**Estimated Effort:** 2-3 days
**Completion Date:** 2026-04-25

---

## Current State

The skills system has **discovery and execution implemented** but is **not wired into the agent loop**:

| Component | File | Status |
|-----------|------|--------|
| Discovery | `internal/skills/discovery.go` | Working |
| Parser | `internal/skills/parser.go` | Working |
| Registry | `internal/skills/registry.go` | Working |
| Executor | `internal/skills/executor.go` | Implemented, not called |
| Models | `internal/skills/models.go` | Working |

### What Exists

1. **Discovery** - Three-tier skill discovery:
   - `.meept/skills/` (project-local)
   - `~/.meept/skills/` (user-global)
   - `~/.config/meept/skills/` (system-wide)
   - `~/.meept/clawskills/` (third-party, `claw:` prefix)

2. **Parser** - YAML frontmatter parsing:
   ```yaml
   ---
   name: code-review
   description: Automated code review
   requires: [code, reasoning]
   allowed-tools: [file_read, web_fetch]
   risk-level: low
   max-iterations: 5
   temperature: 0.3
   ---
   # Skill body (system prompt)
   ```

3. **Registry** - Thread-safe skill management

4. **Executor** - Full implementation:
   - Model resolution based on skill requirements
   - LLM client creation/switching
   - Prompt building
   - Temperature/max_tokens control
   - Multi-turn support

### What's Missing

1. **No dispatcher integration** - Dispatcher doesn't invoke skills
2. **No RPC endpoint** - Can't trigger skills via RPC
3. **No agent loop integration** - Skills aren't used for task routing
4. **No tool filtering** - `allowed-tools` not enforced

---

## Implementation Plan

### Phase 1: Initialize Skills in Daemon

**File:** `internal/daemon/components.go`

**Changes:**

1. Add skill discovery and registry:
```go
func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize skills
    var skillRegistry *skills.Registry
    if cfg.Skills.Enabled {
        discovery := skills.NewDiscovery(cfg.Skills, logger)
        skillRegistry = skills.NewRegistry(logger)

        // Discover and register all skills
        discovered, err := discovery.DiscoverAll()
        if err != nil {
            logger.Warn("skill discovery failed", "error", err)
        } else {
            for _, skill := range discovered {
                skillRegistry.Register(skill)
            }
            logger.Info("skills loaded", "count", len(discovered))
        }
    }

    // ...
}
```

2. Create skill executor:
```go
    var skillExecutor *skills.Executor
    if skillRegistry != nil && llmResolver != nil {
        skillExecutor = skills.NewExecutor(llmResolver,
            skills.WithExecutorLogger(logger),
            skills.WithClient(llmClient))
    }
```

### Phase 2: Add Skill Invocation to Agent System

**File:** `internal/agent/spec.go`

**Changes:**

1. Add skill reference to agent specs:
```go
type AgentSpec struct {
    // ... existing fields
    PreferredSkill string   // Skill to use for this agent's tasks
    SkillTriggers  []string // Keywords that trigger skill use
}
```

2. Update dispatcher spec:
```go
var DispatcherSpec = AgentSpec{
    ID:   "dispatcher",
    // ...
    SkillTriggers: []string{"review", "debug", "deploy", "analyze"},
}
```

### Phase 3: Skill-Based Routing in Dispatcher

**File:** `internal/agent/dispatcher.go` (new)

**Changes:**

1. Create dispatcher logic:
```go
type Dispatcher struct {
    skillRegistry *skills.Registry
    skillExecutor *skills.Executor
    agentRegistry *AgentRegistry
    logger        *slog.Logger
}

func (d *Dispatcher) Route(ctx context.Context, message string) (*RouteDecision, error) {
    // 1. Check for explicit skill invocation (/skill-name)
    if strings.HasPrefix(message, "/") {
        skillName := extractSkillName(message)
        if skill := d.skillRegistry.Get(skillName); skill != nil {
            return &RouteDecision{
                Type:  RouteTypeSkill,
                Skill: skill,
            }, nil
        }
    }

    // 2. Check for keyword-based skill matching
    for _, skill := range d.skillRegistry.List() {
        for _, trigger := range skill.Triggers {
            if strings.Contains(strings.ToLower(message), trigger) {
                return &RouteDecision{
                    Type:  RouteTypeSkill,
                    Skill: skill,
                }, nil
            }
        }
    }

    // 3. Route to specialist agent
    agentID := d.classifyIntent(message)
    return &RouteDecision{
        Type:    RouteTypeAgent,
        AgentID: agentID,
    }, nil
}
```

### Phase 4: Execute Skills in Agent Loop

**File:** `internal/agent/loop.go`

**Changes:**

1. Add skill execution path:
```go
func (l *AgentLoop) RunWithSkill(ctx context.Context, skill *skills.Skill, input string) (*Response, error) {
    l.logger.Info("executing skill", "name", skill.Name)

    // Execute skill
    result, err := l.skillExecutor.Execute(ctx, skill, input)
    if err != nil {
        return nil, fmt.Errorf("skill execution failed: %w", err)
    }

    return &Response{
        Content:      result.Content,
        Model:        result.Model,
        TokensUsed:   result.TotalTokens,
        SkillUsed:    skill.Name,
    }, nil
}
```

### Phase 5: Tool Filtering by Skill

**File:** `internal/agent/executor.go`

**Changes:**

1. Filter tools based on skill's `allowed-tools`:
```go
func (e *Executor) FilterToolsForSkill(skill *skills.Skill) *tools.Registry {
    if len(skill.AllowedTools) == 0 {
        return e.registry // No filtering
    }

    filtered := tools.NewRegistry()
    for _, toolName := range skill.AllowedTools {
        if tool := e.registry.Get(toolName); tool != nil {
            filtered.Register(tool)
        }
    }
    return filtered
}
```

2. Use filtered registry during skill execution:
```go
func (l *AgentLoop) RunWithSkill(ctx context.Context, skill *skills.Skill, input string) (*Response, error) {
    // Create filtered tool registry
    filteredTools := l.executor.FilterToolsForSkill(skill)

    // Use filtered tools for this execution
    originalRegistry := l.executor.Registry()
    l.executor.SetRegistry(filteredTools)
    defer l.executor.SetRegistry(originalRegistry)

    // Execute...
}
```

### Phase 6: RPC Endpoint for Skills

**File:** `internal/rpc/proxy.go`

**Changes:**

1. Add skill execution endpoint:
```go
func (p *Proxy) RegisterHandlers() {
    // ...
    p.Handle("skills.list", p.handleSkillsList)
    p.Handle("skills.execute", p.handleSkillsExecute)
    p.Handle("skills.triage", p.handleSkillsTriage)
}

func (p *Proxy) handleSkillsExecute(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        SkillName string `json:"skill_name"`
        Input     string `json:"input"`
    }
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }

    skill := p.skillRegistry.Get(req.SkillName)
    if skill == nil {
        return nil, fmt.Errorf("skill not found: %s", req.SkillName)
    }

    result, err := p.skillExecutor.Execute(ctx, skill, req.Input)
    if err != nil {
        return nil, err
    }

    return result, nil
}
```

### Phase 7: CLI Skill Commands

**File:** `cmd/meept/skills.go`

**Changes:**

1. Add skill subcommands:
```go
var skillsCmd = &cobra.Command{
    Use:   "skills",
    Short: "Manage skills",
}

var skillsListCmd = &cobra.Command{
    Use:   "list",
    Short: "List available skills",
    Run: func(cmd *cobra.Command, args []string) {
        // Call RPC skills.list
    },
}

var skillsRunCmd = &cobra.Command{
    Use:   "run <skill-name> <input>",
    Short: "Execute a skill",
    Run: func(cmd *cobra.Command, args []string) {
        // Call RPC skills.execute
    },
}
```

---

## Testing Plan

### Unit Tests

1. **Discovery tests** (exist)
2. **Parser tests** (exist)
3. **Executor tests** (exist)
4. **Integration tests** (new):
   - Test skill invocation through dispatcher
   - Test tool filtering
   - Test model resolution

### Manual Testing

1. Create a test skill in `~/.meept/skills/test.md`
2. Invoke via CLI: `./bin/meept skills run test "test input"`
3. Invoke via TUI: `/test test input`
4. Verify tool filtering works
5. Verify model switching works

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Initialize skills |
| `internal/agent/loop.go` | Add skill execution path |
| `internal/agent/executor.go` | Add tool filtering |
| `internal/rpc/proxy.go` | Add skill RPC endpoints |
| `cmd/meept/main.go` | Add skills subcommand |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/agent/dispatcher.go` | Skill/agent routing logic |
| `cmd/meept/skills.go` | CLI skill commands |
| `tests/integration/skills_test.go` | Integration tests |

---

## Success Criteria

1. Skills are discovered and loaded on daemon startup
2. Skills can be invoked via `/skill-name` in chat
3. Skills can be invoked via CLI
4. Tool filtering works based on `allowed-tools`
5. Model switching works based on `requires`
6. Tests pass

---

## Design Deviations from Original Plan

The implementation diverges from the original plan in several intentional ways. These deviations represent a richer, more capable design than what was originally specified.

### 1. AgentSpec Fields (Phase 2)

**Plan specified:** `PreferredSkill string` and `SkillTriggers []string`
**Implemented:** `AvailableSkills []string` and `SkillTriggers map[string]string`

The implementation uses a richer design:
- `AvailableSkills []string` lists all skills the agent can invoke (not just one preferred skill)
- `SkillTriggers map[string]string` maps keywords to specific skill names, enabling multiple trigger-keyword-to-skill mappings

This is more expressive than the plan's single `PreferredSkill` field and provides structured trigger-to-skill routing rather than unstructured keyword lists.

### 2. RunWithSkill on AgentLoop (Phase 4)

**Plan specified:** `RunWithSkill()` method that calls `skillExecutor.Execute()` directly.
**Implemented:** `RunWithSkill()` runs through the full agent loop with skill constraints applied (tool filtering, iteration limits, system prompt override).

The implementation integrates skill execution into the agent loop rather than bypassing it, ensuring that:
- Tool filtering is enforced via `FilterToolsForSkill()`
- Max iterations from the skill are respected
- The skill body becomes the system prompt
- The full reasoning cycle (with cycle detection, convergence detection, token budget) runs

Additionally, skills are automatically discovered and injected as context via `discoverRelevantSkills()` + `buildSystemPromptWithSkills()` in the normal `RunOnce()` path, providing seamless skill awareness without explicit invocation.

### 3. Keyword-Based Skill Matching (Phase 3)

**Plan specified:** Keyword matching in the dispatcher that scans all skills for trigger words.
**Implemented:** Two complementary approaches:

1. **Explicit `/skill-name` invocation** in the dispatcher for direct skill routing
2. **Metadata-driven discovery** via `CapabilityIndex` with TF-IDF weighting for automatic skill context injection

The plan's approach of scanning all skills for keyword triggers in the dispatcher was replaced with a more sophisticated system:
- `CapabilityIndex` uses TF-IDF-like scoring for relevance ranking
- `KeywordExtractor` generates keywords from skill metadata (name, tags, examples, description)
- `SkillTriggers` on `AgentSpec` provides agent-level keyword-to-skill mapping

### 4. Tool Filtering Integration (Phase 5)

**Plan specified:** `Executor.FilterToolsForSkill()` method on the executor.
**Implemented:** `FilterToolsForSkill()` as a standalone function in `executor.go`, wired into both:
- `RunOnce()` - automatically filters tools when the top discovered skill has `allowed-tools`
- `RunWithSkill()` - filters tools for explicit skill execution through the agent loop

### 5. Dispatcher Integration (Phase 3)

**Plan specified:** New `internal/agent/dispatcher.go` file.
**Implemented:** Dispatcher already existed with full skill support including:
- `parseSkillInvocation()` for `/skill-name` parsing
- `getSkill()` for registry lookup
- `executeSkill()` for skill execution via `skills.Executor`
- `GetSkillRegistry()` for external access

---

## Files Actually Modified/Created

| File | Status | Notes |
|------|--------|-------|
| `internal/skills/executor.go` | Pre-existing | Full executor implementation |
| `internal/skills/registry.go` | Pre-existing | Thread-safe registry with tag/capability lookup |
| `internal/skills/discovery.go` | Pre-existing | Three-tier discovery system |
| `internal/skills/parser.go` | Pre-existing | YAML frontmatter parsing |
| `internal/skills/models.go` | Pre-existing | Skill and result types |
| `internal/skills/index.go` | Pre-existing | SkillIndex with metadata-only entries |
| `internal/skills/capability_index.go` | Pre-existing | TF-IDF skill matching |
| `internal/skills/keyword_extractor.go` | Pre-existing | Keyword extraction from metadata |
| `internal/skills/lazy_loader.go` | Pre-existing | LRU-cached on-demand skill loading |
| `internal/agent/dispatcher.go` | Pre-existing | Skill routing in ClassifyAndRoute |
| `internal/agent/executor.go` | Pre-existing | FilteredToolRegistry, FilterToolsForSkill |
| `internal/agent/spec.go` | Pre-existing | AgentSpec with AvailableSkills, SkillTriggers |
| `internal/agent/loop.go` | **Modified** | Added RunWithSkill(), tool filtering in RunOnce() |
| `tests/integration/skills_test.go` | **Modified** | Added 6 new comprehensive integration tests |
