# Skill Evolution System -- Decision Framework

**Generated**: 2026-06-12
**Purpose**: Brainstorming and decision-making guide for implementing automatic skill creation and evolution in Meept
**Inspired by**: Hermes-Agent (Nous Research) skill system analysis

---

## Executive Summary

This document walks through **5 key architectural decisions** for implementing introspection capability across all Meept sessions. Each decision presents 2-4 options with trade-offs, followed by a **recommendation** based on Meept's existing architectural patterns.

### Capabilities Being Implemented

1. **Agent-accessible skill management** -- Create, edit, patch, archive, restore skills programmatically (no permanent deletion)
2. **Usage telemetry** -- Track skill views, executions, and modifications via plain SQLite (no FTS5)
3. **Curator process** -- Background lifecycle management (archive stale skills, LLM-driven consolidation, backup snapshots)
4. **Session introspection** -- Post-task analysis for pattern extraction (success-path only; failure-path deferred)
5. **Trajectory compression** -- Convert successful runs into reusable skill candidates via heuristic + embedding + classifier novelty gate + LLM synthesis

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
1. Meept already has robust SQLite patterns -- follow the plain `sqlx.DB` approach from `internal/security/engine.go` rather than FTS5 (usage tracking needs counts and timestamps, not full-text search)
2. No shadowing ambiguity -- skill name is primary key, not filesystem path
3. Query capability is essential for Curator ("archive skills unused in 60 days")
4. Atomic writes via transactions, no crash consistency issues
5. The daemon already depends on SQLite for 4+ subsystems

**Implementation Path**:
```go
// internal/skills/usage.go
type SkillUsageStore struct {
    db     *sqlx.DB
    logger *slog.Logger
}

// Follow security/engine.go pattern (plain sqlx.DB, no FTS5):
// - NewSkillUsageStore() with DB pool
// - initSchema() for table creation
// - BumpView(), BumpUse(), BumpPatch() for tracking
// - GetUsage(), GetAllUsage() for queries
// - DeleteUsage() for cleanup on skill deletion
//
// NOTE: Do NOT embed SQLiteFTSStore -- usage tracking is a simple
// key-value/timeseries pattern and doesn't need FTS5 overhead.
// The security engine pattern (plain table via sqlx.DB) is more appropriate.
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
// tools.skill_delete(name: string)  // NOTE: replaced with skill_archive in recommended option
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
5. Different `BuiltinRules` per action is supported (e.g., `skill_archive` = high risk, requires user confirmation)
6. **Enabled by default** -- skill management tools are wired into all agent configurations out of the box, no config flag required to activate them

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
func (m *SkillManager) Archive(ctx, name) error  // NOT Delete -- archive only, requires user confirmation
func (m *SkillManager) Restore(ctx, name) error   // Un-archive a previously archived skill

// Tools are thin wrappers
func (t *SkillCreateTool) Execute(ctx, args) (any, error) {
    name := args["name"].(string)
    content := args["content"].(string)
    err := t.mgr.Create(ctx, name, content)
    return tools.NewSuccessResult(map[string]any{"name": name}), err
}

// Registration in daemon/components.go -- wired by default, no config toggle
func RegisterSkillManagerTools(reg *tools.Registry, mgr *SkillManager) {
    reg.Register(NewSkillCreateTool(mgr))
    reg.Register(NewSkillEditTool(mgr))
    reg.Register(NewSkillPatchTool(mgr))
    reg.Register(NewSkillArchiveTool(mgr))   // Was "delete" -- now archive-only
    reg.Register(NewSkillRestoreTool(mgr))    // NEW: undo archival
    reg.Register(NewSkillListTool(mgr))
}
```

**Important: No deletion, only archival.** Agents cannot permanently delete skills. `skill_archive` moves the skill to an archived state (file renamed with `.archived` suffix or moved to `~/.meept/skills-archived/`). The user can restore archived skills via `skill_restore`. The agent must ask for user confirmation before archiving any skill, regardless of who created it.

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
    interval         time.Duration
    staleAfterDays   int
    archiveAfterDays int
    stopCh           chan struct{}
    running          bool
    mu               sync.Mutex
    bus              *bus.MessageBus
    skillsMgr        *skills.Manager
    usage            *skills.UsageTracker
    classifier       *llm.Client       // LLM for consolidation pass
    snapshotDir      string            // ~/.meept/skills/.curator_backups/
    maxSnapshots     int               // Keep last N snapshots (default: 5)
    emitter          *events.Emitter   // Publish curator.* events
    logger           *slog.Logger
}

// Lifecycle
func NewCurator(cfg CuratorConfig, bus *bus.MessageBus, mgr *skills.Manager, ...) *Curator
func (c *Curator) Start(ctx context.Context)
func (c *Curator) Stop()
func (c *Curator) IsRunning() bool

// Scheduling: inactivity-triggered (not just ticker)
// Checks if daemon has been idle before running, like Hermes does.
// Falls back to ticker if no activity signal available.
func (c *Curator) shouldRunNow() bool {
    // If last activity was < minIdleAgo, skip (daemon is busy)
    if time.Since(c.lastActivityTime) < c.minIdle {
        return false
    }
    // If last run was < interval ago, skip (too soon)
    if time.Since(c.lastRunTime) < c.interval {
        return false
    }
    return true
}

// Backup: tar.gz snapshot before every mutating cycle
func (c *Curator) createSnapshot(ctx context.Context) error {
    snapshotName := fmt.Sprintf("%s.tar.gz", time.Now().UTC().Format("2006-01-02T15-04-05"))
    snapshotPath := filepath.Join(c.snapshotDir, snapshotName)

    // Tar the entire skills tree
    // Write manifest.json with: timestamp, skill count, total size
    // Keep last c.maxSnapshots, prune older ones
    ...
}

func (c *Curator) pruneSnapshots() {
    // List snapshots sorted by name (chronological)
    // Delete all but last c.maxSnapshots
    ...
}

func (c *Curator) RestoreSnapshot(ctx context.Context, snapshotName string) error {
    // Validate snapshot exists
    // Create a pre-restore backup of current state
    // Extract tar.gz over skills directory
    // Re-index skill registry
    ...
}

// LLM-driven consolidation (inspired by Hermes curator.py:344-483)
func (c *Curator) consolidateRelated(ctx context.Context) {
    // Step 1: Gather all agent-created, active skills
    candidates := c.usage.GetActiveAgentCreatedSkills(ctx)

    // Step 2: Identify prefix clusters (skills sharing a first word or domain keyword)
    clusters := c.identifyClusters(candidates)

    // Step 3: For each cluster with 2+ members, ask the LLM
    for _, cluster := range clusters {
        if len(cluster) < 2 {
            continue
        }

        consolidation, err := c.llmConsolidationPass(ctx, cluster)
        if err != nil {
            c.logger.Warn("consolidation pass failed", "cluster", cluster[0], "error", err)
            continue
        }

        // Apply consolidation (requires user confirmation)
        c.applyConsolidation(ctx, consolidation)
    }
}

type ConsolidationResult struct {
    Action       string   // "merge_into_umbrella", "demote_to_reference", "rename"
    UmbrellaName string   // Target umbrella skill name
    MemberSkills []string // Skills to merge/demote
    Rationale    string   // Why this consolidation makes sense
}

func (c *Curator) llmConsolidationPass(ctx context.Context, cluster []SkillInfo) (*ConsolidationResult, error) {
    prompt := fmt.Sprintf(`Analyze these related skills and determine if they should be consolidated.
Target shape: CLASS-LEVEL skills, each with rich content and references.
NOT a long flat list of narrow one-session-one-skill entries.

Skills in cluster:
%s

Respond with JSON:
{
  "action": "merge_into_umbrella" | "demote_to_reference" | "no_action",
  "umbrella_name": "proposed-umbrella-name",
  "member_skills": ["skill-a", "skill-b"],
  "rationale": "why this consolidation improves the library"
}`, formatClusterForPrompt(cluster))

    response, err := c.classifier.Chat(ctx, prompt)
    if err != nil {
        return nil, err
    }

    result := &ConsolidationResult{}
    if err := json.Unmarshal([]byte(response), result); err != nil {
        return nil, err
    }
    return result, nil
}

func (c *Curator) applyConsolidation(ctx context.Context, result *ConsolidationResult) {
    // Queue consolidation as a plan for user approval
    // User sees: "Merge skills X, Y, Z into umbrella 'foo-bar'"
    // On approval: merge content, move demoted skills to references/, archive originals
    plan := &plan.Plan{
        Title:       fmt.Sprintf("Consolidate: %s → %s", strings.Join(result.MemberSkills, ", "), result.UmbrellaName),
        Description: result.Rationale,
        Type:        plan.TypeSkillConsolidation,
        Status:      plan.StatusPending,
        Metadata: map[string]any{
            "action":       result.Action,
            "umbrella":     result.UmbrellaName,
            "members":      result.MemberSkills,
        },
    }
    c.planStore.Create(ctx, plan)
}

// Internal cycle
func (c *Curator) runCycle(ctx) {
    // Step 0: Create backup snapshot before any mutations
    if err := c.createSnapshot(ctx); err != nil {
        c.logger.Error("snapshot failed, skipping cycle", "error", err)
        return
    }

    // Step 1: Transition lifecycles (active -> stale -> archived)
    c.transitionLifecycles()

    // Step 2: LLM-driven consolidation (cluster + merge)
    c.consolidateRelated(ctx)

    // Step 3: Cleanup old snapshots (keep last N)
    c.pruneSnapshots()

    c.publishEvent("curator.scan.completed")
}

// Wiring in daemon/components.go
if cfg.Curator.Enabled {
    classifierClient := llm.NewClient(cfg.Skills.SynthesisModel)  // Reuse synthesis model for curator LLM passes
    c.Curator = curator.New(cfg.Curator, msgBus, skillsMgr, usageTracker, classifierClient, planStore, emitter, logger)
}

// Start in Components.Start():
go c.Curator.Start(ctx)

// Stop in Components.Stop():
if c.Curator != nil {
    c.Curator.Stop()
}
```

**Dependency Note**: The Curator depends on usage telemetry data accumulated during Phase 1. After Phase 1 ships, the usage tracker needs to run for a sufficient period (default: `stale_after_days`) before the Curator has enough data to make informed lifecycle decisions. The Curator's first cycle will likely find no stale skills -- this is expected and correct behavior. Do not lower thresholds to compensate.

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
    qDetector    *q.PatternDetector
    skillManager *SkillManager
    llm          *llm.Client      // Synthesis model (expensive)
    classifier   *llm.Client      // Review/novelty model (cheaper)
    embedder     embedding.Client // For NoveltyScore computation
    planStore    *plan.Store      // For queuing candidates
    logger       *slog.Logger
}

// For RunOnce entry
func (e *SkillExtractor) AnalyzeConversation(ctx, conv, response) {
    // 1. Build enhanced trajectory (with tool calls)
    // 2. Run heuristic judgment (success rate, tool count)
    // 3. Run LLM review pass (skill-extraction SKILL.md criteria)
    // 4. Compute novelty (embedding + classifier disambiguation)
    // 5. Preference order: patch existing → add reference → create new
    // 6. If qualifies, extract skill candidate
    // 4. Optionally auto-create or queue for approval
}

// For RunWithTask entry
func (e *SkillExtractor) AnalyzeTask(ctx, task *task.Task, response) {
    // 1. Get task summary and build trajectory
    // 2. Run heuristic judgment (success rate, tool count)
    // 3. Run LLM review pass (skill-extraction SKILL.md criteria)
    // 4. Compute novelty (embedding + classifier disambiguation)
    // 5. Preference order: patch existing → add reference → create new
    // 6. Store via plan approval queue or auto-create
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

**Future Enhancement -- Failure Path Introspection**: The current design only hooks into success paths (`err == nil`). Some of the most valuable skill extraction comes from "how I debugged and recovered from failure" trajectories. A future phase should add failure-path introspection:

```go
// Future: also extract skills from successful recovery
if l.skillExtractor != nil && err != nil {
    go l.skillExtractor.AnalyzeRecovery(ctx, conv, finalResponse, err)
}
```

This is deferred because recovery trajectories are harder to judge for quality and require additional heuristic tuning. Track as a follow-up item.

---

## Decision 5: Trajectory Compression for Skill Extraction

**Question**: How should successful task runs be compressed into skill candidates?

**Decision**: Use Option C (Hybrid) with a **dedicated synthesis model** configured in `config/meept.json5`.

### Model Selection for Synthesis

Following Meept's skill execution pattern (`internal/skills/executor.go`), the skill synthesis component will use a **dedicated model configuration**:

**Extend existing `SkillsConfig` in `internal/config/schema.go`** (currently has `Enabled`, `SearchPaths`, `AutoReload`, `CacheSize` -- add new fields):

```go
type SkillsConfig struct {
    // Existing fields (do NOT remove):
    Enabled     bool     `json:"enabled"           toml:"enabled"`
    SearchPaths []string `json:"search_paths"      toml:"search_paths"`
    AutoReload  bool     `json:"auto_reload"       toml:"auto_reload"`
    CacheSize   int      `json:"max_cached_skills" toml:"max_cached_skills"`

    // NEW fields for skill evolution:
    AutoCreate     bool           `json:"auto_create"                toml:"auto_create"`                // Auto-extract from tasks
    SynthesisModel *ModelOverride `json:"synthesis_model,omitempty"  toml:"synthesis_model,omitempty"`  // Dedicated model for skill synthesis

    Curator CuratorConfig `json:"curator" toml:"curator"`
    Usage   UsageConfig   `json:"usage"   toml:"usage"`
}

type ModelOverride struct {
    ProviderID  string  `json:"provider_id"  toml:"provider_id"`
    ModelID     string  `json:"model_id"     toml:"model_id"`
    Temperature float64 `json:"temperature"  toml:"temperature"`
    MaxTokens   int     `json:"max_tokens"   toml:"max_tokens"`
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
    classifier   *llm.Client  // Cheaper model for review pass
}

func (e *SkillExtractor) AnalyzeAndExtract(ctx, trajectory) error {
    // Stage 1: Heuristic qualification (cheap, deterministic)
    if !e.qualifiesForSkill(trajectory) {
        return nil  // Not worth extracting
    }

    // Stage 2: LLM review pass -- Hermes-style classifier screening
    // Uses criteria defined in a skill-extraction SKILL.md (see below)
    reviewResult, err := e.runReviewPass(ctx, trajectory)
    if err != nil || !reviewResult.HasExtractableContent {
        return nil  // LLM says nothing worth extracting
    }

    // Stage 3: Novelty check -- embedding similarity + classifier disambiguation
    novelty, err := e.computeNovelty(ctx, trajectory)
    if err != nil || novelty < 0.3 {
        // Existing skill already covers this -- but maybe we should PATCH it
        if novelty >= 0.15 && novelty < 0.3 {
            return e.considerPatch(ctx, trajectory, reviewResult)
        }
        return nil  // Truly covered, nothing to do
    }

    // Stage 4: LLM synthesis (most expensive, only reached for novel candidates)
    candidate := e.synthesizeSkill(ctx, trajectory, reviewResult)

    // Stage 5: Auto-create or queue for approval
    if cfg.AutoCreateSkills {
        e.skillManager.Create(ctx, candidate.Name, candidate.Body)
    } else {
        e.queueForApproval(ctx, candidate)
    }
}

func (e *SkillExtractor) qualifies(t *Trajectory) bool {
    return t.SuccessRate >= 0.9 &&
           len(t.ToolCalls) >= 3
}
```

### Stage 2: LLM Review Pass (Hermes-Inspired)

Before investing in embedding computation and synthesis, a lightweight LLM classifier evaluates the trajectory against extraction criteria defined in a **skill-extraction skill** (`~/.meept/skills/skill-extraction/SKILL.md`). This mirrors Hermes' approach where the background review prompt defines what signals to look for -- but Meept makes the criteria configurable and skill-based.

**Skill-extraction SKILL.md** (the extraction criteria):

```markdown
---
name: skill-extraction
description: Criteria for identifying extractable patterns from task trajectories
requires: [reasoning]
---

# Skill Extraction Criteria

Review the task trajectory and determine whether it contains extractable
procedural knowledge. Look for these signals:

## Extractable Signals (any one qualifies)

1. **User corrections** -- User corrected the agent's style, tone, format,
   verbosity, or approach. Frustration signals are FIRST-CLASS skill signals.
2. **Workflow corrections** -- User redirected the sequence of steps, added
   a missing step, or removed an unnecessary one.
3. **Technique emergence** -- A non-trivial fix, workaround, debugging path,
   or tool combination emerged that was not obvious from the initial request.
4. **Skill inaccuracy** -- A loaded skill turned out wrong, missing, or
   outdated during execution.
5. **Novel tool sequence** -- A sequence of 3+ tool calls that produced a
   successful outcome and is not covered by an existing skill.

## Non-Extractable (never capture)

- Environment-dependent failures (missing binaries, path mismatches)
- Negative claims about tools or features ("X is broken")
- Session-specific transient errors that resolved before completion
- One-off task narratives without reusable structure
- Patterns that are already standard behavior (basic file read/write)

## Preference Order (highest to lowest priority)

1. **PATCH** an existing skill that was loaded during this task
2. **PATCH** an existing umbrella skill to cover this new case
3. **ADD** a reference/template to an existing umbrella skill
4. **CREATE** a new class-level umbrella skill (last resort)

## Output Format

Respond with JSON:
{
  "has_extractable_content": true/false,
  "signal_type": "user_correction|workflow_correction|technique_emergence|skill_inaccuracy|novel_tool_sequence",
  "summary": "Brief description of what was extractable",
  "existing_skill_match": "skill-name or null",
  "action": "patch|create|add_reference",
  "target_skill": "skill to patch or null for new skill",
  "extraction_notes": "Additional context for synthesis"
}
```

**Implementation**:

```go
// internal/agent/introspection.go

type ReviewResult struct {
    HasExtractableContent bool
    SignalType            string
    Summary               string
    ExistingSkillMatch    string  // Name of matched skill, or empty
    Action                string  // "patch", "create", "add_reference"
    TargetSkill           string  // Skill to patch, or empty for new
    ExtractionNotes       string
}

func (e *SkillExtractor) runReviewPass(ctx context.Context, trajectory *Trajectory) (*ReviewResult, error) {
    // Load the skill-extraction criteria SKILL.md
    criteria := e.loadExtractionCriteria(ctx)
    if criteria == "" {
        criteria = defaultExtractionCriteria  // Fallback to hardcoded defaults
    }

    // Build the review prompt with trajectory + criteria
    prompt := fmt.Sprintf(`%s

## Task Trajectory

Task: %s
Tools used: %s
Outcome: %s
Messages: %s`,
        criteria,
        trajectory.TaskName,
        trajectory.ToolSummary(),
        trajectory.Outcome,
        trajectory.MessageSummary(),
    )

    // Use classifier model (cheaper than synthesis model)
    response, err := e.classifier.Chat(ctx, prompt)
    if err != nil {
        return nil, err
    }

    // Parse structured JSON response
    result := &ReviewResult{}
    if err := json.Unmarshal([]byte(response), result); err != nil {
        // LLM didn't return valid JSON -- log and skip
        e.logger.Debug("review pass returned non-JSON, skipping", "err", err)
        return &ReviewResult{HasExtractableContent: false}, nil
    }

    return result, nil
}
```

### Preference Order -- Update Existing Skills Before Creating New Ones

Following Hermes' design, the extractor prefers updating existing skills over creating new ones. This keeps the skill library compact and class-level rather than a growing list of narrow session-specific entries.

```go
func (e *SkillExtractor) considerPatch(ctx context.Context, trajectory *Trajectory, review *ReviewResult) error {
    if review.TargetSkill == "" {
        return nil  // No target for patching
    }

    // Generate a patch proposal for the existing skill
    patch := e.synthesizePatch(ctx, trajectory, review)

    // Queue the patch for approval (patches are lower risk than new skills)
    return e.queuePatchForApproval(ctx, review.TargetSkill, patch)
}
```

### NoveltyScore Calculation

The `NoveltyScore` (0.0-1.0) determines whether a trajectory represents something new vs. something already covered by existing skills. It uses a two-stage approach:

**Stage 1: Embedding similarity against existing skill descriptions**

```go
// internal/agent/introspection.go

func (e *SkillExtractor) computeNovelty(ctx context.Context, trajectory *Trajectory) (float64, error) {
    // 1. Generate embedding for the trajectory's tool sequence + outcome summary
    trajEmbedding, err := e.embedder.Embed(ctx, trajectory.Summary())
    if err != nil {
        return 0, err
    }

    // 2. Get embeddings for all existing skill descriptions
    existingSkills := e.skillManager.ListAll(ctx)
    if len(existingSkills) == 0 {
        return 1.0, nil  // No existing skills -> maximum novelty
    }

    // 3. Compute cosine similarity against each existing skill
    maxSimilarity := 0.0
    for _, skill := range existingSkills {
        skillEmbedding, err := e.embedder.Embed(ctx, skill.Description)
        if err != nil {
            continue
        }
        sim := cosineSimilarity(trajEmbedding, skillEmbedding)
        if sim > maxSimilarity {
            maxSimilarity = sim
        }
    }

    // 4. Novelty = 1 - max_similarity
    // High similarity (>0.8) means an existing skill already covers this
    // Low similarity (<0.3) means this is genuinely novel
    return 1.0 - maxSimilarity, nil
}
```

**Stage 2: Classifier model comparison (when embedding is ambiguous)**

When embedding similarity falls in the ambiguous range (0.4-0.7), fall back to the classifier model for a more precise comparison:

```go
func (e *SkillExtractor) resolveAmbiguity(ctx context.Context, trajectory *Trajectory, candidateSkill *Skill) (bool, error) {
    // Ask the classifier model: "Does this existing skill cover the trajectory?"
    prompt := fmt.Sprintf(`Compare this existing skill with a new task trajectory.
If the existing skill's instructions would produce the same outcome as the trajectory, answer "covered".
If the trajectory demonstrates novel behavior not captured by the skill, answer "novel".

Existing skill: %s

Trajectory summary: %s`, candidateSkill.Content, trajectory.Summary())

    response, err := e.classifier.Chat(ctx, prompt)
    if err != nil {
        return false, err  // Default to not-covered on error
    }
    return strings.Contains(strings.ToLower(response), "covered"), nil
}
```

**Thresholds**:
- `NoveltyScore > 0.7`: Clearly novel, proceed to synthesis
- `NoveltyScore 0.3-0.7`: Ambiguous, run classifier model comparison
- `NoveltyScore < 0.3`: Already covered by existing skill, skip

### No Rate Limiting

Unlike other LLM-backed subsystems, skill synthesis has **no rate limit or daily cap**. The heuristic gate (success rate + tool count + novelty) already filters ~90% of candidates, and the remaining 10% represent genuinely valuable patterns worth the synthesis cost. If cost becomes a concern in practice, add budget integration via the existing `internal/llm/budget` system. Track as **GitHub issue**: `TODO(skill-evolution): add optional LLM budget integration for skill synthesis if cost monitoring shows excessive spend`.

### Approval Workflow

Skill candidates flow through the existing plan approval system (`internal/plan`) rather than a separate queue. This reuses the user's familiar `meept plans approve/reject` workflow:

```go
// Reuse internal/plan infrastructure
func (e *SkillExtractor) queueForApproval(ctx context.Context, candidate *SkillCandidate) error {
    plan := &plan.Plan{
        Title:       fmt.Sprintf("Skill: %s", candidate.Name),
        Description: candidate.Description,
        Type:        plan.TypeSkillExtraction,
        Status:      plan.StatusPending,
        Content:     candidate.Body,
        Metadata: map[string]any{
            "trajectory_ref":  candidate.TrajectoryRef,
            "novelty_score":   candidate.NoveltyScore,
            "tool_count":      candidate.ToolCallCount,
            "synthesis_model": candidate.SynthesisModel,
        },
    }
    return e.planStore.Create(ctx, plan)
}
```

CLI interaction:
```bash
meept plans list                          # Shows skill extraction plans alongside other plans
meept plans show <id>                     # Shows full skill content + trajectory reference
meept plans approve <id>                  # Creates the skill file in the appropriate tier
meept plans reject <id> --reason "..."    # Rejects; feedback stored for future synthesis tuning
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
│  ┌──────────────┐  ┌──┴───────────┐  ┌──────────────┐         │
│  │              │  │              │  │              │         │
│  │skill_archive │  │SkillManager  │  │skill_restore │         │
│  │ tool         │  │ (shared)     │  │ tool         │         │
│  │              │  │              │  │              │         │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘         │
│         │                 │                 │                   │
│         └─────────────┬───┴─────────────────┘                   │
│                       │                                         │
│         ┌─────────────┴─────────────┐                          │
│         │                           │                          │
│  ┌──────▼───────┐          ┌───────▼───────┐                   │
│  │ UsageTracker │          │ SkillWriter   │                   │
│  │  (SQLite,    │          │  (filesystem, │                   │
│  │  plain DB)   │          │  3-tier dirs) │                   │
│  └──────────────┘          └───────────────┘                   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Session Introspection                      │   │
│  │  (hooked into RunOnce + RunWithTask success paths)      │   │
│  │                                                         │   │
│  │  1. heuristic gate (success rate + tool count)          │   │
│  │  2. LLM review pass (skill-extraction SKILL.md criteria)│   │
│  │  3. novelty check (embedding + classifier disambiguation)│   │
│  │  4. preference: patch existing → create new              │   │
│  │  5. LLM synthesis → plan approval queue                 │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Curator Background Process                 │   │
│  │  (inactivity-triggered, with tar.gz snapshots)          │   │
│  │                                                         │   │
│  │  Periodic cycle:                                        │   │
│  │  1. Backup snapshot (tar.gz, keep last 5)               │   │
│  │  2. Transition lifecycles (active→stale→archived)       │   │
│  │  3. LLM-driven consolidation (cluster + merge)          │   │
│  │  4. Publish curator.* events                            │   │
│  │  NOTE: No deletion -- only archival. User can restore.  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Configuration Schema Additions

Extend the **existing** `SkillsConfig` in `internal/config/schema.go` (currently at line 946 with fields `Enabled`, `SearchPaths`, `AutoReload`, `CacheSize`). **Do not replace** -- add new fields only:

```go
type SkillsConfig struct {
    // Existing fields (preserve these):
    Enabled     bool     `json:"enabled"           toml:"enabled"`
    SearchPaths []string `json:"search_paths"      toml:"search_paths"`
    AutoReload  bool     `json:"auto_reload"       toml:"auto_reload"`
    CacheSize   int      `json:"max_cached_skills" toml:"max_cached_skills"`

    // New fields for skill evolution:
    AutoCreate     bool          `json:"auto_create"      toml:"auto_create"`       // Auto-extract from tasks (default: false)
    SynthesisModel *ModelOverride `json:"synthesis_model" toml:"synthesis_model"`   // Dedicated model for skill synthesis

    Curator CuratorConfig `json:"curator" toml:"curator"`
    Usage   UsageConfig   `json:"usage"   toml:"usage"`
}

type CuratorConfig struct {
    Enabled          bool   `json:"enabled"            toml:"enabled"`            // (default: true)
    IntervalHours    int    `json:"interval_hours"     toml:"interval_hours"`     // (default: 168, i.e. 7 days)
    MinIdleMinutes   int    `json:"min_idle_minutes"   toml:"min_idle_minutes"`   // Only run when daemon idle this long (default: 120)
    StaleAfterDays   int    `json:"stale_after_days"   toml:"stale_after_days"`   // (default: 30)
    ArchiveAfterDays int    `json:"archive_after_days" toml:"archive_after_days"` // (default: 90)
    PruneBuiltins    bool   `json:"prune_builtins"     toml:"prune_builtins"`     // (default: false)
    MaxSnapshots     int    `json:"max_snapshots"      toml:"max_snapshots"`      // Backup snapshots to keep (default: 5)
    DryRun           bool   `json:"dry_run"            toml:"dry_run"`            // Log what would happen without doing it (default: true for first N days)
}

type UsageConfig struct {
    Enabled       bool `json:"enabled"        toml:"enabled"`         // (default: true)
    RetentionDays int  `json:"retention_days" toml:"retention_days"`  // (default: 365)
    EventsEnabled bool `json:"events_enabled" toml:"events_enabled"`  // Log individual events (default: false)
}

type ModelOverride struct {
    ProviderID  string  `json:"provider_id"  toml:"provider_id"`
    ModelID     string  `json:"model_id"     toml:"model_id"`
    Temperature float64 `json:"temperature"  toml:"temperature"`
    MaxTokens   int     `json:"max_tokens"   toml:"max_tokens"`
}
```

**No fourth skill tier.** Agent-created skills are written to the existing 3-tier system (`~/.meept/skills/` by default). The `created_by` field in usage telemetry tracks whether a skill was user-authored or agent-extracted, and the Curator only manages agent-created skills. This avoids introducing a new directory tier and keeps the shadowing semantics simple.

---

## Implementation Priority

### Dependency Graph

```
Phase 1 (Skill Manager Tools)
    │
    ├──→ Phase 2 (Session Introspection)  ──→ Phase 4 (Trajectory Compression)
    │         (depends on SkillManager)         (depends on SkillExtractor)
    │
    └──→ Phase 3 (Curator)  [can run in parallel with Phase 2+4]
              (depends on UsageTracker from Phase 1,
               but needs data warm-up period before being useful)
```

Phases 2 and 3 are independent of each other and can proceed in parallel. Phase 4 depends on Phase 2's `SkillExtractor` being in place. Phase 3 (Curator) will be a no-op until enough usage data accumulates.

### Phase 1 (Week 1-2): Skill Manager Tools

- [ ] `internal/skills/usage.go` -- Plain SQLite usage tracker (no FTS5, follow `security/engine.go` pattern)
- [ ] `internal/skills/writer.go` -- Skill file writer to existing 3-tier directories
- [ ] `internal/tools/builtin/skill_manager.go` -- 6 tools (create, edit, patch, archive, restore, list) + SkillManager
- [ ] Security seed rules for new tools (archive = high risk + requires confirmation)
- [ ] Wire tools into all agent configurations by default

**Acceptance criteria**: `go test ./internal/skills/... ./internal/tools/builtin/... -v` passes with coverage >= 70%. All 6 tools registered and callable from agent loop.

### Phase 2 (Week 3-4): Session Introspection

- [ ] `internal/agent/introspection.go` -- SkillExtractor with NoveltyScore (embedding + classifier)
- [ ] Hook into `RunOnce` post-success (loop.go:1245)
- [ ] Hook into `RunWithTask` post-success (loop.go:2516)
- [ ] Heuristic qualification logic (success rate, tool count)
- [ ] LLM review pass using skill-extraction SKILL.md criteria (classifier model)
- [ ] Novelty check: embedding similarity + classifier disambiguation
- [ ] Integration with existing `internal/plan` approval system
- [ ] Seed default `~/.meept/skills/skill-extraction/SKILL.md` on first run

**Acceptance criteria**: Successful task triggers `SkillExtractor.AnalyzeAndExtract()`. LLM review pass correctly identifies extractable signals vs non-extractable noise. NoveltyScore correctly identifies duplicate vs novel patterns. Preference order prefers patching existing skills over creating new ones. Skill candidates appear in `meept plans list`.

### Phase 3 (Week 5-6, parallel with Phase 2): Curator Process

- [ ] `internal/curator/curator.go` -- Lifecycle management (archive-only, no deletion)
- [ ] Wire into daemon/components.go (Start/Stop lifecycle)
- [ ] Inactivity-triggered scheduling (run when daemon idle, not just on ticker)
- [ ] Backup snapshot system (tar.gz before every mutation, keep last 5, with rollback)
- [ ] `curator.run_now` RPC handler
- [ ] Lifecycle transition logic (active -> stale -> archived)
- [ ] LLM-driven consolidation (cluster detection + merge proposals via plan system)
- [ ] User confirmation required before any archival or consolidation action
- [ ] `curator.restore_snapshot` RPC handler for rollback

**Acceptance criteria**: Curator starts/stops cleanly. Skills with zero usage for `stale_after_days` are marked stale. Archived skills can be restored via `skill_restore`. No skill is ever permanently deleted. Consolidation proposals appear in `meept plans list` as `skill_consolidation` type. Snapshots created before each cycle, restorable via RPC.

### Phase 4 (Week 7-8): Trajectory Compression

- [ ] Enhance `buildTrajectory()` with tool call capture
- [ ] Q Agent `detectSkillOpportunity` extension
- [ ] LLM review pass with skill-extraction SKILL.md criteria (Hermes-style classifier screening)
- [ ] Novelty check: embedding similarity + classifier disambiguation
- [ ] Preference order: patch existing skill → add reference → create new (Hermes-inspired)
- [ ] LLM skill synthesis prompt with dedicated model config
- [ ] Auto-create vs plan approval workflow
- [ ] No rate limiting on synthesis (track GitHub issue for future budget integration)

**Acceptance criteria**: End-to-end flow: successful task → heuristic qualifies → LLM review pass identifies extractable signal → novelty check (embedding + classifier) → preference order selects patch or create → LLM synthesis → plan created → user approves → skill file written to `~/.meept/skills/`. `go test ./internal/agent/... -run TestSkillExtraction -v` passes.

---

## Appendix A: Key Files by Decision

| Decision | Primary Files | Secondary Files |
|----------|---------------|-----------------|
| Usage Telemetry | `internal/skills/usage.go` (new) | `internal/security/engine.go` (pattern -- plain SQLite, no FTS5) |
| Skill Manager | `internal/tools/builtin/skill_manager.go` (new) | `internal/daemon/components.go` (wiring) |
| Curator | `internal/curator/curator.go` (new) | `internal/selfimprove/scheduler.go` (pattern), `scratch/hermes/agent/curator.py` (consolidation reference) |
| Introspection | `internal/agent/introspection.go` (new) | `internal/agent/loop.go` (hooks) |
| Trajectory | `internal/agent/introspection.go` (enhance) | `internal/agent/q/pattern_detector.go` (extend), `scratch/hermes/agent/background_review.py` (review pass reference) |
| Extraction Criteria | `~/.meept/skills/skill-extraction/SKILL.md` (new) | `scratch/hermes/agent/background_review.py` lines 45-148 (prompt reference) |

---

## Appendix B: Security & Permissions

### BuiltinRules Entries

Add to `internal/agent/executor.go` line ~20-64 (`ToolActionMap`):

```go
var ToolActionMap = map[string]string{
    // ... existing mappings ...

    // Skill management tools
    "skill_create":  "skill_write",
    "skill_edit":    "skill_write",
    "skill_patch":   "skill_write",
    "skill_archive": "skill_archive",
    "skill_restore": "skill_restore",
    "skill_list":    "platform_read",
}
```

Add to `internal/security/seed_rules.go`:

```go
var SeedRules = []security.ToolRule{
    // ... existing rules ...

    {
        ToolName:           "skill_create",
        Action:             "skill_write",
        RiskLevel:          security.RiskMedium,
        Description:        "Create new skills (agent-authored procedural knowledge)",
        RequiresConfirmation: false,
        Immutable:          false,
    },
    {
        ToolName:           "skill_edit",
        Action:             "skill_write",
        RiskLevel:          security.RiskMedium,
        Description:        "Replace entire skill content",
        RequiresConfirmation: false,
        Immutable:          false,
    },
    {
        ToolName:           "skill_patch",
        Action:             "skill_write",
        RiskLevel:          security.RiskLow,
        Description:        "Targeted find-and-replace in skill files",
        RequiresConfirmation: false,
        Immutable:          false,
    },
    {
        ToolName:           "skill_archive",
        Action:             "skill_archive",
        RiskLevel:          security.RiskHigh,
        Description:        "Archive a skill (moves to archived state, can be restored)",
        RequiresConfirmation: true,  // User must confirm -- no automatic archival
        Immutable:          false,
    },
    {
        ToolName:           "skill_restore",
        Action:             "skill_restore",
        RiskLevel:          security.RiskLow,
        Description:        "Restore a previously archived skill",
        RequiresConfirmation: false,
        Immutable:          false,
    },
    {
        ToolName:           "skill_list",
        Action:             "platform_read",
        RiskLevel:          security.RiskLow,
        Description:        "List available skills",
        RequiresConfirmation: false,
        Immutable:          false,
    },
}
```

### Path Fencing for Skill Management

Skills should only be written to authorized directories within the existing 3-tier system. No fourth tier is introduced. Add to `internal/tools/builtin/skill_manager.go`:

```go
// Authorized skill writing directories (existing 3-tier system)
var authorizedSkillDirs = []string{
    "~/.meept/skills/",      // User-global (default write target for agent-created skills)
    ".meept/skills/",        // Project-local
    "~/.config/meept/skills/", // System-wide (requires elevated confirmation)
}

func (m *SkillManager) Create(ctx context.Context, name, content string) error {
    // Resolve target path -- agent-created skills default to ~/.meept/skills/
    targetDir := m.getSkillWriteTarget()  // Returns ~/.meept/skills/ by default
    skillPath := filepath.Join(targetDir, name)

    // Security fence: ensure path is within authorized dirs
    if !m.isPathAuthorized(skillPath) {
        return fmt.Errorf("skill write path %q not in authorized directories", skillPath)
    }

    // Check for name collision with bundled/system skills
    if m.isBundledSkill(name) {
        return fmt.Errorf("cannot create skill with bundled name %q; use a unique name", name)
    }

    // ... proceed with creation
}
```

---

## Appendix C: Error Handling Strategy

### Error Categories and Recovery

| Error Type | Cause | Recovery Strategy |
|------------|-------|-------------------|
| **NameCollision** | Skill with same name exists | Suggest alternative names (skill-name-2, skill-name-user) |
| **PathUnauthorized** | Write outside authorized dirs | Return clear error; do not attempt write |
| **PrerequisitesMissing** | Env vars or commands not available | List missing prereqs; user must install before use |
| **SynthesisFailed** | LLM returned malformed JSON | Retry with stricter prompt; fall back to heuristic extraction |
| **CuratorArchiveInUse** | Curator archived skill being used | Warn user; offer to restore; skip archival |
| **UsageWriteFailed** | SQLite lock contention | Retry up to 3x; log at DEBUG if fails |
| **ApprovalTimeout** | User didn't approve pending skill | Skill expires after 7 days; notify user |
| **ArchiveNotFound** | Attempt to restore non-existent archive | Return clear error with list of restorable skills |

### Implementation Pattern

```go
// internal/tools/builtin/skill_manager.go

type SkillManagerError struct {
    Code       SkillManagerErrorCode
    SkillName  string
    Detail     string
    Suggestion string  // Optional: how to resolve
    Underlying error
}

type SkillManagerErrorCode string

const (
    ErrNameCollision      SkillManagerErrorCode = "name_collision"
    ErrPathUnauthorized   SkillManagerErrorCode = "path_unauthorized"
    ErrPrerequisitesMissing SkillManagerErrorCode = "prerequisites_missing"
    ErrApprovalTimeout    SkillManagerErrorCode = "approval_timeout"
    ErrArchiveNotFound    SkillManagerErrorCode = "archive_not_found"
)

func (e *SkillManagerError) Error() string {
    if e.Suggestion != "" {
        return fmt.Sprintf("skill %s: %s. Suggestion: %s", e.SkillName, e.Detail, e.Suggestion)
    }
    return fmt.Sprintf("skill %s: %s", e.SkillName, e.Detail)
}

// Usage in Create()
func (m *SkillManager) Create(ctx context.Context, name, content string) error {
    // Check name collision
    if m.registry.Get(name) != nil {
        return &SkillManagerError{
            Code:       ErrNameCollision,
            SkillName:  name,
            Detail:     "a skill with this name already exists",
            Suggestion: fmt.Sprintf("Try %s-2 or %s-custom", name, name),
        }
    }

    // ... rest of creation logic
}
```

---

## Appendix D: Approval Workflow

### Reuse Existing Plan System

Skill candidates flow through the existing plan approval system (`internal/plan`) rather than a separate queue. This reuses the user's familiar `meept plans approve/reject` workflow and avoids creating a parallel approval infrastructure.

**Integration with `internal/plan`**:

```go
// internal/agent/introspection.go

func (e *SkillExtractor) queueForApproval(ctx context.Context, candidate *SkillCandidate) error {
    plan := &plan.Plan{
        Title:       fmt.Sprintf("Skill: %s", candidate.Name),
        Description: candidate.Description,
        Type:        plan.TypeSkillExtraction,  // New plan type
        Status:      plan.StatusPending,
        Content:     candidate.Body,
        Metadata: map[string]any{
            "trajectory_ref":  candidate.TrajectoryRef,
            "novelty_score":   candidate.NoveltyScore,
            "tool_count":      candidate.ToolCallCount,
            "synthesis_model": candidate.SynthesisModel,
        },
    }
    return e.planStore.Create(ctx, plan)
}
```

**New plan type in `internal/plan/models.go`**:

```go
const (
    TypeStandard        PlanType = "standard"
    TypeSkillExtraction PlanType = "skill_extraction"  // NEW
)
```

**Plan approval handler** -- when a `skill_extraction` plan is approved, automatically create the skill file:

```go
// internal/plan/handler.go or internal/services/plan_service.go

func (h *PlanHandler) onPlanApproved(ctx context.Context, p *plan.Plan) error {
    if p.Type == plan.TypeSkillExtraction {
        // Extract skill data from plan metadata
        name := strings.TrimPrefix(p.Title, "Skill: ")
        content := p.Content
        return h.skillManager.Create(ctx, name, content)
    }
    // ... existing plan approval handling
    return nil
}
```

### CLI Commands (existing, reused)

```bash
meept plans list                          # Shows skill extraction plans alongside other plans
meept plans show <id>                     # Shows full skill content + trajectory reference
meept plans approve <id>                  # Creates the skill file in ~/.meept/skills/
meept plans reject <id> --reason "..."    # Rejects; feedback stored for synthesis tuning
```

### UI Integration (TUI/Flutter)

When pending skill extraction plans exist:
- TUI: Show notification on startup, add "pending skill approvals" section
- Flutter: Badge on plans menu item, show skill content preview

---

## Appendix E: RPC/HTTP Endpoints

### Curator Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `curator.run_now` | POST | Trigger curator scan immediately |
| `curator.status` | GET | Get current curator state (running, interval, last run, snapshot count) |
| `curator.set_interval` | POST | Change curator run interval |
| `curator.skills_eligible` | GET | List skills eligible for archival or consolidation |
| `curator.list_snapshots` | GET | List available backup snapshots |
| `curator.restore_snapshot` | POST | Restore skills tree from a named snapshot |
| `curator.list_clusters` | GET | Show identified skill clusters pending consolidation |

### Usage Query Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `skills.usage_query` | POST | Query skill usage telemetry |
| `skills.usage_most_used` | GET | Top N most-used skills |
| `skills.usage_stale` | GET | Skills unused for N days |

### Skill Management Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `skills.archive` | POST | Archive a skill (requires confirmation) |
| `skills.restore` | POST | Restore an archived skill |
| `skills.list_archived` | GET | List archived skills |

### Request/Response Formats

**curator.run_now**:
```json
// Request
{"scan_type": "full"}  // or "lifecycle_only"

// Response
{"status": "started", "scan_id": "uuid"}
```

**skills.usage_query**:
```json
// Request
{
  "skill_name": "graphify",
  "time_range": {"from": "2026-05-01", "to": "2026-06-01"}
}

// Response
{
  "skill_name": "graphify",
  "view_count": 150,
  "use_count": 42,
  "last_used_at": "2026-06-10T14:30:00Z",
  "events": [...]
}
```

---

## Appendix F: Testing Strategy

### Unit Tests

| Component | Test Cases |
|-----------|------------|
| `SkillUsageStore` | BumpView increments, BumpUse timestamps, GetUsage returns correct record |
| `SkillManager` | Create writes file, Edit replaces content, Patch finds/replaces, Archive moves to archived state, Restore recovers |
| `Curator` | Lifecycle transitions, pinned skills skipped, no permanent deletion, snapshot create/restore, cluster identification, consolidation proposal generation |
| `SkillExtractor` | Qualification heuristic, LLM review pass with mock criteria, novelty scoring (embedding + classifier), preference order (patch vs create) |
| `ReviewResult` | JSON parsing of classifier output, fallback on malformed response, signal type classification |

### Integration Tests

| Test | Description |
|------|-------------|
| **End-to-end skill creation** | User invokes skill_create → file written to ~/.meept/skills/ → usage bumped → curator sees it |
| **End-to-end extraction** | Successful task → heuristic qualifies → LLM review pass → novelty check → synthesis → plan created → approve → skill written |
| **Preference order** | Trajectory matches existing skill → extractor patches existing skill instead of creating new one |
| **Curator lifecycle** | Create skill → simulate 30 days no-use → curator marks stale → simulate 90 days → archival (not deletion) |
| **Curator consolidation** | Create 3 overlapping agent-created skills → curator clusters them → consolidation proposal in plans → approve → merged into umbrella |
| **Curator snapshots** | Run curator cycle → verify tar.gz snapshot created → modify skills → restore snapshot → verify original state |
| **Archive and restore** | Archive skill → verify not in active list → restore → verify back in active list |
| **Approval workflow** | Extract skill → plan created → user approves → file in ~/.meept/skills/ |
| **Hermes compatibility** | Load Hermes SKILL.md → parse frontmatter → execute with tool mapping |

### Test Fixtures

```go
// internal/tools/builtin/testdata/skill_manager/

// valid_skill.md -- properly formatted skill
---
name: test-skill
description: Test skill for unit tests
requires: [reasoning]
---

# Test Skill

This is a test skill for unit testing.

// invalid_skill.md -- missing frontmatter
# Invalid Skill

No YAML frontmatter here.

// hermes_skill.md -- Hermes format
---
name: hermes-deep-research
version: 1.0.0
license: MIT
platforms: [macos, linux]
prerequisites:
  env_vars: [BRAVE_API_KEY]
  commands: [curl, jq]
---

# Hermes Deep Research Skill
```

---

## Appendix G: Migration Path for Existing Skills

### Backfilling Usage Records

On first run of the skill evolution system:

```go
// internal/skills/usage.go

func (s *SkillUsageStore) BackfillExistingSkills(ctx context.Context) error {
    // Scan all skill directories
    skillDirs, err := s.discoverAllSkillDirectories()
    if err != nil {
        return err
    }

    for _, dir := range skillDirs {
        // Create baseline record with view_count=1, created_at=now
        // This anchors the inactivity clock from "now" not epoch
        record := &SkillUsageRecord{
            Name:       dir.Name,
            CreatedBy:  "user",  // Not agent-created, so curator won't manage
            ViewCount:  1,
            CreatedAt:  time.Now(),
            State:      "active",
            Pinned:     false,
        }

        if err := s.Upsert(ctx, record); err != nil {
            s.logger.Warn("Failed to backfill usage for skill",
                "name", dir.Name, "error", err)
        }
    }

    s.logger.Info("Backfill complete", "skills_backfilled", len(skillDirs))
    return nil
}
```

### Curator Handling of Pre-existing Skills

Pre-existing skills (created before skill evolution system) carry `created_by: "user"` not `"agent"`, so:
- **Curator ignores them** -- only manages `created_by: "agent"` skills
- **User can pin them** -- pinning works for any skill
- **No auto-archival** -- user must manually archive; even agent-created skills require user confirmation before archival
- **No fourth tier** -- agent-created and user-created skills live in the same 3-tier directory structure (`~/.meept/skills/`), distinguished only by `created_by` in usage telemetry

This prevents curator from suddenly archiving user's favorite skills on first run.

---

## Appendix H: Configuration Examples

### Minimal Config (defaults)

```json5
{
  skills: {
    // Existing fields retain their defaults:
    // enabled: true, auto_reload: false, max_cached_skills: 50
    // New fields default to:
    auto_create: false,  // Don't auto-extract; queue for approval via plans
  },
}
```

### Full Configuration

```json5
{
  skills: {
    // Existing fields:
    enabled: true,
    search_paths: ["~/.hermes/skills"],
    auto_reload: false,
    max_cached_skills: 50,

    // Skill evolution fields:
    auto_create: true,
    synthesis_model: {
      provider_id: "anthropic",
      model_id: "claude-sonnet-4-5-20251001",
      temperature: 0.7,
      max_tokens: 2048,
    },

    curator: {
      enabled: true,
      interval_hours: 168,      // Run weekly (7 days), like Hermes default
      min_idle_minutes: 120,    // Only when daemon idle for 2+ hours
      stale_after_days: 30,
      archive_after_days: 90,
      prune_builtins: false,
      max_snapshots: 5,         // Keep last 5 backup snapshots
      dry_run: true,            // Log actions without executing for first period
    },

    usage: {
      enabled: true,
      retention_days: 365,
      events_enabled: false,
    },
  },
}
```

---

## Appendix I: Out of Scope

The following are explicitly **not covered** by this plan and should be tracked as separate work items:

| Item | Description | Recommended Tracking |
|------|-------------|---------------------|
| **CLI skill management commands** | `meept skills create/edit/list` commands for direct user interaction | GitHub issue |
| **HTTP/RPC endpoints for menubar** | REST API for skill management from the menubar app | GitHub issue |
| **Failure-path introspection** | Extracting skills from failed-then-recovered trajectories | Future phase -- see Decision 4 notes |
| **LLM budget integration** | Capping synthesis spend via `internal/llm/budget` | GitHub issue -- `TODO(skill-evolution): add optional LLM budget integration for skill synthesis if cost monitoring shows excessive spend` |
| **Testing harness** | Automated E2E testing of the full skill lifecycle | Follow `docs/auto-analysis/0000-testing-plan.md` |
| **Inactivity detection** | Implementing `shouldRunNow()` using message bus activity signals | GitHub issue -- fallback to ticker when no signal available |

---

## Appendix J: Relationship to Existing Claudeception Skill

The codebase already has a `claudeception` skill (`~/.claude/skills/claudeception/`) that extracts knowledge from sessions and creates new skills. There is significant overlap with the proposed `SkillExtractor`:

| Concern | Claudeception (existing) | SkillExtractor (proposed) |
|---------|------------------------|--------------------------|
| **Trigger** | Manual (`/claudeception` skill invocation) | Automatic (post-task hooks) |
| **Scope** | Any conversation | Agent task trajectories |
| **Output** | Skill files in Claude Code's skill dirs | Plans in Meept's approval system |
| **Quality gate** | LLM judgment only | Heuristic → LLM review pass (skill-extraction SKILL.md criteria) → embedding novelty → LLM synthesis |
| **Extraction criteria** | Hardcoded in skill prompt | Configurable via `~/.meept/skills/skill-extraction/SKILL.md` |
| **Preference order** | None (always creates new) | Patch existing → add reference → create new (Hermes-inspired) |
| **Storage** | `~/.claude/skills/` | `~/.meept/skills/` via Meept's 3-tier system |

**Recommendation**: Keep both systems. Claudeception operates at the Claude Code level (meta-agent, manual trigger, any context). The SkillExtractor operates at the Meept daemon level (automatic, task-specific, daemon lifecycle). They serve complementary purposes. The SkillExtractor's LLM review pass (Stage 2) is conceptually similar to Claudeception's LLM-driven extraction, but uses configurable criteria from a SKILL.md rather than a hardcoded prompt. Consider a future integration point where Claudeception's extraction logic can feed into Meept's plan approval system, but this is not a Phase 1 concern.

---

## Appendix K: Open GitHub Issues

The following GitHub issues should be created to track deferred work:

1. **`feat(skill-evolution): add LLM budget integration for skill synthesis`**
   - No rate limiting in Phase 1; monitor cost and add budget cap if needed
   - Integrate with `internal/llm/budget` system

2. **`feat(skill-evolution): failure-path introspection for skill extraction`**
   - Extract skills from "how I recovered from failure" trajectories
   - Requires additional heuristic tuning for recovery quality judgment

3. **`feat(skill-evolution): CLI commands for skill management`**
   - `meept skills create/edit/list/archive/restore` CLI commands
   - Direct user interaction without needing agent loop

4. **`feat(skill-evolution): inactivity detection for Curator scheduling`**
   - Implement `shouldRunNow()` idle detection using message bus activity signals
   - Fallback to ticker when no activity signal available
   - Track last activity time from bus events (`agent.*`, `scheduler.*` topics)

5. **`feat(skill-evolution): skill-extraction SKILL.md default seeding`**
   - On first daemon run, seed `~/.meept/skills/skill-extraction/SKILL.md` with default criteria
   - Allow users to customize extraction criteria by editing this skill
