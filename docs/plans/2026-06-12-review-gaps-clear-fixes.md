# Review Gap Fixes — Clear Fix Paths

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 9 verified gaps with mechanical fix paths discovered during the full codebase review (2026-06-12). All tasks have clear start/end states with no design ambiguity.

**Context:** These issues were identified by 7 parallel review agents covering daemon, agent, LLM, security, memory, metrics, comm/transport, CLI, and Flutter GUI. Critical and high issues were already fixed in the same session; this plan covers remaining items with clear fixes.

---

## Task 1: Fix Registry.StartAll Deadlock Risk

**Severity:** HIGH
**Files:**
- Modify: `internal/registry/registry.go`

**Problem:** `StartAll` holds `RLock` while calling `c.Start(ctx)` on each component. If any component's `Start()` calls back into the registry (e.g., to register a sub-component), it deadlocks trying to acquire a write lock. The same bug existed in `StopAll` and was fixed (CORE-7).

**Fix:** Apply the same snapshot-and-unlock pattern used in `StopAll`:

1. Acquire `RLock`
2. Snapshot the ordered component list and their starters to a local slice
3. Release `RLock`
4. Iterate the snapshot calling `Start()` without holding any lock
5. If `Start()` returns an error, log it and continue (or collect errors and return aggregate)

**Step 1:** Read the `StopAll` implementation to confirm the exact pattern used.
**Step 2:** Refactor `StartAll` to use the same pattern.
**Step 3:** Verify no existing tests break.

---

## Task 2: Wire DispatchViz Event Listeners

**Severity:** HIGH
**Files:**
- Modify: `internal/tui/sidebar.go`

**Problem:** `DispatchViz.RegisterEventListeners` subscribes to agent events (`AgentStart`, `AgentEnd`, `TurnStart`, `TurnEnd`, `ToolExecutionStart/End`) for animated robot state changes in the TUI sidebar. The method exists but is never called. The sidebar only updates via periodic `SyncWithData` calls.

**Fix:**

1. In `sidebar.go`, after creating the `DispatchViz`, call `viz.RegisterEventListeners(emitter)` passing the agent event emitter
2. The TUI needs access to the agent emitter — check if it's already available through the `AgentLoop` or if it needs to be threaded through the TUI construction
3. If the emitter isn't accessible, add a getter to `AgentLoop` (e.g., `EventEmitter() *agent.EventEmitter`)

**Step 1:** Read `sidebar.go` to find where `DispatchViz` is created and what context is available.
**Step 2:** Trace how the TUI gets access to daemon components (check `internal/tui/` for init paths).
**Step 3:** Add wiring call. If emitter isn't accessible, add getter and thread it through.
**Step 4:** Verify TUI builds and test manually.

---

## Task 3: Wire Per-Provider Timeout to HTTP Client

**Severity:** MEDIUM
**Files:**
- Modify: `internal/llm/client.go`
- Modify: `internal/llm/providers.go` (if needed)

**Problem:** `ProviderOptionsConfig.Timeout` is parsed from `models.json5` but never applied. `NewClient` always uses `defaultTimeout = 120s`. The `WithTimeout` option exists but is never called during provider resolution.

**Fix:**

1. In `provider_manager.go` (or wherever `createChatterFor` creates clients), check if `cfg.Timeout > 0` and pass `WithTimeout(cfg.Timeout)` to `NewClient`
2. Apply for both OpenAI-compatible and Anthropic client paths

**Step 1:** Read `createChatterFor` and `NewClient` to confirm the wiring point.
**Step 2:** Add `WithTimeout` call when `cfg.Timeout > 0`.
**Step 3:** Add a test or verify the timeout is respected.

---

## Task 4: Implement DaemonService.Start()

**Severity:** MEDIUM
**Files:**
- Modify: `internal/daemon/service.go`
- Possibly modify: `internal/daemon/launchd.go`

**Problem:** `DaemonService.Start()` (line 161) is a no-op stub — it just sets `isRunning = true`. When called via `meept-daemon service start`, it reports success without starting the daemon. Meanwhile `ServiceManager` in `launchd.go` has actual `Load()`/`Unload()` implementations.

**Fix:** Two options (pick based on investigation):
- **Option A:** Delegate `DaemonService.Start()` to launchd `ServiceManager.Load()` + `Start()`
- **Option B:** Implement via `kardianos/service` properly using the `Run()` method

Investigate which approach matches the existing CLI wiring at `cmd/meept-daemon/main.go` lines 178-188.

**Step 1:** Read both implementations and the CLI wiring to understand the current flow.
**Step 2:** Implement the chosen approach.
**Step 3:** Test `meept-daemon service start` manually or via existing test.

---

## Task 5: Add Request Body Size Limits to `web` Package

**Severity:** MEDIUM (security — DoS vector)
**Files:**
- Modify: `internal/comm/web/server.go`
- Modify: `internal/comm/web/memory.go`
- Modify: `internal/comm/web/skills.go`
- Modify: `internal/comm/web/jobs.go`
- Modify: `internal/comm/web/agents.go`
- Modify: `internal/comm/web/sessions.go`
- Modify: `internal/comm/web/streaming.go`

**Problem:** Every handler in `internal/comm/web/` reads `r.Body` via `json.NewDecoder(r.Body).Decode()` with no size limit. The unified HTTP server uses `MaxBytesReader` with 1MB limit. The legacy `web` package is missing this protection.

**Fix:**

1. Create a helper `readJSON(w, r, v, maxBytes)` in `internal/comm/web/` that wraps `r.Body` in `http.MaxBytesReader` before decoding
2. Replace all `json.NewDecoder(r.Body).Decode(&req)` calls with the helper
3. Use 1MB limit consistent with the unified server

**Step 1:** Create the helper function.
**Step 2:** Replace all decode calls across the 7 handler files.
**Step 3:** Verify existing tests still pass.

---

## Task 6: Fix EventEmitter.Subscribe Buffer Blocking

**Severity:** LOW
**Files:**
- Modify: `internal/comm/http/events.go`

**Problem:** `Subscribe()` sends buffered events to a new subscriber's channel (capacity 100) while holding the lock. If the buffer has 100+ events and the subscriber hasn't started reading, `ch <- event` blocks, deadlocking the `Publish` path.

**Fix:**

1. In `Subscribe()`, copy the buffer slice under the lock, then release the lock
2. Send buffered events to the channel outside the lock using non-blocking sends
3. Log a warning if events are dropped during replay

**Step 1:** Read `Subscribe()` and `Publish()` implementations.
**Step 2:** Refactor `Subscribe()` to separate buffer copy from channel send.
**Step 3:** Verify with existing event tests.

---

## Task 7: Fix Integer Query Parameter Validation

**Severity:** LOW
**Files:**
- Modify: `internal/comm/http/api_handlers.go`

**Problem:** Integer query params (limit, offset, etc.) are parsed twice and accept negative/overflow values. Found in ~8 handlers.

**Fix:**

1. Create a helper `parseIntParam(r, key, default, min, max) (int, error)`
2. Replace all inline parsing with the helper
3. Use reasonable defaults: limit [1, 100], offset [0, 100000]

**Step 1:** Create the helper function.
**Step 2:** Replace all inline parsing calls.
**Step 3:** Verify existing handler tests pass.

---

## Task 8: Remove Dead tts_settings.dart

**Severity:** LOW
**Files:**
- Delete: `ui/flutter_ui/lib/features/settings/tts_settings.dart`

**Problem:** This file imports a non-existent `cyberpunk_theme` package, uses hardcoded font families instead of the project's typography system, and is never imported by any other file. It will not compile.

**Fix:**

1. Delete `ui/flutter_ui/lib/features/settings/tts_settings.dart`
2. If TTS settings UI is desired in the future, it should be built fresh in `settings_panel.dart` using the project's actual theme and typography constants

**Step 1:** Confirm no file imports `tts_settings.dart`.
**Step 2:** Delete the file.

---

## Task 9: Add .bak Files to .gitignore

**Severity:** LOW
**Files:**
- Modify: `.gitignore`

**Problem:** `components.go.bak` (126KB) was committed to the repo. Prevent future occurrences.

**Fix:**

1. Add `*.bak` to `.gitignore`
2. Verify no other `.bak` files are tracked: `git ls-files '*.bak'`

**Step 1:** Add `*.bak` to `.gitignore`.
**Step 2:** Verify with `git ls-files '*.bak'`.
