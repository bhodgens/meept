# Implementation Plan: Project Awareness + /project and /session Fixes

## Problem Summary

Seven issues across three areas:

**A. Agent project blindness** (3 issues)
1. "What project is this?" returns raw JSON from memory_search
2. Same question returns raw JSON from platform_status
3. Agent has no project metadata in system prompt and no tool to query it

**B. /project command deficiencies** (4 issues)
4. /project set resets ALL sessions' project binding (global active project)
5. /project set doesn't refresh the Flutter sidebar tree
6. Slash commands aren't recorded in session input history
7. /session (and /project) don't expand `~` to $HOME

---

## Part A: Agent Project Awareness

### A1. Inject project metadata into system prompt

**Root cause:** `buildSystemPromptWithContextAndSkills` (loop.go:4094-4198) never tells the agent what project it's operating in. It loads AGENTS.md and CLAUDE.md content but never states the project name, path, or git branch.

**Fix:** Add a "Current Project" section to the system prompt builder.

**Files:**
- `internal/agent/loop.go` (~line 4163): After loading artifact context, query the session's project info and inject a section:
  ```
  # Current Project
  Name: meept
  Path: /Users/caimlas/git/meept
  Branch: main (dirty)
  Language: Go (go.mod detected)
  ```
- `internal/agent/prompt.go`: Add `WithProjectInfo(name, path, branch string, dirty bool)` method to PromptBuilder

**Implementation:**
1. Add `projectInfo string` field to `PromptBuilder`
2. Add `WithProjectInfo()` method that formats the section
3. In `Build()`, emit it as the first dynamic section (before memory context)
4. In `buildSystemPromptWithContextAndSkills()`, resolve the project from the loop's workingDir:
   - If `l.projectID` is set, query the project manager for name/status
   - If only `workingDir` is set, derive project name from path basename and detect git branch via `git -C <dir> branch --show-current`
   - Skip the section entirely if workingDir is empty

### A2. Add project_info tool

**Root cause:** No tool exists for the agent to query its own project metadata when asked directly.

**Fix:** Add a `project_info` tool that returns the current project's name, path, branch, dirty status, and language.

**Files:**
- `internal/tools/builtin/project_info.go` (new): `ProjectInfoTool` struct
  - `Name()` returns `"project_info"`
  - `Execute()` returns `map[string]any{"name", "path", "branch", "dirty", "language"}`
  - Gets data from a `func() ProjectSnapshot` injected at wiring time
- `internal/daemon/components.go` (~line 4531): Wire the tool alongside `PlatformStatusTool`
  - The status func resolves project info from the loop's session project binding
- Register in the baseline tools list so the system prompt mentions it

**Tool description:** `"Get information about the current project: name, directory path, git branch, and detected language. Use this when asked about the current project."`

### A3. Prevent raw JSON from leaking to user

**Root cause:** When tools return structured JSON and the LLM can't interpret it (because it lacks context), it dumps the raw JSON as its reply. The memory_search tool returns `{"results":[],"count":0,"query":"..."}` and platform_status returns `{"status":"running",...}`.

**Fix:** This is partially fixed by A1 + A2 (the agent now knows the project and has a proper tool). Additionally, add a formatting guideline to the system prompt.

**Files:**
- `internal/agent/prompts/baseline.go` (~line 18, BaselineGuidelines): Add a guideline:
  ```
  - Never return raw tool output (JSON, structured data) directly to the user.
    Always format tool results into natural language. If a tool returns empty
    results, explain what that means in plain language.
  ```

---

## Part B: /project and /session Command Fixes

### B1. /project set should only affect the current session, not all sessions

**Root cause:** `handleSet` (rpc/projects.go:206) calls `pm.DeactivateActive(ctx)` which globally deactivates ALL projects, then activates the new one as the single "active" project. The TUI's `fetchCurrentProject()` (app.go:629-649) finds the "first active project" globally, so switching in one session changes the displayed project for all sessions.

**Fix:** Remove the global activation from the session-binding path. Project activation should be a separate concept from session-project binding.

**Files:**
- `internal/rpc/projects.go` `handleSet()` (~line 206): Remove `pm.DeactivateActive(ctx)` call. Keep only the session binding (`h.sessionStore.SetProject()`). The project doesn't need to be "active" globally — it just needs to be bound to the session.
- `internal/tui/app.go` `fetchCurrentProject()` (~line 617): Change to fetch the **current session's** project, not the globally-active project:
  - Look up `a.currentSession.ProjectID` / `a.currentSession.ProjectPath`
  - If set, query project status for that specific project
  - If not set, show "no project bound"
- `internal/tui/app.go` `SetProjectResultMsg` handler (~line 1688): After successful SetProject RPC, call `fetchCurrentProject` (already done) but ensure it reads from the session, not the global active state.

**Note:** The global active-project concept may still be needed for `EnsureDefault` during session creation. The fix is to decouple "active project" (used for new session defaults) from "session-bound project" (used for display and execution context).

### B2. /project set should refresh the Flutter sidebar tree

**Root cause:** After `project.set` RPC succeeds, the Flutter sidebar doesn't re-fetch sessions (which now have updated `projectId`/`projectPath`). The sidebar groups sessions by project, so the moved session stays in its old group visually.

**Fix:** After SetProject succeeds in the Flutter chat provider, trigger a session list refresh.

**Files:**
- `ui/flutter_ui/lib/providers/chat_provider.dart`: After `setProject()` call succeeds, the provider or UI layer should call `ref.read(sessionProvider.notifier).loadSessions()` to re-fetch sessions with updated project bindings.
- `ui/flutter_ui/lib/features/home/sidebar_home_screen.dart` (~line 178): The `_refreshData()` method already calls `loadSessions()` — ensure it's triggered after project switch. The `/project` slash command handler in Flutter needs to invoke the session refresh.

**Flutter slash command handler:**
- The `/project` command in Flutter is handled server-side (sent as chat message). The result comes back via WebSocket. Need to detect the project-switch result and trigger sidebar refresh.
- Alternatively: add a `onProjectChanged` callback in the chat input that fires after `/project set` completes, which calls `loadSessions()` and `currentProjectProvider.refresh()`.

### B3. Slash commands should be recorded in session input history

**Root cause:** In the TUI (app.go:958,973,1842), slash commands are executed via `commandHandler.Execute(cmd)` without ever calling `addToHistory()`. In Flutter, slash commands are sent as chat messages which DO go through the history path, but server-side commands (like `/project`) are processed differently.

**TUI Fix:**
- `internal/tui/app.go` (~lines 958, 973, 1842): Before executing a slash command, call `a.chat.addToHistory(input)` (the raw `/project set foo` text). This makes slash commands recallable with Up arrow.
- Need to expose `addToHistory` or add a method on ChatModel like `RecordInputToHistory(text string)`.

**Flutter Fix:**
- `ui/flutter_ui/lib/features/chat/chat_input.dart`: Slash commands are already added to `SessionHistoryStore` when sent as messages. Verify `/project` and `/session` commands go through the same `SessionHistoryStore.add()` path. If they're intercepted client-side, add explicit history recording.

### B4. ~ expansion for /session and /project paths

**Root cause:** `expandTilde()` exists in `internal/rpc/projects.go:406` but is only used by `handleReadDir`. The `handleSet()` path function (rpc/projects.go:210) passes `req.Path` directly to `CreateOrResolve` without expanding tildes. There's no `/session` command in the TUI, but Flutter has one.

**TUI/daemon Fix:**
- `internal/rpc/projects.go` `handleSet()` (~line 210): Apply `expandTilde(req.Path)` before passing to `CreateOrResolve`. Move `expandTilde` to a shared utility location or call it inline.
- `internal/rpc/projects.go` `handleDetect()` (if it exists): Same treatment.
- `internal/tui/command_handler.go` `executeProjectSet()` (~line 1811): Expand `~` in the query before sending to DetectProject RPC, or rely on the daemon-side fix.

**Flutter Fix:**
- `ui/flutter_ui/lib/features/chat/chat_input.dart` or `slash_commands.dart`: When handling `/session` or `/project` with a path argument, expand `~` to the home directory before sending to the daemon.
- Add a shared `expandTilde(String path)` utility in Flutter that replaces leading `~` with the platform home directory (using `Platform.environment['HOME']`).

---

## Implementation Order

1. **A2** (project_info tool) — new file, no dependencies
2. **A1** (prompt injection) — depends on having project info available
3. **A3** (anti-JSON guideline) — one-line prompt change
4. **B1** (per-session project) — daemon RPC fix + TUI display fix
5. **B4** (tilde expansion) — daemon + client
6. **B3** (slash command history) — TUI + Flutter
7. **B2** (sidebar refresh) — Flutter only, depends on B1

## Testing

- A1/A2: Unit test the prompt builder renders project info; unit test the tool returns correct data
- B1: Test that switching project in session A doesn't change session B's displayed project
- B3: Test that slash commands appear in Up-arrow history
- B4: Test `/project set ~/git/foo` resolves correctly
- All: `go test ./internal/agent/... ./internal/tools/... ./internal/rpc/... ./internal/tui/...`

## Risk Notes

- B1 is the riskiest: decoupling "active project" from "session project" could affect `EnsureDefault` behavior. Need to verify new session creation still works.
- A1 requires careful prompt engineering to avoid bloating the system prompt. Keep the project info section compact.
- B2 requires coordination between the Flutter chat provider and the session/sidebar providers.
