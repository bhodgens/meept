# Dormant Wiring Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire all implemented-but-dormant features into production runtime paths, closing the gap between "tests pass" and "feature works in production."

**Architecture:** Each task wires an existing, tested implementation into its production call site — primarily daemon `components.go` construction, HTTP handler registration, or store delegation. No new features; only wiring.

**Tech Stack:** Go 1.24, SQLite, message bus, daemon component lifecycle

**Specs:**
- `docs/superpowers/specs/2026-06-23-ai-employee-design.md` — Employee system (PlanDisposer, TurnCollector, metrics, finding attachment)
- `docs/superpowers/specs/2026-06-20-llm-reasoning-effort-design.md` — Reasoning effort (Anthropic translation)
- `docs/superpowers/plans/2026-06-20-thread-based-context-partitioning.md` — Thread persistence
- `docs/plans/20260609-menubar-desktop-notifications-implementation.md` — Push notification delivery

---

## File Structure Mapping

### Files to Modify

| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Wire PlanDisposer, TurnCollector, EmitMetricFunc, OnFindingAttached, PushService |
| `internal/daemon/employee_service_adapter.go` | Add planDisposerAdapter wrapping plan.PlanManager |
| `internal/daemon/epistemic_wiring.go` | Replace FileWatcherHook no-op stub with real construction |
| `internal/session/store_sqlite.go` | Fix 4 thread stub methods to delegate to SQLiteThreadStore |
| `internal/llm/anthropic.go` | Call applyAnthropicReasoning from buildRequest |
| `internal/llm/client.go` | Add reasoning field to chatOptions, WithReasoning option, apply in Chat path |
| `internal/agent/loop.go` | Pass resolved reasoning config into chatWithFailover |
| `internal/comm/http/api_handlers.go` | Wire sessionFilterOptions into session list handler |
| `internal/configui/sections_reasoning.go` | Register buildReasoningFields in config UI |
| `internal/tui/thread_indicator.go` | Wire or delete updateFromData |

### Files to Create

| File | Responsibility |
|------|----------------|
| `internal/daemon/employee_wiring_test.go` | Integration test for employee daemon wiring |

---

## Task 1: Wire Employee PlanDisposer

The employee spec says tier-2 plans route through the existing Plan signoff workflow (spec line 294-306). `PlanDisposer` interface exists at `internal/employee/manager.go:402`, `SetPlanDisposer` at `:1474`, but the daemon never calls it. `Manager.ApprovePlan`/`RejectPlan` always return "not configured" in production.

**Files:**
- Modify: `internal/daemon/employee_service_adapter.go` (add adapter)
- Modify: `internal/daemon/components.go:2804` (wire setter)
- Test: `internal/daemon/employee_wiring_test.go`

- [ ] **Step 1: Write the wiring test**

```go
// internal/daemon/employee_wiring_test.go
package daemon

import (
	"testing"
)

func TestPlanDisposerWired(t *testing.T) {
	// Verify that EmployeeManager has a non-nil planDisposer after components init.
	// We can't call NewComponents in a unit test, so we verify the adapter type
	// exists and is constructable from a plan.PlanManager.
	cfg := setupTestComponents(t)
	if cfg.EmployeeManager == nil {
		t.Skip("EmployeeManager not constructed (missing config)")
	}
	// ApprovePlan should NOT return "not configured" when PlanManager is wired.
	// A non-existent plan ID will give a different error (not "not configured").
	_, err := cfg.EmployeeManager.ApprovePlan(t.Context(), "", "nonexistent", "")
	if err != nil && strings.Contains(err.Error(), "not configured") {
		t.Error("PlanDisposer not wired: ApprovePlan returns 'not configured' in production")
	}
}
```

- [ ] **Step 2: Add planDisposerAdapter**

Add to `internal/daemon/employee_service_adapter.go`:

```go
// planDisposerAdapter wraps *plan.PlanManager to satisfy
// employee.PlanDisposer without creating an import cycle.
type planDisposerAdapter struct {
	pm *plan.PlanManager
}

func (a *planDisposerAdapter) ApprovePlan(ctx context.Context, planID, sessionID, by string) error {
	return a.pm.ApprovePlan(ctx, planID, sessionID, by)
}

func (a *planDisposerAdapter) RejectPlan(ctx context.Context, planID, sessionID, by, reason string) error {
	return a.pm.RejectPlan(ctx, planID, sessionID, by, reason)
}
```

- [ ] **Step 3: Wire in components.go**

After line 2804 (`c.EmployeeManager.SetPeriodicAuditor(periodic)`), add:

```go
// Wire PlanDisposer so ApprovePlan/RejectPlan route to the existing
// plan.PlanManager signoff path (spec lines 294-306).
if c.PlanManager != nil {
    c.EmployeeManager.SetPlanDisposer(&planDisposerAdapter{pm: c.PlanManager})
}
```

Note: This must be OUTSIDE the `if c.LLMResolver != nil && c.EmployeeAuditStore != nil` block since PlanManager availability doesn't depend on the audit LLM. Place it after the auditor wiring block ends (after line 2815).

- [ ] **Step 4: Build and verify**

```bash
go build ./internal/daemon/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/employee_service_adapter.go internal/daemon/components.go internal/daemon/employee_wiring_test.go
git commit -m "feat(employee): wire PlanDisposer into EmployeeManager (dormant gap #1)"
```

---

## Task 2: Wire Employee SetTurnCollector

The periodic auditor needs turn records to audit (spec line 389: "Reviews the last N invocations in bulk"). `TurnCollectorFunc` exists at `internal/employee/scheduler_jobs.go:404`, `SetTurnCollector` at `:408`, but is never called from daemon code. The periodic audit runs on schedule but always finds zero turns.

The implementation needs a runtime turn collector. The simplest approach: `PostTurnAuditor` already receives `TurnRecord`s via `Audit()`. Add a ring buffer to `PostTurnAuditor` that caches the last N turns per employee, and expose a collector function that reads from it.

**Files:**
- Modify: `internal/employee/enforcement.go` (add turn cache to PostTurnAuditor)
- Modify: `internal/daemon/components.go` (wire SetTurnCollector)

- [ ] **Step 1: Add turn cache to PostTurnAuditor**

In `internal/employee/enforcement.go`, add to the `PostTurnAuditor` struct (near line 1060):

```go
// turnCache stores recent TurnRecords per employee for the periodic
// auditor to bulk-review. Capped at maxCachedTurns per employee.
turnCache   map[string][]TurnRecord
turnCacheMu sync.RWMutex
```

Add constants near the struct:

```go
const maxCachedTurns = 50
```

- [ ] **Step 2: Cache turns during Audit()**

In `PostTurnAuditor.Audit()` at line 1119, after the method snapshots fields but before the LLM call, add:

```go
// Cache the turn for the periodic auditor (spec line 389).
a.turnCacheMu.Lock()
empTurns := a.turnCache[turn.EmployeeID]
empTurns = append(empTurns, turn)
if len(empTurns) > maxCachedTurns {
    empTurns = empTurns[len(empTurns)-maxCachedTurns:]
}
a.turnCache[turn.EmployeeID] = empTurns
a.turnCacheMu.Unlock()
```

Initialize `turnCache` in `NewPostTurnAuditor`:

```go
func NewPostTurnAuditor(model llm.Chatter, store *AuditStore, prompt string) *PostTurnAuditor {
	return &PostTurnAuditor{
		model:     model,
		store:     store,
		prompt:    prompt,
		turnCache: make(map[string][]TurnRecord),
	}
}
```

- [ ] **Step 3: Add RecentTurns method**

```go
// RecentTurns returns cached turns for an employee. Called by the
// periodic audit job via TurnCollectorFunc.
func (a *PostTurnAuditor) RecentTurns(employeeID string, limit int, lookback time.Duration) []TurnRecord {
	a.turnCacheMu.RLock()
	defer a.turnCacheMu.RUnlock()

	turns := a.turnCache[employeeID]
	if len(turns) == 0 {
		return nil
	}

	// Apply lookback filter
	cutoff := time.Now().Add(-lookback)
	var filtered []TurnRecord
	for _, t := range turns {
		// TurnRecord doesn't have a timestamp; use the cache order
		// (most recent appended last). For now, return last N.
		_ = cutoff // lookback is advisory; cache is already bounded
	}
	filtered = turns
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}
```

- [ ] **Step 4: Wire SetTurnCollector in daemon**

In `internal/daemon/components.go`, after the PostTurnAuditor construction (line ~2801), add:

```go
// Wire TurnCollector so the periodic audit job has turns to review
// (spec line 389). The collector reads from PostTurnAuditor's cache.
c.EmployeeManager.SetTurnCollector(func(employeeID string, limit int, lookback time.Duration) []employee.TurnRecord {
    return postTurn.RecentTurns(employeeID, limit, lookback)
})
```

- [ ] **Step 5: Build and run employee tests**

```bash
go build ./internal/employee/... ./internal/daemon/...
go test ./internal/employee/... -run TestPostTurnAuditor -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/employee/enforcement.go internal/daemon/components.go
git commit -m "feat(employee): wire TurnCollector from PostTurnAuditor cache (dormant gap #2)"
```

---

## Task 3: Wire Employee EmitMetricFunc

The spec says `employee.goal.health` metric is emitted (spec line 675). `SetEmitMetricFunc` exists at `internal/employee/goal_loop.go:376` but is never called from daemon code. The daemon has an EventEmitter for bus-based metric publishing.

**Files:**
- Modify: `internal/daemon/components.go` (wire SetEmitMetricFunc on each GoalLoop)

- [ ] **Step 1: Wire EmitMetricFunc in the GoalLoop registration block**

In `internal/daemon/components.go`, inside the per-employee GoalLoop registration (after line 3007 `c.EmployeeManager.RegisterGoalLoop(emp.ID, loop)`), add before the RegisterGoalLoop call:

```go
// Wire EmitMetricFunc to publish metrics via the event bus
// (spec line 675: employee.goal.health gauge).
if c.EventEmitter != nil {
    emitter := c.EventEmitter
    loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
        emitter.EmitMetric(name, value, tags)
    })
}
```

Check whether `EventEmitter` has an `EmitMetric` method. If not, publish via the bus directly:

```go
// Wire EmitMetricFunc to publish metrics via the message bus.
loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
    _ = msgBus.Publish(models.MessageTypeEvent, "employee.metric", map[string]any{
        "name": name, "value": value, "tags": tags,
    })
})
```

- [ ] **Step 2: Verify EventEmitter API**

```bash
grep -n 'EmitMetric\|func.*EventEmitter.*Publish' internal/daemon/components.go internal/agent/events.go
```

Use whichever method exists. If neither exists, use the bus publish approach above.

- [ ] **Step 3: Build and test**

```bash
go build ./internal/daemon/...
go test ./internal/employee/... -run TestMetrics -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(employee): wire EmitMetricFunc for goal.health telemetry (dormant gap #3)"
```

---

## Task 4: Wire Employee SetOnFindingAttached

The spec says findings attach to goals (spec line 382: "attach to owning Goal"). `SetOnFindingAttached` exists at `internal/employee/enforcement.go:1095`. The callback should call `Goal.AttachFinding` and persist via `GoalStore`.

**Files:**
- Modify: `internal/daemon/components.go` (wire SetOnFindingAttached)

- [ ] **Step 1: Wire SetOnFindingAttached after PostTurnAuditor construction**

In `internal/daemon/components.go`, after `postTurn.SetBusPublisher(c.empBusPub)` (line 2801), add:

```go
// Wire OnFindingAttached so findings are linked to goals
// via Goal.AttachFinding (spec line 382). The callback loads
// the goal, attaches the finding, and persists.
if c.EmployeeGoalStore != nil {
    gs := c.EmployeeGoalStore
    postTurn.SetOnFindingAttached(func(goalID, findingID string) {
        goal, err := gs.Get(context.Background(), goalID)
        if err != nil || goal == nil {
            return // goal not found; finding unlinked
        }
        goal.AttachFinding(findingID)
        if err := gs.Save(context.Background(), goal); err != nil {
            c.Logger.Warn("failed to persist finding attachment",
                "goal_id", goalID, "finding_id", findingID, "error", err)
        }
    })
}
```

- [ ] **Step 2: Build and test**

```bash
go build ./internal/daemon/...
go test ./internal/employee/... -run TestAttachFinding -v
```

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(employee): wire OnFindingAttached to Goal.AttachFinding (dormant gap #4)"
```

---

## Task 5: Wire FileWatcherHook

`internal/agent/file_watcher.go` is fully implemented (240+ lines, 9 tests) but the daemon wiring at `internal/daemon/epistemic_wiring.go:158` is a no-op stub. Config schema exists at `internal/config/schema.go:2191` (`FileWatcherHookConfig`).

**Files:**
- Modify: `internal/daemon/epistemic_wiring.go:158` (replace stub)
- Reference: `internal/agent/file_watcher.go` (existing implementation)
- Reference: `internal/config/schema.go:2191` (FileWatcherHookConfig)

- [ ] **Step 1: Read FileWatcherHookConfig schema**

```bash
grep -A 15 'type FileWatcherHookConfig struct' internal/config/schema.go
```

- [ ] **Step 2: Replace the stub**

Replace the body of `wireFileWatcherHook` at `internal/daemon/epistemic_wiring.go:158`:

```go
func wireFileWatcherHook(agentLoop *agent.AgentLoop, cfg config.Config, bus *bus.MessageBus, logger *slog.Logger) {
	if agentLoop == nil {
		return
	}
	fwCfg := cfg.Hooks.FileWatcher
	if !fwCfg.Enabled {
		return
	}
	if fwCfg.Pattern == "" {
		logger.Debug("file watcher hook disabled: no pattern configured")
		return
	}

	debounce := time.Duration(fwCfg.DebounceMs) * time.Millisecond
	if debounce == 0 {
		debounce = 500 * time.Millisecond
	}

	hook := agent.NewFileWatcherHook(
		fwCfg.Pattern,
		debounce,
		fwCfg.Ignore,
		logger.With("component", "file-watcher-hook"),
	)
	hook.SetBus(bus)

	agentLoop.SetFileWatcher(hook)
	logger.Info("file watcher hook wired",
		"pattern", fwCfg.Pattern,
		"debounce", debounce,
		"ignore_count", len(fwCfg.Ignore),
	)
}
```

- [ ] **Step 3: Build and verify**

```bash
go build ./internal/daemon/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/epistemic_wiring.go
git commit -m "feat(agent): wire FileWatcherHook from config (dormant gap #5)"
```

---

## Task 6: Fix Thread Store Delegation Stubs

`SQLiteStore` has 4 methods that are stubs: `GetActiveThread` (returns nil,nil), `ListThreadsBySession` (returns empty), `GetThread` (returns "not found"), `DeleteThread` (returns "not found"). Meanwhile `CreateThread`, `UpdateThread`, `SetActiveThread` already delegate to `SQLiteThreadStore`. The read methods should follow the same pattern.

**Files:**
- Modify: `internal/session/store_sqlite.go:2210-2253` (4 methods)

- [ ] **Step 1: Fix GetActiveThread**

```go
// GetActiveThread returns the active thread for a session.
func (s *SQLiteStore) GetActiveThread(ctx context.Context, sessionID string) (*Thread, error) {
	ts := NewSQLiteThreadStore(s.db, sessionID, nil)
	return ts.GetActiveThread(ctx, sessionID)
}
```

- [ ] **Step 2: Fix ListThreadsBySession**

```go
// ListThreadsBySession returns all threads for a session.
func (s *SQLiteStore) ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error) {
	ts := NewSQLiteThreadStore(s.db, sessionID, nil)
	return ts.ListThreadsBySession(ctx, sessionID)
}
```

- [ ] **Step 3: Fix GetThread**

```go
// GetThread retrieves a thread by ID.
func (s *SQLiteStore) GetThread(ctx context.Context, threadID string) (*Thread, error) {
	// ThreadID is globally unique (UUID); we don't know the sessionID here.
	// Query the thread_store's GetThread which queries by ID directly.
	// We need a sessionID for NewSQLiteThreadStore but GetThread queries by
	// threadID, not sessionID. Use empty sessionID — GetThread ignores it.
	ts := NewSQLiteThreadStore(s.db, "", nil)
	return ts.GetThread(ctx, threadID)
}
```

Check whether `SQLiteThreadStore.GetThread` uses `s.sessionID` in its query. If it does, add a `GetThreadByID` method to `SQLiteThreadStore` that queries by ID only. If not, the empty sessionID is fine.

- [ ] **Step 4: Fix DeleteThread**

```go
// DeleteThread removes a thread by ID.
func (s *SQLiteStore) DeleteThread(ctx context.Context, threadID string) error {
	ts := NewSQLiteThreadStore(s.db, "", nil)
	return ts.DeleteThread(ctx, threadID)
}
```

Same sessionID caveat as Step 3.

- [ ] **Step 5: Verify SQLiteThreadStore queries**

```bash
grep -A 15 'func (s \*SQLiteThreadStore) GetThread' internal/session/thread_store.go
grep -A 10 'func (s \*SQLiteThreadStore) DeleteThread' internal/session/thread_store.go
```

If queries use `s.sessionID` in a WHERE clause, add `GetThreadByID(threadID)` and `DeleteThreadByID(threadID)` methods that omit the sessionID filter.

- [ ] **Step 6: Build and run thread store tests**

```bash
go build ./internal/session/...
go test ./internal/session/... -run TestThread -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/session/store_sqlite.go internal/session/thread_store.go
git commit -m "fix(session): delegate thread store read methods to SQLiteThreadStore (dormant gap #6)"
```

---

## Task 7: Wire applyAnthropicReasoning

The reasoning effort spec (line 737) says `anthropic.go` should call `applyAnthropicReasoning`. Currently `buildRequest` at line 712 hardcodes `req.Thinking` from capability check, ignoring any `ReasoningConfig`. This requires threading reasoning config through `chatOptions`.

**Files:**
- Modify: `internal/llm/client.go` (add reasoning to chatOptions + WithReasoning option)
- Modify: `internal/llm/client.go` (call applyOpenAICompatReasoning in Chat path)
- Modify: `internal/llm/anthropic.go:712` (call applyAnthropicReasoning)
- Modify: `internal/agent/loop.go` (pass reasoning into chatWithFailover)
- Test: `internal/llm/reasoning_wiring_test.go`

- [ ] **Step 1: Add reasoning to chatOptions**

In `internal/llm/client.go`, add to `chatOptions` struct (line 607):

```go
reasoning *ReasoningConfig
```

Add the option function:

```go
// WithReasoning sets the reasoning/thinking effort for the chat request.
func WithReasoning(rc *ReasoningConfig) ChatOption {
    return func(o *chatOptions) {
        o.reasoning = rc
    }
}
```

- [ ] **Step 2: Apply reasoning in Client.Chat**

In `Client.Chat` (the OpenAI-compatible path), after building the request body map and before sending, add:

```go
// Apply reasoning effort translation (spec §2).
if opts.reasoning != nil && c.config != nil {
    applyOpenAICompatReasoning(body, c.config, opts.reasoning, nil)
}
```

The exact location depends on where `body` is constructed. Find the `body` map construction in `Client.Chat`.

- [ ] **Step 3: Apply reasoning in AnthropicClient.buildRequest**

In `internal/llm/anthropic.go` `buildRequest` (line 594), replace lines 712-718 with:

```go
// Apply reasoning effort from chatOptions (spec §2, line 737).
// When no ReasoningConfig is provided, fall back to capability-based
// detection (legacy behavior).
if opts.reasoning != nil {
    applyAnthropicReasoning(&req, c.config, opts.reasoning, nil)
} else if c.config.HasCapability("extended_thinking") {
    req.Thinking = &anthropicThinkingConfig{
        Type: "enabled",
    }
}
```

- [ ] **Step 4: Thread reasoning from AgentLoop into chatWithFailover**

In `internal/agent/loop.go` `reasoningCycle`, before the `chatWithFailover` call (line 2458), resolve the effective reasoning config:

```go
// Resolve effective reasoning config and pass to LLM.
var reasoningCfg *llm.ReasoningConfig
if l.reasoningOverride != nil {
    reasoningCfg = l.reasoningOverride
} else if l.reasoningForNextTurn != "" {
    reasoningCfg = &llm.ReasoningConfig{Effort: l.reasoningForNextTurn}
} else if l.agentReasoning != nil {
    reasoningCfg = l.agentReasoning.ToReasoningConfig(l.agentReasoning.Effort)
}
if reasoningCfg != nil {
    chatOpts = append(chatOpts, llm.WithReasoning(reasoningCfg))
}
```

Place this block right before `response, err := l.chatWithFailover(ctx, messages, chatOpts...)` at line 2458.

- [ ] **Step 5: Clear reasoningForNextTurn after application**

After the chatWithFailover call succeeds (after line 2458), add:

```go
if l.reasoningForNextTurn != "" {
    l.reasoningForNextTurn = ""
}
```

- [ ] **Step 6: Build and test**

```bash
go build ./internal/llm/... ./internal/agent/...
go test ./internal/llm/... -run TestReasoning -v
go test ./internal/agent/... -run TestReasoning -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/llm/client.go internal/llm/anthropic.go internal/agent/loop.go
git commit -m "feat(llm): wire reasoning translation into Chat paths (dormant gap #7)"
```

---

## Task 8: Wire Push Notification System

`PushService` + `ChannelRegistry` + 4 push channel implementations (Telegram, CLI, TUI, HTTP) are fully implemented in `internal/services/push_channels.go` and `push_service.go` but never constructed by the daemon.

**Files:**
- Modify: `internal/daemon/components.go` (construct PushService + ChannelRegistry)
- Modify: `internal/daemon/components.go` (wire PushService into notification path)

- [ ] **Step 1: Add PushService to Components struct**

In `internal/daemon/components.go`, add to the Components struct:

```go
PushService *services.PushService
```

- [ ] **Step 2: Construct ChannelRegistry and PushService in NewComponents**

After the notification emitter wiring (around line 916), add:

```go
// Construct push channel registry and service.
pushRegistry := services.NewChannelRegistry(logger.With("component", "push-channels"))

// Register HTTP push channel if notification emitter is available.
if c.NotificationEmitter != nil {
    httpCh, err := services.NewHTTPPushChannel(
        &notificationEventAdapter{emitter: c.NotificationEmitter},
        logger.With("component", "push-http"),
    )
    if err == nil {
        pushRegistry.Register(httpCh)
    }
}

c.PushService = services.NewPushServiceWithChannels(msgBus, pushRegistry,
    logger.With("component", "push-service"))
```

- [ ] **Step 3: Create notificationEventAdapter**

Add a small adapter in `internal/daemon/employee_service_adapter.go` (or a new file) that wraps the daemon's `EventEmitter` to satisfy the `services.notifier` interface:

```go
// notificationEventAdapter wraps daemon's EventEmitter to satisfy
// the services.notifier interface for push channels.
type notificationEventAdapter struct {
    emitter *agent.EventEmitter // or whatever the concrete type is
}

func (a *notificationEventAdapter) Publish(event interface{}) {
    // Delegate to EventEmitter.Publish
    // The exact call depends on EventEmitter's API.
}

func (a *notificationEventAdapter) PublishNotification(sessionID, agentID string, notifType interface{}, title, message string) {
    // Delegate to EventEmitter.PublishNotification
}
```

Check the concrete EventEmitter type and adjust accordingly. The `notifier` interface in `push_channels.go` uses `interface{}` types to avoid import cycles.

- [ ] **Step 4: Wire PushService into the notification path**

Find where `EventEmitter` publishes notifications (search for `PublishNotification` calls in daemon). Add a `PushService.Push` call alongside the existing notification emission so push channels are also notified.

- [ ] **Step 5: Build and test**

```bash
go build ./internal/daemon/... ./internal/services/...
go test ./internal/services/... -run TestPush -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/components.go internal/daemon/employee_service_adapter.go
git commit -m "feat(daemon): wire PushService + ChannelRegistry (dormant gap #8)"
```

---

## Task 9: Wire Session Filter Options

`sessionFilterOptions` and `parseSessionListQuery` at `internal/comm/http/api_handlers.go:3654-3676` parse designation and limit query params but are never called.

**Files:**
- Modify: `internal/comm/http/api_handlers.go` (wire parseSessionListQuery into session list handler)

- [ ] **Step 1: Find the session list handler**

```bash
grep -n 'func.*handleSessionList\|func.*listSessions\|GET.*sessions' internal/comm/http/api_handlers.go
```

- [ ] **Step 2: Wire the filter**

In the session list handler function, replace the direct listing with:

```go
opts := parseSessionListQuery(r)
// Pass opts.Designation and opts.Limit to the store query.
sessions, err := h.store.ListSessions(ctx, opts.Designation, opts.Limit)
```

Adjust based on what the existing handler's store query looks like. The designation filter may need nil-handling (nil = no filter).

- [ ] **Step 3: Remove nolint:unused directives**

Remove the `//nolint:unused` lines from `sessionFilterOptions` and `parseSessionListQuery`.

- [ ] **Step 4: Build and test**

```bash
go build ./internal/comm/http/...
go test ./internal/comm/http/... -run TestSession -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/comm/http/api_handlers.go
git commit -m "feat(http): wire session filter options into list handler (dormant gap #9)"
```

---

## Task 10: Wire ConfigUI Reasoning Section

`buildReasoningFields` in `internal/configui/sections_reasoning.go` creates config UI fields for reasoning tier budgets but is never registered.

**Files:**
- Modify: `internal/configui/sections_reasoning.go` (remove nolint)
- Modify: `internal/configui/sections.go` or wherever sections are registered

- [ ] **Step 1: Find section registration**

```bash
grep -n 'MenuItems\|AddSection\|sections\[' internal/configui/app.go internal/configui/sections.go
```

- [ ] **Step 2: Register reasoning section**

Add a new menu item / section for reasoning config that calls `buildReasoningFields()`:

```go
app.AddSection("reasoning", "reasoning", "reasoning effort budgets", buildReasoningFields())
```

Adjust to match the existing section registration API.

- [ ] **Step 3: Remove nolint:unused**

Remove `//nolint:unused -- reserved for future reasoning config section` from `buildReasoningFields`.

- [ ] **Step 4: Build and test**

```bash
go build ./internal/configui/...
go test ./internal/configui/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/configui/
git commit -m "feat(configui): register reasoning section (dormant gap #10)"
```

---

## Task 11: Delete Dead Code

Per CLAUDE.md guidance against backwards-compat hacks, delete code with no plan, no callers, and no additive value.

**Files to clean:**
- `internal/rpc/reasoning.go:573-585` — delete `saveModelsConfigAtomic`
- `internal/preferences/store.go:60-63` — delete `setLogger`
- `internal/agent/loop.go:2856-2867` — delete `chatWithFailoverStream` (trivial wrapper)
- `internal/agent/reflection_collector.go:26` — delete `classifierModel` field (keep constructor param)
- `internal/services/push_channels.go:14-25` — delete `pushNotification` struct (unused within push system)
- `internal/tui/thread_indicator.go:225-240` — delete `updateFromData` if the alternative path covers all use cases

- [ ] **Step 1: Delete each item**

For each file above, remove the dead code and its `//nolint:unused` directive.

- [ ] **Step 2: Update chatWithFailoverStream callers**

Search for any callers of `chatWithFailoverStream`:

```bash
grep -rn 'chatWithFailoverStream' internal/agent/
```

If any exist, replace with `chatWithFailoverRaw(ctx, messages, nil, opts...)`.

- [ ] **Step 3: Clean up reflection_collector constructor**

In `internal/agent/reflection_collector.go`, keep the `classifierModel` parameter in the constructor signature (for API stability) but remove the field assignment. Add a comment:

```go
_ = classifierModel // accepted for API stability; model override not yet supported
```

- [ ] **Step 4: Build everything**

```bash
go build ./...
```

- [ ] **Step 5: Run full test suite**

```bash
go test ./... -count=1
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: delete dead code (saveModelsConfigAtomic, setLogger, chatWithFailoverStream, pushNotification, updateFromData)"
```

---

## Verification

After all tasks complete:

- [ ] **Full build:** `go build ./...`
- [ ] **Full test:** `go test ./... -count=1`
- [ ] **Race detector on employee + daemon:** `go test -race ./internal/employee/... ./internal/daemon/...`
- [ ] **Mutexio analyzer on touched files:** `go run ./tools/analyzers/mutexio/ ./internal/daemon/... ./internal/employee/...`
- [ ] **Grep verification — no stale nolint:unused in production code:**
  ```bash
  rg 'nolint:(U1000|unused)' --type go internal/ | grep -v _test.go
  ```
- [ ] **Verify no dormant setters remain:**
  ```bash
  # Every Set* method on employee/agent types should have a caller in internal/daemon/
  ```
