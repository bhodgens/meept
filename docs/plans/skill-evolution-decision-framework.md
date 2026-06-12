# Skill Evolution System -- Decision Framework

**Generated**: 2026-06-12
**Purpose**: Brainstorming and decision-making guide for implementing automatic skill creation and evolution in Meept
**Inspired by**: Hermes-Agent (Nous Research) skill system analysis

---

## Executive Summary

This document walks through **5 key architectural decisions** for implementing introspection capability across all Meept sessions. Each decision presents 2-4 options with trade-offs, followed by a **recommendation** based on Meept's existing architectural patterns.

### Capabilities Being Implemented

1. **Agent-accessible skill management** -- Create, edit, patch, delete skills programmatically
2. **Usage telemetry** -- Track skill views, executions, and modifications
3. **Curator process** -- Background lifecycle management (archive stale skills, consolidate related)
4. **Session introspection** -- Post-task analysis for pattern extraction
5. **Trajectory compression** -- Convert successful runs into reusable skill candidates

---

## Decision 1: Usage Telemetry Storage

**Question**: How should skill usage tracking be persisted?

### Option A: SQLite (following Meept memory/metrics patterns)

**Implementation**:
```go
// internal/skills/usage.go
type SkillUsageTracker struct {
    db *sql.DB  // Shared pool with memory/metrics
}

// Tables:
// skill_usage: (skill_name, view_count, use_count, last_viewed_at, last_used_at)
// skill_usage_events: (skill_name, event_type, timestamp, metadata_json)
```

**Pros**:
- Follows established Meept patterns (memory/ftstore.go, metrics/store.go)
- Single file, easy backup
- Queryable (e.g., "which skills unused in 30 days?")
- No shadowing ambiguity with 3-tier skill discovery
- Atomic writes via transactions

**Cons**:
- Requires schema migration on first run
- Slightly more complex than JSON file
- Connection pool management overhead

**Meept Precedent**:
- `internal/memory/ftstore.go` -- Shared SQLite FTS5 store base
- `internal/metrics/store.go` -- Time-series metrics
- `internal/security/engine.go` -- SQLite-backed permission rules

---

### Option B: Sidecar JSON Files (following Hermes-Agent pattern)

**Implementation**:
```
~/.meept/skills/graphify/.usage.json5
~/.claude/skills/superpowers/.usage.json5
```

```json5
{
  view_count: 42,
  use_count: 7,
  last_viewed_at: "2026-06-12T10:30:00Z",
  last_patched_at: "2026-06-10T09:00:00Z",
  created_by: "agent"
}
```

**Pros**:
- No schema migrations
- Human-readable and editable
- Each skill's telemetry is collocated with skill
- No database dependency

**Cons**:
- Many small files to manage
- Race conditions across daemon connections
- No join/query capability
- Shadowing complexity (project shadows system -- which sidecar?)
- Crash consistency (flush timing)

**Hermes Precedent**: `~/.hermes/skills/.usage.json` (single file, actually -- not per-skill)

---

### Option C: Single Sidecar File (hybrid approach)

**Implementation**:
```
~/.meept/skills/.usage.json  // Single file, keyed by skill name
```

```json
{
  "graphify": { "view_count": 42, "use_count": 7, ... },
  "superpowers": { "view_count": 100, "use_count": 25, ... }
}
```

**Pros**:
- Single file like SQLite
- JSON format, no migrations
- Simple to read/write

**Cons**:
- Still has race conditions (file locking needed)
- No query capability
- Entire file must be read/written on each update
- Doesn't scale well (1000+ skills)

---

### Recommendation: **Option A (SQLite)**

**Rationale**:
1. Meept already has robust SQLite patterns -- reuse `SQLiteFTSStore` base from memory package
2. No shadowing ambiguity -- skill name is primary key, not filesystem path
3. Query capability is essential for Curator ("archive skills unused in 60 days")
4. Atomic writes via transactions, no crash consistency issues
5. The daemon already depends on SQLite for 4+ subsystems

**Implementation Path**:
```go
// internal/skills/usage.go
type SkillUsageStore struct {
    *memory.SQLiteFTSStore  // Embed the shared base
}

// Follow ftstore.go pattern:
// - NewSkillUsageStore() with DB pool
// - initSchema() for table creation
// - BumpView(), BumpUse(), BumpPatch() for tracking
// - GetUsage(), GetAllUsage() for queries
// - DeleteUsage() for cleanup on skill deletion
```

---

## Decision 2: Skill Manager Tool Architecture

**Question**: How should the skill management tool(s) be structured?

### Option A: Single Tool with Action Dispatch

```go
type SkillManagerTool struct {
    registry *skills.Registry
    writer   *skills.Writer
}

func (t *SkillManagerTool) Execute(ctx, args) (any, error) {
    action := args["action"].(string)
    switch action {
    case "create": return t.create(args)
    case "edit": return t.edit(args)
    case "patch": return t.patch(args)
    case "delete": return t.delete(args)
    }
}

// LLM sees:
// tools.skill_manager(action: "create|edit|patch|delete", name: string, ...)
```

**Pros**:
- Single registration point
- LLM only needs to remember one tool name
- Shared dependencies across actions
- Easy to add new actions

**Cons**:
- Not idiomatic Meept (no existing tool does this)
- Parameters differ wildly per action
- Harder to set different permissions per action

---

### Option B: Separate Tools per Action (Meept-idiomatic)

```go
// internal/tools/builtin/skill_manager.go
type SkillCreateTool struct{ ... }
type SkillEditTool struct{ ... }
type SkillPatchTool struct{ ... }
type SkillDeleteTool struct{ ... }
type SkillListTool struct{ ... }

// Wiring in components.go:
func RegisterSkillManagerTools(reg *tools.Registry, mgr *skills.Manager) {
    reg.Register(NewSkillCreateTool(mgr))
    reg.Register(NewSkillEditTool(mgr))
    reg.Register(NewSkillPatchTool(mgr))
    reg.Register(NewSkillDeleteTool(mgr))
}

// LLM sees:
// tools.skill_create(name: string, content: string)
// tools.skill_edit(name: string, content: string)
// tools.skill_patch(name: string, path: string, old: string, new: string)
// tools.skill_delete(name: string)
```

**Pros**:
- Follows Meept convention (team_*, scheduler_*, memory_* all do this)
- Different parameters per tool cleanly
- Different permissions per action (create=Risky, list=Safe)
- Easier to test in isolation

**Cons**:
- More boilerplate (5 structs vs 1)
- LLM must remember multiple tool names
- Shared logic needs helper functions

---

### Option C: Hybrid -- Single Manager, Multiple Wrappers

```go
// Shared manager
type SkillManager struct {
    registry *skills.Registry
    writer   *skills.Writer
    usage    *skills.UsageTracker
}

// Thin tool wrappers
type SkillCreateTool struct{ mgr *SkillManager }
type SkillEditTool struct{ mgr *SkillManager }
// ... each wrapper calls mgr.Create(), mgr.Edit(), etc.
```

**Pros**:
- Shared business logic cleanly factored
- Each tool still has focused parameters
- Manager can be tested independently
- Consistent error handling

**Cons**:
- Still has boilerplate of multiple structs
- Indirection layer adds complexity

---

### Recommendation: **Option C (Hybrid)**

**Rationale**:
1. Follows Meept convention of separate tools per action
2. Shared `SkillManager` encapsulates business logic (like `TeamOrchestrator` for team tools)
3. Manager can be made available to other layers (RPC, HTTP) without tool dependencies
4. Each tool struct is thin -- just parameter parsing + manager call
5. Different `BuiltinRules` per action is supported (e.g., `skill_delete` = high risk)

**Implementation Path**:
```go
// internal/tools/builtin/skill_manager.go

// Manager holds all dependencies
type SkillManager struct {
    registry *skills.Registry
    writer   *skills.Writer
    usage    *skills.UsageTracker
    logger   *slog.Logger
}

// Manager methods
func (m *SkillManager) Create(ctx, name, content) error
func (m *SkillManager) Edit(ctx, name, content) error
func (m *SkillManager) Patch(ctx, name, patch) error
func (m *SkillManager) Delete(ctx, name) error

// Tools are thin wrappers
func (t *SkillCreateTool) Execute(ctx, args) (any, error) {
    name := args["name"].(string)
    content := args["content"].(string)
    err := t.mgr.Create(ctx, name, content)
    return tools.NewSuccessResult(map[string]any{"name": name}), err
}

// Registration in daemon/components.go
func RegisterSkillManagerTools(reg *tools.Registry, mgr *SkillManager) {
    reg.Register(NewSkillCreateTool(mgr))
    reg.Register(NewSkillEditTool(mgr))
    reg.Register(NewSkillPatchTool(mgr))
    reg.Register(NewSkillDeleteTool(mgr))
    reg.Register(NewSkillListTool(mgr))
}
```

---

## Decision 3: Curator Process Integration

**Question**: How should the Curator background process integrate with the daemon?

### Option A: Inline Goroutine in Components.Start() (like ProgressSynthesizer)

```go
// internal/daemon/components.go
func (c *Components) Start(ctx context.Context) {
    // ... existing startups ...

    // Line ~1800
    if c.Curator != nil {
        go func() {
            ticker := time.NewTicker(c.cfg.Curator.Interval)
            for {
                select {
                case <-ctx.Done():
                    return
                case <-ticker.C:
                    c.Curator.Run(ctx)
                }
            }
        }()
    }
}
```

**Pros**:
- No extra struct needed
- Uses daemon's `ctx` for lifecycle
- Simple pattern, already used for ProgressSynthesizer

**Cons**:
- No explicit Stop() -- relies entirely on ctx cancellation
- Can't be controlled/reconfigured at runtime
- Harder to expose via RPC

---

### Option B: Dedicated Curator Struct (like selfimprove.Scheduler)

```go
// internal/curator/curator.go
type Curator struct {
    interval    time.Duration
    stopCh      chan struct{}
    running     bool
    mu          sync.Mutex
    bus         *bus.MessageBus
    skillsMgr   *skills.Manager
    logger      *slog.Logger
}

func (c *Curator) Start(ctx) {
    c.mu.Lock()
    if c.running { c.mu.Unlock(); return }
    c.running = true
    c.mu.Unlock()

    ticker := time.NewTicker(c.interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-c.stopCh:
            return
        case <-ticker.C:
            c.runCycle(ctx)
        }
    }
}

func (c *Curator) Stop() {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.running {
        close(c.stopCh)
        c.running = false
    }
}
```

**Pros**:
- Own lifecycle (independent start/stop)
- Can expose RPC handlers (`curator.run_now`, `curator.set_interval`)
- Dual-channel stop (ctx + stopCh) like selfimprove.Scheduler
- Can be queried for status

**Cons**:
- More boilerplate
- Requires explicit wiring in Components.Stop()

---

### Option C: Bus-Event-Driven (no ticker)

```go
// Curator subscribes to relevant events
bus.Subscribe("curator", "agent.lifecycle.ended")
bus.Subscribe("curator", "scheduler.job.completed")

// Runs when certain conditions are met
// (e.g., N tasks completed without curator run in X hours)
```

**Pros**:
- Reacts to actual activity, not arbitrary timer
- No wasted cycles when daemon is idle

**Cons**:
- No idle detection exists (subagent confirmed)
- Would need to track "last curator run" and "last activity"
- More complex logic than a simple ticker

---

### Recommendation: **Option B (Dedicated Struct)**

**Rationale**:
1. Mirrors `selfimprove.Scheduler` pattern exactly -- well-tested in Meept
2. Enables RPC handlers for runtime control (`curator.run_now`, `curator.status`)
3. Dual-channel stop is more robust (immediate stop via stopCh, graceful via ctx)
4. Can be extended later with pause/resume, dynamic interval adjustment
5. Fits the "component" mental model (like SecurityEngine, MemoryManager)

**Implementation Path**:
```go
// internal/curator/curator.go
type Curator struct {
    interval       time.Duration
    staleAfterDays int
    archiveAfterDays int
    stopCh         chan struct{}
    running        bool
    mu             sync.Mutex
    bus            *bus.MessageBus
    skillsMgr      *skills.Manager
    usage          *skills.UsageTracker
    emitter        *events.Emitter  // Publish curator.* events
    logger         *slog.Logger
}

// Lifecycle
func NewCurator(cfg CuratorConfig, bus *bus.MessageBus, mgr *skills.Manager, ...) *Curator
func (c *Curator) Start(ctx context.Context)
func (c *Curator) Stop()
func (c *Curator) IsRunning() bool

// Internal cycle
func (c *Curator) runCycle(ctx) {
    c.transitionLifecycles()  // active -> stale -> archived
    c.consolidateRelated()    // Merge similar skills
    c.publishEvent("curator.scan.completed")
}

// Wiring in daemon/components.go
if cfg.Curator.Enabled {
    c.Curator = curator.New(cfg.Curator, msgBus, skillsMgr, usageTracker, emitter, logger)
}

// Start in Components.Start():
go c.Curator.Start(ctx)

// Stop in Components.Stop():
if c.Curator != nil {
    c.Curator.Stop()
}
```

---

## Decision 4: Session Introspection Integration

**Question**: Where should post-task introspection hooks be placed?

### Option A: RunOnce Post-Success Path (loop.go:1245-1248)

```go
// Current code (line 1245-1248):
if l.learningPipeline != nil && err == nil {
    go l.triggerLearning(context.Background(), conv, conversationID, finalResponse)
}

// Add:
if l.skillExtractor != nil && err == nil {
    go l.skillExtractor.Analyze(context.Background(), conv, finalResponse)
}
```

**Pros**:
- Runs after ALL iterations complete -- has full conversation context
- Already established async pattern (learning pipeline)
- Access to complete message history, final response, success/failure

**Cons**:
- Only fires for `RunOnce` entry point, not `RunWithTask`
- No access to task metadata (name, ID, description)

---

### Option B: RunWithTask Post-Success Path (loop.go:2516-2524)

```go
// Current code (line 2516-2524):
if l.memvid != nil {
    go l.recordTaskExecution(context.Background(), t, response)
}
if l.taskCollector != nil {
    l.recordTaskMetrics(t, modelID, true, taskIterations, ...)
}

// Add:
if l.skillExtractor != nil {
    go l.skillExtractor.AnalyzeTask(context.Background(), t, response)
}
```

**Pros**:
- Has full `task.Task` object (name, ID, metadata, inherited memories)
- Already chains async post-work (memvid, metrics)
- Task-level scope matches "skill from completed task" semantics

**Cons**:
- Only fires for `RunWithTask` entry point, not `RunOnce`
- Duplicates work if both RunOnce and RunWithTask hooks are added

---

### Option C: Activate Dormant HookRegistry in reasoningCycle

```go
// hooks.go has interfaces but they're NEVER called
type ShouldStopAfterTurnHook interface {
    ShouldStopAfterTurn(ctx, conv, iteration) (bool, error)
}
type PrepareNextTurnHook interface {
    PrepareNextTurn(ctx, conv) error
}

// Add to reasoningCycle end (line 2093-2136):
if l.hookRegistry != nil {
    if shouldStop, err := l.hookRegistry.RunShouldStopAfterTurn(...); err != nil {
        // handle error
    }
}

// Add new interface:
type TaskCompletedHook interface {
    TaskCompleted(ctx, conv, response)
}
```

**Pros**:
- Uniform hook system across entire agent lifecycle
- Priority-ordered hook chains
- Can fire per-turn AND on final turn
- Clean separation of concerns

**Cons**:
- Requires modifying `reasoningCycle` (critical path, 2000+ line function)
- HookRegistry is currently unused -- wiring it adds complexity
- Over-engineering for "just add skill extraction"

---

### Option D: Learning Pipeline Extension

```go
// learningPipeline.Judge() already evaluates trajectory quality
// learningPipeline.Distill() already extracts patterns
// Just enhance buildTrajectory() to include tool calls

// Current: buildTrajectory() only captures user/assistant messages
// Enhanced: Also capture tool calls, arguments, results, success

func (l *AgentLoop) buildTrajectory(conv *Conversation) *Trajectory {
    steps := []TrajectoryStep{}
    for _, msg := range conv.GetMessages() {
        // Existing user/assistant handling
        // NEW: Tool call extraction
        if msg.ToolCalls != nil {
            for _, tc := range msg.ToolCalls {
                steps = append(steps, TrajectoryStep{
                    Type: "tool_call",
                    Tool: tc.Function.Name,
                    Args: tc.Function.Arguments,
                    Success: result.Success,
                })
            }
        }
    }
}
```

**Pros**:
- Learning pipeline already runs post-success
- `buildTrajectory()` is the right place to capture tool history
- Minimal changes -- just enhance existing code
- Skill extraction fits "distill successful patterns" semantics

**Cons**:
- Learning pipeline is conversation-level, not task-level
- Would need to add task metadata to trajectory

---

### Recommendation: **Option A + Option B (Both Entry Points)**

**Rationale**:
1. Both locations already have async post-work patterns -- adding skill extraction is natural
2. Covers both entry points (`RunOnce` and `RunWithTask`)
3. Minimal surgical changes -- add 2-3 lines at each location
4. Different extractors can be used (conversation-focused vs task-focused)

**Implementation Path**:
```go
// internal/agent/introspection.go
type SkillExtractor struct {
    learningPipeline *LearningPipeline
    memvid           *memvid.Client
    logger           *slog.Logger
}

// For RunOnce entry
func (e *SkillExtractor) AnalyzeConversation(ctx, conv, response) {
    // 1. Build enhanced trajectory (with tool calls)
    // 2. Run heuristic judgment (success rate, novelty)
    // 3. If qualifies, extract skill candidate
    // 4. Optionally auto-create or queue for approval
}

// For RunWithTask entry
func (e *SkillExtractor) AnalyzeTask(ctx, task *task.Task, response) {
    // 1. Get memvid task summary
    // 2. Check if task pattern is novel (not seen before)
    // 3. Extract generalized skill from task execution
    // 4. Store via SkillManager
}

// Wiring in loop.go
// Location A (line 1245-1248):
if l.skillExtractor != nil && err == nil {
    go l.skillExtractor.AnalyzeConversation(ctx, conv, finalResponse)
}

// Location B (line 2516-2524):
if l.skillExtractor != nil && err == nil {
    go l.skillExtractor.AnalyzeTask(ctx, t, response)
}
```

---

## Decision 5: Trajectory Compression for Skill Extraction

**Question**: How should successful task runs be compressed into skill candidates?

**Decision**: Use Option C (Hybrid) with a **dedicated synthesis model** configured in `config/meept.json5`.

### Model Selection for Synthesis

Following Meept's skill execution pattern (`internal/skills/executor.go`), the skill synthesis component will use a **dedicated model configuration**:

**Add to `internal/config/schema.go`**:
```go
type SkillsConfig struct {
    ExternalDirs   []string            `json:"external_dirs,omitempty"`
    AutoCreate     bool                `json:"auto_create"`
    SynthesisModel *llm.ModelConfig    `json:"synthesis_model,omitempty"`  // NEW
}
```

**Example `config/meept.json5`**:
```json5
{
  skills: {
    synthesis_model: {
      provider_id: "anthropic",
      model_id: "claude-sonnet-4-5-20251001",
      temperature: 0.7,
      max_tokens: 2048,
    },
    auto_create: true,
  }
}
```

**Rationale**:
1. **Cost control** - Skill synthesis is token-heavy; use a cheaper model than the agent's
2. **Quality tuning** - Different temperature (0.7 for creativity) than chat (0.3)
3. **Consistency** - Follows existing `models.json5` pattern
4. **Fallback** - If not configured, fall back to agent's current model

### Option A: LLM-Based Extraction (like memory consolidator)

```go
func (e *SkillExtractor) ExtractSkill(trajectory *Trajectory) (*SkillCandidate, error) {
    // Send trajectory to LLM
    prompt := `Extract a reusable skill from this successful task execution:
    ${trajectory}`

    // Expect JSON:
    response := `{
        "name": "skill-name",
        "description": "...",
        "trigger_patterns": ["when to use"],
        "steps": ["step 1", "step 2"],
        "required_tools": ["tool1", "tool2"]
    }`
}
```

**Pros**:
- Follows memory/consolidation.go pattern (summarizeWithLLM, line 448)
- Flexible -- LLM understands novel patterns
- Can generalize from one example

**Cons**:
- Token cost on every successful task
- Inconsistent output quality
- Needs prompt engineering + JSON parsing

---

### Option B: Heuristic Pattern Matching (like Q Agent detectSkillOpportunity)

```go
// internal/agent/q/pattern_detector.go:497 already has:
func (d *PatternDetector) detectSkillOpportunity(analysis *SessionAnalysis) *SkillOpportunity {
    // Looks for repeated tool sequences (count >= 5)
    // Returns opportunity with tool_sequence, frequency
}

// Extend to also check success rate:
// - If tool sequence succeeds N times without failure
// - And sequence length > threshold
// - Mark as skill candidate
```

**Pros**:
- Zero LLM cost
- Deterministic and predictable
- Already partially implemented

**Cons**:
- Only detects repetitive sequences, not novel one-offs
- Can't generalize or create instructions
- Needs LLM anyway to write skill body

---

### Option C: Hybrid -- Heuristic Trigger + LLM Synthesis

```go
// Step 1: Heuristic screening (cheap)
if trajectory.SuccessRate > 0.9 && trajectory.ToolCallCount >= 3 {
    // Step 2: LLM synthesis (only when heuristic qualifies)
    candidate := synthesizeSkillWithLLM(trajectory)
}
```

**Pros**:
- Filters out non-candidates heuristically (90% savings)
- Only pays LLM cost for promising candidates
- Combines deterministic gating with flexible synthesis

**Cons**:
- Two-stage pipeline adds complexity
- Heuristic thresholds need tuning

---

### Recommendation: **Option C (Hybrid)**

**Rationale**:
1. Q Agent already has `detectSkillOpportunity()` -- extend it
2. LLM cost is real -- only synthesize when worth it
3. Heuristic can check multiple signals:
   - Success rate (trajectory)
   - Tool call count
   - Novelty vs existing skills
   - User approval history (if task was approved)
4. LLM synthesis produces human-readable skill body

**Implementation Path**:
```go
// internal/agent/introspection.go
type SkillExtractor struct {
    qDetector    *q.PatternDetector
    skillManager *SkillManager
    llm          *llm.Client
}

func (e *SkillExtractor) AnalyzeAndExtract(ctx, trajectory) error {
    // Stage 1: Heuristic qualification
    if !e.qualifiesForSkill(trajectory) {
        return nil  // Not worth extracting
    }

    // Stage 2: Check if similar skill exists
    if e.existingSkillCovers(trajectory) {
        return nil  // Already have this skill
    }

    // Stage 3: LLM synthesis
    candidate := e.synthesizeSkill(ctx, trajectory)

    // Stage 4: Auto-create or queue for approval
    if cfg.AutoCreateSkills {
        e.skillManager.Create(ctx, candidate.Name, candidate.Body)
    } else {
        e.queueForApproval(ctx, candidate)
    }
}

func (e *SkillExtractor) qualifies(t *Trajectory) bool {
    return t.SuccessRate >= 0.9 &&
           len(t.ToolCalls) >= 3 &&
           t.NoveltyScore > 0.5
}
```

---

## Summary: Recommended Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Meept Daemon                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │              │  │              │  │              │          │
│  │ skill_create │  │ skill_edit   │  │ skill_patch  │          │
│  │ tool         │  │ tool         │  │ tool         │          │
│  │              │  │              │  │              │          │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘          │
│         │                 │                 │                   │
│         └─────────────┬───┴─────────────────┘                   │
│                       │                                         │
│              ┌────────▼────────┐                                │
│              │  SkillManager   │                                │
│              │  (shared logic) │                                │
│              └────────┬────────┘                                │
│                       │                                         │
│         ┌─────────────┴─────────────┐                          │
│         │                           │                          │
│  ┌──────▼───────┐          ┌───────▼───────┐                   │
│  │ UsageTracker │          │ SkillWriter   │                   │
│  │  (SQLite)    │          │  (filesystem) │                   │
│  └──────────────┘          └───────────────┘                   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Session Introspection                      │   │
│  │  (hooked into RunOnce + RunWithTask success paths)      │   │
│  │                                                         │   │
│  │  trajectory → heuristic check → LLM synthesis → create  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Curator Background Process                 │   │
│  │                                                         │   │
│  │  Periodic cycle:                                        │   │
│  │  1. Transition lifecycles (active→stale→archived)       │   │
│  │  2. Consolidate related skills                          │   │
│  │  3. Publish curator.* events                            │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Configuration Schema Additions

Add to `internal/config/schema.go`:

```go
type SkillsConfig struct {
    ExternalDirs []string `json:"external_dirs,omitempty"`
    AutoCreate   bool     `json:"auto_create"`   // Auto-extract from tasks
    ManagerDir   string   `json:"manager_dir"`   // ~/.meept/skills-managed/

    Curator CuratorConfig `json:"curator"`
    Usage   UsageConfig   `json:"usage"`
}

type CuratorConfig struct {
    Enabled          bool `json:"enabled"`
    IntervalHours    int  `json:"interval_hours"`
    StaleAfterDays   int  `json:"stale_after_days"`
    ArchiveAfterDays int  `json:"archive_after_days"`
    PruneBuiltins    bool `json:"prune_builtins"`
}

type UsageConfig struct {
    Enabled      bool   `json:"enabled"`
    RetentionDays int   `json:"retention_days"`
    EventsEnabled bool  `json:"events_enabled"`  // Log individual events
}
```

---

## Implementation Priority

**Phase 1 (Week 1-2)**: Skill Manager Tools
- [ ] `internal/skills/usage.go` -- SQLite usage tracker
- [ ] `internal/skills/writer.go` -- Skill file writer
- [ ] `internal/tools/builtin/skill_manager.go` -- 5 tools + manager
- [ ] Security seed rules for new tools

**Phase 2 (Week 3-4)**: Session Introspection
- [ ] `internal/agent/introspection.go` -- SkillExtractor
- [ ] Hook into `RunOnce` (loop.go:1245)
- [ ] Hook into `RunWithTask` (loop.go:2516)
- [ ] Heuristic qualification logic

**Phase 3 (Week 5-6)**: Curator Process
- [ ] `internal/curator/curator.go` -- Lifecycle management
- [ ] Wire into daemon/components.go
- [ ] `curator.run_now` RPC handler
- [ ] Lifecycle transition logic

**Phase 4 (Week 7-8)**: Trajectory Compression
- [ ] Enhance `buildTrajectory()` with tool call capture
- [ ] Q Agent `detectSkillOpportunity` extension
- [ ] LLM skill synthesis prompt
- [ ] Auto-create vs approval workflow

---

## Appendix: Key Files by Decision

| Decision | Primary Files | Secondary Files |
|----------|---------------|-----------------|
| Usage Telemetry | `internal/skills/usage.go` (new) | `internal/memory/ftstore.go` (pattern) |
| Skill Manager | `internal/tools/builtin/skill_manager.go` (new) | `internal/daemon/components.go` (wiring) |
| Curator | `internal/curator/curator.go` (new) | `internal/selfimprove/scheduler.go` (pattern) |
| Introspection | `internal/agent/introspection.go` (new) | `internal/agent/loop.go` (hooks) |
| Trajectory | `internal/agent/introspection.go` (enhance) | `internal/agent/q/pattern_detector.go` (extend) |
