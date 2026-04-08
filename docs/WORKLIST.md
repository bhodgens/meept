# Worklist: Missing, Incomplete, or Unwired Implementation

Generated: 2026-04-08 from review of all 5 plan documents.

## Executive Summary

| Category | Count | Severity |
|----------|-------|----------|
| Scheduler Agent Tools (TODO) | 1 | Low |
| Tool Interface Duplication | 1 | High |
| Memory Store Duplication | 1 | Medium |
| Bus Handler Duplication | 1 | Medium |
| Naming Inconsistencies | 1 | Low |
| Missing io.Closer Assertions | 1 | Low |
| Review Phase 3 (StateTesting) | 1 | Low |
| Review Metrics | 1 | Low |
| Revision Count in TUI | 1 | Low |
| AST/LSP Unit Tests | 1 | Medium |

**Total**: 10 outstanding items (2 high/medium, 8 low)

All other planned work from the 5 documents has been completed or is explicitly deferred as out-of-scope.

---

## 1. Scheduler Agent Tools TODO
**Source:** `plan-missing-implementation.md` §1.1
**File:** `internal/agent/prompts/specialists.go:188`
**Severity:** Low
**Effort:** Small

### Problem
The scheduler agent prompt references non-existent `schedule`, `list_jobs`, `cancel_job` tools. The TODO comment on line 188 indicates these were never implemented. The agent currently works via `task_create` + `shell_execute` for cron/systemd.

### Options
**A.** Implement dedicated scheduling tools (`internal/code/tools/schedule_*.go`)
**B.** Remove TODO and update prompt to reflect current reality (shell_execute for cron/systemd)

### Recommended
**Option B** — Remove TODO, clarify prompt. Dedicated tools are unnecessary overhead when the agent already succeeds via the existing paths.

---

## 2. Tool Interface Duplication
**Source:** `plan-missing-implementation.md` §2.1
**Files:** `internal/tools/interface.go:19`, `internal/agent/executor.go:62`
**Severity:** High
**Effort:** Medium

### Problem
Two `Tool` interfaces with identical signatures coexist. While Go's structural typing currently makes them compatible, they can drift — creating a maintenance hazard. The `agent.Tool` definition includes `Parameters()` so it matches `tools.Tool`, but it's still a separate type.

### Fix
1. Remove `Tool` interface from `internal/agent/executor.go`
2. Import and use `tools.Tool` as canonical
3. Update `ToolRegistry` interface in executor.go to use `tools.Tool`
4. Verify all adapters still compile (they should since signatures match)

---

## 3. Memory Store Duplication (~80% code overlap)
**Source:** `plan-missing-implementation.md` §3.1
**Files:** `internal/memory/episodic.go`, `internal/memory/task.go`
**Severity:** Medium
**Effort:** Large

### Problem
Both types implement ~80% identical code: SQLite + FTS5 init, schema, triggers, `Store()`/`Search()`/`GetRecent()`/`Delete()` methods.

**Differences:**
- `EpisodicMemory` uses `category` field
- `TaskMemory` uses `domain` field
- `TaskMemory` has `FindDuplicates()` method

### Fix
Extract generic `SQLiteFTSStore[T]` base struct:
- Move shared logic into the generic layer
- Configure field names (`category` vs `domain`) in constructor
- Type-specific methods (like `FindDuplicates`) stay in the wrapper

---

## 4. Message Bus Handler Duplication
**Source:** `plan-missing-implementation.md` §3.2
**Files:** `internal/task/registry.go:337-369`, `internal/queue/queue.go:286-317`, `internal/session/session.go:379-417`
**Severity:** Medium
**Effort:** Medium

### Problem
Three packages implement nearly identical bus subscription handlers:
- `Handler` struct
- `Start(ctx)` subscribes to topics and spawns goroutines
- Identical shutdown logic

### Fix
Extract generic `bus.SubscriptionHandler` base type:
- Takes a `map[string]func(context.Context, json.RawMessage)` callback map
- Handles subscribe/dispatch/teardown
- Each package embeds the generic handler and provides callbacks

---

## 5. ToolRegistry vs ToolExecutor Naming
**Source:** `plan-missing-implementation.md` §4.3
**Files:** `internal/agent/executor.go:75` (`ToolRegistry`), `internal/tools/interface.go:62` (`ToolExecutor`)
**Severity:** Low
**Effort:** Small

### Problem
Names are confusable. `ToolRegistry` queries (Get, List, GetDefinitions) while `ToolExecutor` acts (Execute).

### Fix
Rename `ToolExecutor` → `ToolInvoker` or merge with `ToolRegistry` after §2 is resolved.

---

## 6. Missing io.Closer Assertions
**Source:** `plan-missing-implementation.md` §4.2
**Severity:** Low
**Effort:** Small

### Problem
30+ types implement `Close() error` without explicitly satisfying `io.Closer`. Makes lifecycle expectations unclear.

### Fix
Add `var _ io.Closer = (*Type)(nil)` assertions for resource-owning types like:
- LSP client/manager
- ParserManager
- TaskStore/StepStore
- Memory stores
- MessageBus
- AgentLoop types

---

## 7. Review Phase 3 — StateTesting Final-Review Step
**Source:** `plan-reviewer-agents.md` (Deferred section)
**File:** `internal/task/task.go:16` (`StateTesting` constant exists but unused)
**Severity:** Low
**Effort:** Small

### Problem
`StateTesting` constant is never activated. No final-review step is auto-injected on task completion. It's speculative — nothing currently requires it, and coupling review into task-completion semantics changes behavior.

### Fix
**Option A:** Implement — create final review step in `TacticalScheduler.OnAllStepsCompleted`
**Option B:** Remove constant entirely (cleaner)
**Option C:** Leave as-is (documented as deferred)

### Recommended
**Option C** — Leave deferred. The existing step-level review is sufficient. `StateTesting` can be removed if it serves no future purpose.

---

## 8. Review Metrics Not Emitted
**Source:** `plan-reviewer-agents.md` (Deferred section)
**Severity:** Low
**Effort:** Small

### Problem
No metrics for review pass rate or average revision cycles. The bus events (`step.review_requested`, `step.review_approved`, `step.review_rejected`) provide data for external scraping, but no internal metrics aggregation.

### Fix
Add a simple metrics collector that:
- Subscribes to review bus events
- Tracks pass rate, avg revision count per reviewer type
- Exposes via `/metrics` endpoint or logs periodically

---

## 9. Revision Count Not Shown in TUI
**Source:** `plan-reviewer-agents.md` (Deferred section)
**File:** `internal/task/step.go:49` (`RevisionCount` field exists)
**Severity:** Low (cosmetic)
**Effort:** Very small

### Problem
`TaskStep.RevisionCount` is persisted in SQLite but not projected through `TaskStepView` to the Tasks dashboard detail modal.

### Fix
1. Add `RevisionCount int` to `internal/tui/types/types.go:TaskStepView`
2. Map it in the RPC `ListTaskSteps` handler (`internal/rpc/proxy.go` or `internal/task/registry.go`)
3. Show it in the detail modal in `internal/tui/models/tasks.go` (e.g., "Revisions: N")

---

## 10. AST/LSP Unit Tests Missing
**Source:** `plan-lsp-ast-tooling.md` (Deferred section)
**Severity:** Medium
**Effort:** Large

### Problem
No unit tests for `internal/code/ast/`, `internal/code/lsp/`, or `internal/code/tools/`. The code compiles and the daemon registers all tools successfully, and end-to-end agent coverage exists via the orchestrator and handler tests, but there are no targeted unit tests for:
- ParserManager (multi-language parsing)
- Symbol extraction per language
- Tree-sitter query execution
- LSP client transport (stdio/TCP)
- LSP method calls (definition, references, hover, etc.)
- Tool execute methods

### Fix
Add tests per component:
- `internal/code/ast/parser_test.go` — multi-language parsing, error handling
- `internal/code/ast/symbols_test.go` — func/class/method extraction for Go, Python, TS
- `internal/code/ast/query_test.go` — S-expression query execution
- `internal/code/lsp/client_test.go` — LSP lifecycle (mock server or use gopls)
- `internal/code/lsp/manager_test.go` — multi-server lifecycle
- `internal/code/tools/ast_parse_test.go`, `ast_symbols_test.go`, `ast_query_test.go` — tool input/output
- `internal/code/tools/lsp_*.go` — each LSP tool

---

## What's Already Done

All major planned work from the 5 documents has been completed:

### plan-hierarchial-async-agents.md
✅ Phases 1–6: StepStore, StrategicPlanner, TacticalScheduler, Orchestrator, async dispatch
✅ Phase 7: TUI sidebar progress bars, task detachment, step detail modal
✅ Phase 8: End-to-end orchestrator test

### plan-reviewer-agents.md
✅ Phases 1–2, 4–5: Review states, ReviewManager, reviewer specs, tactical integration, TUI icons, config
✅ Human escalation on max revision cycles

### plan-lsp-ast-tooling.md
✅ Phases 1–4: Full AST parsing and LSP client with 8 tools
✅ Phase 6: Docs updated in CLAUDE.md and features.md

### plan-missing-implementation.md
✅ Resolved items: Stale NOTE comments removed, deprecated NewStore removed, Phase 8 comment fixed, Skill type collision resolved, pathutil consolidated

---

## Execution Order

Recommended priority:

1. **High Priority** (affects correctness): #2 Tool Interface Duplication
2. **Medium Priority** (tech debt reduction): #3 Memory Store Duplication, #4 Bus Handler Duplication, #10 AST/LSP Unit Tests
3. **Low Priority** (cosmetic/optional): #1 Scheduler TODO, #5 Naming Rename, #6 io.Closer, #7 StateTesting, #8 Metrics, #9 Revision Count UI

Total estimated effort: 2–3 days for high + medium items.
