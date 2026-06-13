# Dead Code Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove all dead code, unused types, and redundant functions identified during the systematic codebase review.

**Architecture:** Straightforward deletion of code confirmed to have no callers and/or a superior replacement already in use. Each task deletes one item, verifies the build, and commits.

**Tech Stack:** Go 1.22+, Dart/Flutter

---

## Investigation Results Summary

All 10 items investigated. Verdicts:
- **8 items: DELETE** (replacement exists or no consumer)
- **3 items: DELETE** (metrics collector methods — no near-term wiring planned)
- **0 items: WIRE** (nothing needs wiring that isn't already served)

---

### Task 1: Delete `Consolidator.PruneOld()` method

**Files:**
- Modify: `internal/memory/consolidation.go:549-572`
- Test: `internal/memory/consolidation_test.go` (remove any `PruneOld` tests)

**Rationale:** `Run()` already calls `runAccessBasedExpiration()` which provides superior access-based pruning with optional summarization. `PruneOld` is a simpler age-only delete that nobody calls.

- [ ] **Step 1: Find and remove `PruneOld` method**

In `internal/memory/consolidation.go`, delete the `PruneOld` method (approximately lines 549-572).

- [ ] **Step 2: Remove any `PruneOld` test coverage**

Search `internal/memory/consolidation_test.go` for any test functions referencing `PruneOld` and remove them.

- [ ] **Step 3: Verify build**

Run: `go build ./internal/memory/...`
Expected: success

- [ ] **Step 4: Verify no callers**

Run: `grep -r "PruneOld" --include="*.go" .`
Expected: no results (only docs/generated references which auto-update)

- [ ] **Step 5: Commit**

```bash
git add internal/memory/consolidation.go internal/memory/consolidation_test.go
git commit -m "refactor(memory): remove dead PruneOld method (subsumed by runAccessBasedExpiration)"
```

---

### Task 2: Delete `PeriodicCollector` type and constructor

**Files:**
- Modify: `internal/metrics/collector.go:546-589`

**Rationale:** `Collector` already has its own `startCollection()` goroutine with ticker. `PeriodicCollector` was an unused generic wrapper that was never instantiated.

- [ ] **Step 1: Delete `PeriodicCollector` type, `NewPeriodicCollector`, and `CollectFunc` typedef**

In `internal/metrics/collector.go`, delete approximately lines 546-589 (the `CollectFunc` type alias, `PeriodicCollector` struct, and `NewPeriodicCollector` function).

- [ ] **Step 2: Verify build**

Run: `go build ./internal/metrics/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/metrics/collector.go
git commit -m "refactor(metrics): remove unused PeriodicCollector type"
```

---

### Task 3: Delete four unused `Collector` methods

**Files:**
- Modify: `internal/metrics/collector.go:314-347`

**Rationale:** These metrics API methods were designed for instrumentation that was never wired:
- `RecordQueueDepth` — redundant with `collect()` callback
- `RecordJobDuration` — no caller (QueueService doesn't emit metrics)
- `RecordMemoryOperation` — no caller (memory operations not instrumented)
- `RecordModelResolution` — no caller (LLM resolver doesn't emit metrics)

If instrumentation is needed later, these can be re-added at that time.

- [ ] **Step 1: Delete all four methods**

In `internal/metrics/collector.go`, delete methods `RecordQueueDepth`, `RecordJobDuration`, `RecordMemoryOperation`, and `RecordModelResolution` (approximately lines 314-347).

- [ ] **Step 2: Verify build**

Run: `go build ./internal/metrics/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/metrics/collector.go
git commit -m "refactor(metrics): remove unused collector methods (RecordQueueDepth, RecordJobDuration, RecordMemoryOperation, RecordModelResolution)"
```

---

### Task 4: Delete `internal/config/agents_json5.go` (entire file)

**Files:**
- Delete: `internal/config/agents_json5.go`

**Rationale:** `agents.go` already handles JSON5 natively — `AgentDefinition` has dual `json`/`toml` tags, and `LoadAgentDefinitionsDefault()` already tries JSON5 first then TOML. The `AgentDefinitionJSON5` type and `LoadAgentDefinitionsDefaultWithJSON5` function in `agents_json5.go` are completely redundant and never called.

- [ ] **Step 1: Verify no callers**

Run: `grep -r "AgentDefinitionJSON5\|LoadAgentDefinitionsDefaultWithJSON5\|AgentsFileJSON5" --include="*.go" .`
Expected: only references within `agents_json5.go` itself

- [ ] **Step 2: Delete the file**

Run: `rm internal/config/agents_json5.go`

- [ ] **Step 3: Verify build**

Run: `go build ./internal/config/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add -u internal/config/agents_json5.go
git commit -m "refactor(config): remove redundant agents_json5.go (agents.go handles JSON5 natively)"
```

---

### Task 5: Delete `StripBoundaryMarkers` function and test

**Files:**
- Modify: `internal/security/prompt_guard.go:247-257`
- Modify: `internal/security/prompt_guard_test.go` (remove `TestStripBoundaryMarkers`)

**Rationale:** No production code calls this. Boundary markers are added by `WrapUserInput`/`WrapToolOutput` and extracted by `ExtractUserInput`/`ExtractToolOutput`. Nobody needs to strip them wholesale. Trivially reconstructable if needed.

- [ ] **Step 1: Delete `StripBoundaryMarkers` from `prompt_guard.go`**

Delete the function at approximately lines 247-257.

- [ ] **Step 2: Delete `TestStripBoundaryMarkers` from `prompt_guard_test.go`**

Search for and remove the test function.

- [ ] **Step 3: Verify build and tests**

Run: `go test ./internal/security/... -v -run TestStripBoundaryMarkers`
Expected: no test found (test deleted)

Run: `go build ./internal/security/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/security/prompt_guard.go internal/security/prompt_guard_test.go
git commit -m "refactor(security): remove unused StripBoundaryMarkers function"
```

---

### Task 6: Delete `AsyncState` Flutter files

**Files:**
- Delete: `ui/flutter_ui/lib/providers/async_state.dart`
- Delete: `ui/flutter_ui/lib/providers/async_state.freezed.dart`
- Delete: `ui/flutter_ui/lib/providers/async_state.g.dart` (if exists)

**Rationale:** `AsyncState<T>` is a Freezed union that no provider uses. The app uses ad-hoc state classes per provider (e.g., `AgentState` with `isLoading`/`error`/`agents` fields).

- [ ] **Step 1: Verify no imports**

Run: `grep -r "async_state" --include="*.dart" ui/flutter_ui/lib/`
Expected: only the `async_state.dart` file itself and its generated companion

- [ ] **Step 2: Delete the files**

```bash
rm ui/flutter_ui/lib/providers/async_state.dart
rm ui/flutter_ui/lib/providers/async_state.freezed.dart 2>/dev/null
rm ui/flutter_ui/lib/providers/async_state.g.dart 2>/dev/null
```

- [ ] **Step 3: Verify Flutter build**

Run: `cd ui/flutter_ui && flutter analyze lib/providers/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add -u ui/flutter_ui/lib/providers/async_state.dart
git commit -m "refactor(flutter): remove unused AsyncState freezed union"
```

---

### Task 7: Delete `AgentsList` Flutter widget

**Files:**
- Delete: `ui/flutter_ui/lib/features/agents/agents_list.dart`

**Rationale:** `AgentsTab` in `agents_tab.dart` is the live implementation using a GridView with cards. `AgentsList` was an earlier ListView-based version that was superseded.

- [ ] **Step 1: Verify no imports**

Run: `grep -r "agents_list" --include="*.dart" ui/flutter_ui/lib/`
Expected: no results

- [ ] **Step 2: Delete the file**

```bash
rm ui/flutter_ui/lib/features/agents/agents_list.dart
```

- [ ] **Step 3: Verify Flutter build**

Run: `cd ui/flutter_ui && flutter analyze lib/features/agents/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add -u ui/flutter_ui/lib/features/agents/agents_list.dart
git commit -m "refactor(flutter): remove unused AgentsList widget (superseded by AgentsTab)"
```

---

### Task 8: Delete `LaunchAgentController` from launchd.go

**Files:**
- Modify: `internal/daemon/launchd.go:473-495`

**Rationale:** `DaemonControl` (line 510) already implements `DaemonController` and wraps `ServiceManager` for external consumers. `LaunchAgentController` is an unused backward-compat wrapper.

- [ ] **Step 1: Delete `LaunchAgentController` type, `NewLaunchAgentController`, and the compat comment block**

Delete approximately lines 473-495 in `launchd.go`.

- [ ] **Step 2: Verify build**

Run: `go build ./internal/daemon/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/launchd.go
git commit -m "refactor(daemon): remove unused LaunchAgentController (DaemonControl replaces it)"
```

---

### Task 9: Delete `Backend` interface from memory types

**Files:**
- Modify: `internal/memory/types.go:142-160`

**Rationale:** No type implements this interface. `EpisodicMemory` and `TaskMemory` embed `*SQLiteFTSStore` directly. The working abstraction is `ConsolidationBackend` which has two implementations.

- [ ] **Step 1: Delete the `Backend` interface**

Delete approximately lines 142-160 in `types.go`.

- [ ] **Step 2: Verify build**

Run: `go build ./internal/memory/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/memory/types.go
git commit -m "refactor(memory): remove unimplemented Backend interface (ConsolidationBackend is the actual abstraction)"
```

---

### Task 10: Delete unused `PoolSize` config fields

**Files:**
- Modify: `internal/memory/episodic.go:70-71`
- Modify: `internal/memory/task.go:77-78`

**Rationale:** `SQLiteFTSStore` hardcodes `SetMaxOpenConns(1)` — the pool size config is never read. No caller passes `PoolSize` in production code.

- [ ] **Step 1: Remove `PoolSize` from `EpisodicConfig`**

In `internal/memory/episodic.go`, delete the `PoolSize int` field from `EpisodicConfig` struct.

- [ ] **Step 2: Remove `PoolSize` from `TaskMemoryConfig` and `DefaultTaskMemoryConfig`**

In `internal/memory/task.go`, delete the `PoolSize int` field from `TaskMemoryConfig` and the `PoolSize: 5` line from `DefaultTaskMemoryConfig()`.

- [ ] **Step 3: Verify build**

Run: `go build ./internal/memory/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/memory/episodic.go internal/memory/task.go
git commit -m "refactor(memory): remove unused PoolSize config fields (SQLiteFTSStore uses single connection)"
```

---

### Task 11: Regenerate docs and final verification

**Files:**
- Generated: `docs/reference/generated/` (auto-updated)

- [ ] **Step 1: Run full build**

Run: `go build ./...`
Expected: success

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -timeout 120s`
Expected: all pass

- [ ] **Step 3: Regenerate docs (if make target exists)**

Run: `make docs-generate 2>/dev/null || echo "no docs-generate target"`

- [ ] **Step 4: Commit docs regeneration if changes exist**

```bash
git add docs/reference/generated/
git commit -m "docs: regenerate reference docs after dead code cleanup"
```
