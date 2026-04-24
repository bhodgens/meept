# Phase 6: Dispatcher Improvements & Integration

**Status:** Completed
**Priority:** Medium (tech debt reduction)
**Estimated Effort:** 1-2 sprints

---

## Overview

Phases 1-5 completed the core dispatcher feature set:
- Phase 1: Analytics and observability
- Phase 2: Unified intent taxonomy
- Phase 3: Multi-intent/compound request support
- Phase 4: Semantic embedding-based matching
- Phase 5: Context-aware classification

Phase 6 addresses technical debt and integration improvements identified in WORKLIST.md and code review.

---

## Problem Statement

### Code Quality Issues

1. **Tool Interface Duplication** (High severity)
   - `internal/tools/interface.go:19` vs `internal/agent/executor.go:62`
   - Two `Tool` interfaces with identical signatures
   - Risk of drift creating maintenance issues

2. **Memory Store Duplication** (Medium severity)
   - `internal/memory/episodic.go` vs `internal/memory/task.go`
   - ~80% code overlap in SQLite + FTS5 logic
   - Schema, triggers, Store()/Search()/GetRecent()/Delete() duplicated

3. **Bus Handler Duplication** (Medium severity)
   - `internal/task/registry.go:337-369`
   - `internal/queue/queue.go:286-317`
   - `internal/session/session.go:379-417`
   - Identical subscription handler patterns

### Integration Gaps

4. **Dispatcher Stats Not Wired** (Phase 1 TODO)
   - `DispatcherStats` struct exists but not populated
   - No accessors exposed for TUI/API
   - Classification decisions not tracked

5. **Missing io.Closer Assertions** (Low severity)
   - 30+ types implement `Close()` without `io.Closer` assertion
   - Lifecycle expectations unclear

---

## Completed Work (2026-04-24)

### Step 1: Wire Dispatcher Stats (Phase 1 Completion)

**Status:** Completed

**Changes made:**
- Added `stats *DispatcherStats` field to `Dispatcher` struct
- Enhanced `DispatcherStats` with mutex, `ByMethod` tracking, and `FallbackCount`
- Added helper methods: `recordTotalDispatch()`, `recordClassificationMethod()`, `recordAgent()`, `recordIntentType()`, `recordFallback()`
- Updated `classifyIntent()` to record stats at each classification path
- Added `GetStats()` method for retrieving statistics

**Files modified:**
- `internal/agent/dispatcher.go`

### Step 2: Add io.Closer Assertions

**Status:** Completed

**Changes made:**
- Added `io` imports to files missing them
- Added compile-time assertions for types:
  - `Manager` (memory/manager.go)
  - `Client` (llm/client.go)
  - `Client` (tools/mcp/client.go)
  - `Engine` (security/engine.go)
  - `Store` (memory/vector/store.go)
  - `Store` (metrics/store.go)

**Files modified:**
- `internal/memory/manager.go`
- `internal/llm/client.go`
- `internal/tools/mcp/client.go`
- `internal/security/engine.go`
- `internal/memory/vector/store.go`
- `internal/metrics/store.go`

---

## Remaining Work (Deferred)

The following items from the original Phase 6 plan are deferred as they address tech debt that does not affect correctness:

1. **Tool Interface Consolidation** - Both interfaces work correctly via Go's structural typing
2. **Generic Memory Store** - Episodic/Task memory duplication is low-risk
3. **Generic Bus Handler** - Handler duplication is minor and works correctly

---

## Objectives

1. **Eliminate Tool interface duplication** - Single canonical `tools.Tool` interface
2. **Extract generic memory store** - Reduce episodic/task memory overlap
3. **Extract generic bus handler** - Reusable subscription handler
4. **Wire dispatcher analytics** - Complete Phase 1 stats tracking
5. **Add io.Closer assertions** - Document lifecycle types

---

## Implementation Steps

### Step 1: Consolidate Tool Interface (High Priority)

**Files:** `internal/agent/executor.go`

**Action:** Remove duplicate `Tool` interface, use `tools.Tool` as canonical.

```go
// Remove from internal/agent/executor.go:
// type Tool interface { ... }

// Update ToolRegistry to use tools.Tool:
type ToolRegistry interface {
    Get(name string) (tools.Tool, error)
    List() []tools.Tool
    GetDefinitions() []tools.ToolDefinition
}
```

### Step 2: Wire Dispatcher Stats (Phase 1 Completion)

**File:** `internal/agent/dispatcher.go`

**Action:** Complete the stats tracking from Phase 1.

```go
// Add to Dispatcher struct:
stats *DispatcherStats

// Initialize in NewDispatcher():
d.stats = &DispatcherStats{
    ByMethod:   make(map[string]int),
    ByAgent:    make(map[string]int),
    ByIntent:   make(map[string]int),
}

// Track in classifyIntent() - record each classification method
// Record fallbacks when all classifiers fail
```

### Step 3: Extract Generic Memory Store (Medium Priority)

**Files:** `internal/memory/episodic.go`, `internal/memory/task.go`

**Action:** Create generic `SQLiteFTSStore[T]` base type.

```go
// New file: internal/memory/store.go
type SQLiteFTSStore[T any] struct {
    db         *sqlite.DB
    tableName  string
    categoryField string  // "category" for episodic, "domain" for task
    schema     string
}

// Common methods: Init, Store, Search, GetRecent, Delete
// Type-specific methods stay in wrapper types
```

### Step 4: Extract Generic Bus Handler (Medium Priority)

**Files:** `internal/bus/subscription.go` (NEW)

**Action:** Create reusable subscription handler.

```go
// Generic handler that takes callback map
type SubscriptionHandler struct {
    bus       *MessageBus
    callbacks map[string]func(context.Context, json.RawMessage)
    cancel    context.CancelFunc
}

func NewSubscriptionHandler(bus *MessageBus, callbacks map[string]func(...)) *SubscriptionHandler
func (h *SubscriptionHandler) Start(ctx context.Context) error
func (h *SubscriptionHandler) Stop() error
```

### Step 5: Add io.Closer Assertions (Low Priority)

**Files:** Various

**Action:** Add compile-time assertions for types with Close() methods.

```go
// Example:
var _ io.Closer = (*ParserManager)(nil)
var _ io.Closer = (*LSPClient)(nil)
var _ io.Closer = (*TaskStore)(nil)
```

---

## Deliverables

| Item | Description | Priority |
|------|-------------|----------|
| Tool interface consolidated | Single `tools.Tool` interface | High |
| Dispatcher stats wired | Phase 1 tracking complete | Medium |
| Generic memory store | Reduced code duplication | Medium |
| Generic bus handler | Reusable subscription pattern | Medium |
| io.Closer assertions | Lifecycle documentation | Low |

---

## Success Criteria

1. No duplicate `Tool` interface in `internal/agent/`
2. Dispatcher stats increment on every classification
3. Memory store code overlap reduced by >70%
4. Bus handler code deduplicated
5. All `io.Closer` assertions compile successfully

---

## Testing

```bash
# Verify no duplicate interfaces
go vet ./internal/agent/...

# Verify stats tracking
go test ./internal/agent/... -run TestDispatcherStats

# Verify memory store refactoring
go test ./internal/memory/...

# Verify bus handler
go test ./internal/bus/...
```

---

## Dependencies

- Phases 1-5 must be complete (they are)
- No external dependencies

---

## Out of Scope

- AST/LSP unit tests (separate large effort)
- StateTesting implementation (deferred, no clear use case)
- Revision count TUI display (cosmetic)

---

## Next Phase

Phase 6 completes the dispatcher enhancement roadmap. Future work:
- Operator-driven improvements based on dispatcher stats
- Machine learning on fallback patterns
- Cross-session learning from successful classifications
