# Plan: Missing Implementation and Codebase Cleanup

## Overview

Catalog of remaining stub code, duplicate code, and architectural inconsistencies in the Meept codebase. Originally drafted February 2025; refreshed 2026-04-05 against `main` to remove items that have since been resolved.

A "Resolved" appendix at the bottom records what was completed since the original audit.

---

## Executive Summary (Remaining Work)

| Category | Count | Severity |
|----------|-------|----------|
| Genuine TODOs (incomplete features) | 1 | Low |
| Duplicate/Shadowed Interfaces | 1 | High |
| Duplicate Implementations | 2 | Medium |
| Architectural Inconsistencies | 3 | Medium |
| Cleanup / Stale Comments | 3 | Low |

---

## Part 1: Incomplete Features (TODOs)

### 1.1 Scheduler Agent Tools — Prompt References Nonexistent Tools

**File:** `internal/agent/prompts/specialists.go:188`

```
# TODO: implement dedicated schedule, list_jobs, cancel_job tools
```

The scheduler agent prompt advertises `schedule`, `list_jobs`, and `cancel_job` tools that do not exist in the tool registry. Either:
- Implement the dedicated tools (`tool_schedule_*.go`), or
- Remove the prompt references and rely on existing `task_create` / `platform_status` paths.

**Priority:** Low — agent currently routes through `task_create`.

---

## Part 2: Duplicate/Shadowed Interfaces

### 2.1 `Tool` Interface Duplication — HIGH

Two `Tool` interface definitions still coexist with identical signatures:

- `internal/tools/interface.go:19` — canonical
- `internal/agent/executor.go:62` — duplicate (now has matching `Parameters()` method but is still a separate type)

**Impact:** Implementations satisfying one type aren't structurally guaranteed to satisfy the other unless Go's structural typing happens to align. Maintenance hazard: the two can drift again.

**Fix:**
1. Adopt `tools.Tool` as canonical.
2. Remove `Tool` from `agent/executor.go`; import from `internal/tools`.
3. Update `ToolRegistry` interface (in executor.go) to use `tools.Tool`.
4. Remove `PlaceholderToolRegistry` (see §5.3).

---

## Part 3: Duplicate Implementations

### 3.1 EpisodicMemory vs TaskMemory — MEDIUM

**Files:** `internal/memory/episodic.go`, `internal/memory/task.go`

~80% code duplication: SQLite + FTS5 init, schema, FTS triggers, `Store()`/`Search()`/`GetRecent()`/`Delete()` methods.

**Differences:**
- EpisodicMemory uses `category` field
- TaskMemory uses `domain` field
- TaskMemory has `FindDuplicates()`

**Fix:** Refactor into generic `SQLiteFTSStore[T]` with configurable field names, or extract a shared base struct that both embed.

---

### 3.2 Message Bus Handler Boilerplate — MEDIUM

Three packages implement nearly identical bus subscription handlers:

| Package | File | Lines |
|---------|------|-------|
| task | `internal/task/registry.go` | 337–369 |
| queue | `internal/queue/queue.go` | 286–317 |
| session | `internal/session/session.go` | 379–417 |

All three define: `Handler` struct, `Start(ctx)` that subscribes to topics and spawns `handleTopic` goroutines, identical shutdown logic.

**Fix:** Extract a generic `bus.SubscriptionHandler` base type that takes a topic→callback map and handles subscribe/dispatch/teardown.

---

## Part 4: Architectural Inconsistencies

### 4.1 Inconsistent Configuration Patterns

Two patterns coexist across the codebase:

1. **Struct-based Config:** `internal/worker/worker.go`, `internal/shadow/manager.go`
2. **Functional Options:** `internal/skills/registry.go`, `internal/tools/registry.go`, `internal/agent/executor.go`

**Fix:** Standardize on functional options for components with optional dependencies; keep struct-based config for pure data (e.g. parsed TOML).

---

### 4.2 Inconsistent `io.Closer` Usage

30+ types implement `Close() error` without explicitly satisfying `io.Closer`.

**Fix:** Add explicit `io.Closer` assertions (e.g. `var _ io.Closer = (*Foo)(nil)`) for types that own resources, to make lifecycle expectations grep-able.

---

### 4.3 `ToolRegistry` vs `ToolExecutor` Naming Overlap

- `internal/agent/executor.go:75` — `ToolRegistry` (query: `Get`, `List`, `GetDefinitions`)
- `internal/tools/interface.go:62` — `ToolExecutor` (action: `Execute(name, args)`)

These serve different purposes but the names are confusable.

**Fix:** Rename `ToolExecutor` → `ToolInvoker` (or merge with `ToolRegistry` after §2.1 is resolved). Low priority but improves grep-ability.

---

## Part 5: Cleanup

### 5.1 Deprecated `NewStore` Constructor

**File:** `internal/session/session.go:39`

```go
// Deprecated: Use NewSQLiteStore for persistent sessions.
func NewStore(...) *MemoryStore
```

Decide: remove the deprecated constructor, or set an explicit removal date.

---

### 5.2 Outdated `NOTE:` Comments in Agent Specs

**File:** `internal/agent/spec.go`

| Line | Comment | Action |
|------|---------|--------|
| 159 | `NOTE: exec_tool does not exist yet` | Verify and remove if accurate, implement if needed |
| 177 | `NOTE: exec_tool and run_tests do not exist yet` | Same |
| 192 | `NOTE: decompose_task and create_subtasks tools do not exist yet` | Same |
| 214 | `NOTE: summarize tool does not exist yet` | Same |
| 230 | `NOTE: git_* tools do not exist yet` | Same — `git_*` builtins likely exist, audit and remove |

These NOTEs accumulate as stale debt; either implement the tools or strike the comment.

---

### 5.3 `PlaceholderToolRegistry`

**File:** `internal/agent/executor.go:74` and `:487-520`

```go
// This is a placeholder interface that will be implemented in Phase 8.
// ...
// PlaceholderToolRegistry is a simple implementation for testing.
```

The "Phase 8" comment is obsolete — the real registry exists at `internal/tools/registry.go` and is used in production. Decide:
- If still used by tests only: rename to `FakeToolRegistry` and move into a `_test.go` file.
- If unused: delete.
- Either way, remove the "Phase 8" reference.

---

## Part 6: Implementation Plan

### Phase A: Interface Consolidation (High Priority)
1. Unify `Tool` interface — canonicalize on `tools.Tool`, delete duplicate from `executor.go`.
2. Remove or relocate `PlaceholderToolRegistry`.
3. Rename `ToolExecutor` for clarity.

### Phase B: Code Consolidation (Medium Priority)
1. Extract generic SQLite+FTS store; refactor `EpisodicMemory` and `TaskMemory` onto it.
2. Extract generic bus subscription handler; refactor task/queue/session handlers.

### Phase C: Cleanup (Low Priority)
1. Remove deprecated `session.NewStore`.
2. Audit and resolve all `NOTE:` comments in `spec.go`.
3. Resolve scheduler-agent prompt TODO.
4. Add explicit `io.Closer` assertions.
5. Standardize on functional-options config pattern.

---

## Critical Files Reference (Remaining)

| File | Issue | Priority |
|------|-------|----------|
| `internal/agent/executor.go` | Duplicate `Tool` interface, `PlaceholderToolRegistry`, "Phase 8" comment | High |
| `internal/tools/interface.go` | Canonical `Tool`/`ToolExecutor` (rename target) | High |
| `internal/memory/episodic.go` + `task.go` | ~80% duplication | Medium |
| `internal/task/registry.go`, `internal/queue/queue.go`, `internal/session/session.go` | Duplicate bus handler boilerplate | Medium |
| `internal/agent/spec.go` | 5 stale NOTE comments | Low |
| `internal/agent/prompts/specialists.go` | Scheduler-agent tool TODO | Low |
| `internal/session/session.go` | Deprecated `NewStore` | Low |

---

## Appendix: Resolved Since Original Audit

The following items from the original February 2025 audit have been completed and removed from the active list:

### Completed Features
- ✅ **Web API endpoints** — `handleMemorySearch`, `handleSkillsList`, `handleJobsList` in `internal/comm/web/server.go` are fully wired (memory searcher, skills lister, jobs lister injected).
- ✅ **Status handler token tracking** — `internal/daemon/components.go` now reports `tokens_used` from `budgetStatus.HourlyUsed` (lines 1701, 1890); fallback-to-zero only when budget is nil.
- ✅ **TUI sidebar memory data** — `internal/tui/sidebar.go:420` fetches memories via RPC.
- ✅ **Task `FilterMine` filter** — `internal/tui/models/tasks.go:417` now compares `t.AssignedAgent` against `m.currentAgentID`; `SetCurrentAgent`/`SetCurrentSession` accessors added.

### Resolved Duplications
- ✅ **`Skill` type collision** — `internal/clawskills/models.go` renamed to `RemoteSkill`; only `internal/skills.Skill` keeps the bare name.
- ✅ **Path expansion utilities** — consolidated into `internal/pathutil/expand.go`.
- ✅ **`LLMChatter` duplicate** — `internal/shadow/middleware.go` now defines `ChatterWithConfig` (a distinct interface that wraps configuration), not a duplicate of `llm.Chatter`.

### Cleanup
- ✅ **`internal/agent/loop.go.bak`** — removed.

### Partial Progress
- ⚠️ **`Tool` interface duplication** — the `agent.Tool` definition now includes `Parameters()` so signatures match `tools.Tool`, but the duplicate type itself remains. Still listed in §2.1.

### Resolved on 2026-04-06
- ✅ **Stale `NOTE:` comments in `spec.go`** — all 5 audited and removed (exec_tool, run_tests, decompose_task, create_subtasks, summarize, git_*). Specs now describe the current reality (shell_execute is the execution surface) without ghost-tool references.
- ✅ **Deprecated `session.NewStore`** — removed; `session.NewMemoryStore` is now the only constructor for `MemoryStore` and absorbs the initializer body directly.
- ✅ **"Phase 8" placeholder comment** on `agent.ToolRegistry` — replaced with an accurate doc comment explaining why the narrower interface exists (test isolation from `internal/tools/registry.go`).

### Still Deferred (each is effectively its own plan)
These cross-cutting refactors carry real blast radius and are intentionally **not** tackled here; they should each be promoted to their own plan document before execution:
1. **Tool interface unification** (§2.1) — collapse `agent.Tool` onto `tools.Tool`. Touches the full executor pipeline, every builtin tool adapter, and the `PlaceholderToolRegistry` test seam.
2. **Generic `SQLiteFTSStore[T]`** (§3.1) — `EpisodicMemory` + `TaskMemory` merge. Needs careful handling of the differing `category` vs `domain` columns, FTS triggers, and `FindDuplicates` semantics on `TaskMemory`.
3. **Generic `bus.SubscriptionHandler`** (§3.2) — task/queue/session subscription plumbing extraction.
4. **`ToolRegistry` vs `ToolExecutor` rename** (§4.3) — mechanical but touches many call sites; do it after §2.1.
5. **`io.Closer` audit + functional-options standardization** (§4.1, §4.2) — sweep-style cleanups; low value unless paired with actual API changes.
6. **Scheduler agent tools** (§1.1) — either implement `schedule`/`list_jobs`/`cancel_job` as first-class tools, or remove the prompt references; the agent currently succeeds via `task_create` so the impact is cosmetic.
