# Plan: Missing Implementation and Codebase Cleanup

## Overview

This document catalogs stub code, incomplete implementations, duplicate code, and architectural inconsistencies found in the Meept codebase as of February 2025.

---

## Executive Summary

| Category | Count | Severity |
|----------|-------|----------|
| Genuine TODOs (incomplete features) | 7 | Medium |
| Duplicate/Shadowed Interfaces | 3 | High |
| Duplicate Implementations | 3 | Medium |
| Backup Files | 1 | Low |
| Outdated Documentation Comments | 8 | Low |

---

## Part 1: Incomplete Features (TODOs)

### 1.1 Web Server API Endpoints - HIGH PRIORITY

**File:** `internal/comm/web/server.go`

Three HTTP endpoints return placeholder responses:

| Line | Endpoint | Status |
|------|----------|--------|
| 245 | `POST /api/v1/memory/search` | Returns empty results array |
| 254 | `GET /api/v1/skills` | Returns empty array |
| 262 | `GET /api/v1/jobs` | Returns empty array |

**Current Implementation:**
```go
func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
    // TODO: Implement memory search
    s.writeJSON(w, http.StatusOK, map[string]any{
        "results": []any{},
        "query":   query,
    })
}
```

**Fix Required:**
- Wire up to `internal/memory/manager` for search functionality
- Connect to `internal/skills/registry` for skills listing
- Connect to `internal/scheduler` for jobs listing

---

### 1.2 Status Handler Token Tracking

**File:** `internal/daemon/components.go:1443`

```go
"tokens_used":    0, // TODO: Get from budget tracker
```

**Fix Required:** Fetch actual token usage from `internal/llm/budget` tracker.

---

### 1.3 TUI Sidebar Memory Data

**File:** `internal/tui/sidebar.go:353`

```go
Memory:        nil, // TODO: Fetch from RPC when available
```

**Fix Required:** Add RPC call to fetch memory statistics from daemon.

---

### 1.4 Task Filter "Mine" Implementation

**File:** `internal/tui/models/tasks.go:389`

```go
// TODO: Compare with current user/agent
if t.AssignedAgent != "" {
    filtered = append(filtered, t)
}
```

**Issue:** Filter doesn't actually check if the task belongs to the current user/agent.

**Fix Required:** Compare `t.AssignedAgent` against actual current user/agent ID.

---

### 1.5 Scheduler Agent Tools

**File:** `internal/agent/prompts/specialists.go:188`

```
# TODO: implement dedicated schedule, list_jobs, cancel_job tools
```

**Note:** Some of these tools may already exist as `tool_schedule_*.go` files. Verify and update prompt.

---

## Part 2: Duplicate/Shadowed Interfaces (HIGH SEVERITY)

### 2.1 Tool Interface Duplication

**CRITICAL:** Two different `Tool` interfaces exist:

**Location 1:** `internal/tools/interface.go:19-36`
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() llm.FunctionParameters  // <-- Has this
    Execute(ctx context.Context, args map[string]any) (any, error)
}
```

**Location 2:** `internal/agent/executor.go:50-57`
```go
type Tool interface {
    Name() string
    Description() string
    Execute(ctx context.Context, args map[string]any) (any, error)
    // MISSING: Parameters() method
}
```

**Impact:** Tools registered via `tools.Registry` may be incompatible with `agent.Executor`.

**Fix Required:**
1. Choose one interface as source of truth (recommend `tools.Tool`)
2. Remove duplicate definition from `agent/executor.go`
3. Update all implementations to use the canonical interface

---

### 2.2 ToolRegistry Interface Duplication

**Location 1:** `internal/tools/interface.go:62-66`
```go
type ToolExecutor interface {
    Execute(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error)
}
```

**Location 2:** `internal/agent/executor.go:61-68`
```go
type ToolRegistry interface {
    Get(name string) Tool
    List() []Tool
    GetDefinitions() []llm.ToolDefinition
}
```

**Impact:** These serve different purposes but have overlapping naming.

**Fix Required:** Rename for clarity:
- `ToolExecutor` → `ToolExecutionService`
- Keep `ToolRegistry` for query operations
- Or unify into single comprehensive interface

---

### 2.3 Chatter Interface Duplication

**Location 1:** `internal/llm/interface.go:27-37`
```go
type Chatter interface { ... }
```

**Location 2:** `internal/shadow/middleware.go:13`
```go
type LLMChatter interface { ... }
```

**Fix Required:** Remove duplicate, use `llm.Chatter` throughout.

---

## Part 3: Duplicate Implementations (MEDIUM SEVERITY)

### 3.1 EpisodicMemory vs TaskMemory

**Files:**
- `internal/memory/episodic.go`
- `internal/memory/task.go`

**Issue:** ~80% code duplication between the two memory stores.

**Shared Patterns:**
- SQLite initialization with FTS5
- Same schema structure (id, content, metadata_json, created_at)
- Same trigger patterns for FTS sync
- Similar methods: `Store()`, `Search()`, `GetRecent()`, `Delete()`, etc.

**Differences:**
- EpisodicMemory uses `category` field
- TaskMemory uses `domain` field
- TaskMemory has `FindDuplicates()` method

**Fix Required:** Refactor into generic `SQLiteFTSStore` with configurable field names.

---

### 3.2 Path Expansion Utilities (3 copies)

**Locations:**
- `internal/llm/providers.go` - `expandTildePath()`
- `internal/security/engine.go` - `expandPath()`
- `internal/skills/discovery.go` - `expandPath()`

**Fix Required:** Create shared utility package `internal/pathutil` or use `os.ExpandEnv` consistently.

---

### 3.3 Skill Type Name Collision

**Locations:**
- `internal/skills/models.go` - `Skill` (local SKILL.md files)
- `internal/clawskills/models.go` - `Skill` (remote ClawHub registry)

**Issue:** Same type name for different concepts causes confusion.

**Fix Required:** Rename one to `LocalSkill` and `RemoteSkill` or similar.

---

## Part 4: Architectural Inconsistencies

### 4.1 Inconsistent Configuration Patterns

**Two patterns coexist:**

1. **Struct-based Config:**
   - `internal/worker/worker.go:43` - `type Config struct`
   - `internal/shadow/manager.go:44` - `type ManagerConfig struct`

2. **Functional Options:**
   - `internal/skills/registry.go:17` - `type RegistryOption func(*Registry)`
   - `internal/tools/registry.go` - Uses functional options
   - `internal/agent/executor.go:107` - Uses `ExecutorOption`

**Fix Required:** Standardize on functional options pattern throughout.

---

### 4.2 Message Bus Handler Duplication

Three packages implement nearly identical message bus handlers:

| Package | File | Handler Pattern |
|---------|------|-----------------|
| task | `internal/task/registry.go:311-356` | Handler struct with Start/Stop/handleTopic |
| queue | `internal/queue/queue.go:265-358` | Same pattern |
| session | `internal/session/session.go:330-451` | Same pattern |

**Fix Required:** Extract generic `MessageBusHandler` base type.

---

### 4.3 Inconsistent io.Closer Usage

**Issue:** 30+ types implement `Close() error` but don't explicitly use `io.Closer`.

**Fix Required:** Add `io.Closer` to all Close()-implementing types for consistency.

---

## Part 5: Cleanup Required

### 5.1 Backup Files

**File:** `internal/agent/loop.go.bak`

**Action Required:** Remove or move to `archive/` directory.

---

### 5.2 Deprecated Code

**File:** `internal/session/session.go:39`

```go
// Deprecated: Use NewSQLiteStore for persistent sessions.
func NewStore(...) *MemoryStore
```

**Action Required:** Remove deprecated constructor or set removal timeline.

---

### 5.3 Outdated Documentation Comments

**File:** `internal/agent/spec.go`

| Line | Comment | Status |
|------|---------|--------|
| 134 | `// NOTE: web_search tool does not exist yet` | INCORRECT - tool exists |
| 154 | `// NOTE: exec_tool does not exist yet` | Accurate |
| 172 | `// NOTE: exec_tool and run_tests do not exist yet` | Accurate |
| 187 | `// NOTE: decompose_task and create_subtasks tools do not exist yet` | Accurate |

**Action Required:** Remove or update NOTE comments.

---

### 5.4 PlaceholderToolRegistry

**File:** `internal/agent/executor.go:387-423`

```go
// PlaceholderToolRegistry is a placeholder implementation for testing.
// This will be replaced with a real implementation in Phase 8.
```

**Status:** The "Phase 8" comment is outdated - real implementation exists in `internal/tools/registry.go`.

**Action Required:** Update comment or remove if unused.

---

## Part 6: Implementation Plan

### Phase 1: Critical Interface Consolidation (High Priority)
1. Unify `Tool` interface - choose `tools.Tool` as canonical
2. Remove duplicate from `agent/executor.go`
3. Update all tool implementations
4. Consolidate `ToolRegistry`/`ToolExecutor` interfaces
5. Remove `LLMChatter` duplicate

### Phase 2: Complete Incomplete Features (High Priority)
1. Implement web API endpoints (memory search, skills, jobs)
2. Wire up budget tracker token usage in status handler
3. Fix "FilterMine" task filter
4. Add TUI sidebar memory data fetching

### Phase 3: Code Consolidation (Medium Priority)
1. Refactor EpisodicMemory/TaskMemory into generic store
2. Create shared path expansion utility
3. Rename Skill types to avoid collision
4. Extract MessageBusHandler base type

### Phase 4: Cleanup (Low Priority)
1. Remove `loop.go.bak` backup file
2. Remove deprecated constructors
3. Update/outdated NOTE comments
4. Add `io.Closer` to all Close() implementations

---

## Critical Files Reference

| File | Issue | Priority |
|------|-------|----------|
| `internal/tools/interface.go` | Tool interface (canonical) | High |
| `internal/agent/executor.go` | Duplicate Tool interface, PlaceholderToolRegistry | High |
| `internal/comm/web/server.go` | 3 incomplete endpoints | High |
| `internal/memory/episodic.go` | Duplicate with task.go | Medium |
| `internal/memory/task.go` | Duplicate with episodic.go | Medium |
| `internal/agent/loop.go.bak` | Backup file to remove | Low |
| `internal/agent/spec.go` | Outdated NOTE comments | Low |
