# Fix Plan: project_info Permission Denied + Tool Closure + Status Bar

## Problems

4 root causes producing 3 user-visible symptoms:

1. `project_info` blocked by security — not in ToolActionMap or BuiltinRules
2. `project_info` tool closure hardcoded to os.Getwd() — shared across all sessions
3. Status bar shows [local:name] even for git repos when reading from session
4. System prompt project info works but tool doesn't — mismatched data sources

---

## FIX 1: Add project_info to security allowlist (BLOCKER — causes "permission denied")

**File:** `internal/agent/executor.go` (2 sites)

**1a. Add to ToolActionMap** (~line 268, near the platform_read entries):
```go
ToolPlatformStatus: "platform_read",
ToolPlatformAgents: "platform_read",
ToolPlatformTools:  "platform_read",
"project_info":     "platform_read",  // ADD THIS
```

**1b. Add to allowedSafeTools** (~line 946, the fail-closed allowlist):
```go
allowedSafeTools := map[string]bool{
    ToolPlatformStatus:   true,
    ToolPlatformAgents:   true,
    ToolPlatformTools:    true,
    "project_info":       true,  // ADD THIS
    ToolMemorySearch:     true,
    ToolMemoryGetContext: true,
}
```

This maps project_info to the existing "platform_read" action category which is already permitted. No new BuiltinRules entry needed — "platform_read" already exists and is allowed.

## FIX 2: Make project_info tool return session-scoped data

**Problem:** The tool closure at `internal/daemon/components.go:4562` calls `os.Getwd()`. This closure is captured once at daemon startup and shared across ALL loops via `ConfigSnapshot()` → `WithToolRegistry(l.registry)`.

**Approach:** Make ProjectInfoTool resolve working directory from the AgentLoop at call time instead of from a static closure.

**File:** `internal/tools/builtin/project_info.go`
- Change `getInfo` from `func() map[string]any` to accept a working-directory resolver
- OR: make ProjectInfoTool implement a `SetWorkingDirFunc(func() string)` setter that the loop calls when building its system prompt / executing tools

**File:** `internal/agent/loop.go`
- After creating a session-scoped loop (or on each prompt build), call `projectInfoTool.SetWorkingDirFunc(func() { return l.GetWorkingDir() })`
- This way the tool resolves from the loop's actual workingDir, not os.Getwd()

**Simpler alternative:** Since the system prompt's `resolveProjectInfo` already works correctly (it uses `l.GetWorkingDir()`), and the LLM can see the `# Current Project` section in the prompt, the project_info TOOL is somewhat redundant for the "what project is this?" question. But it's still needed for the LLM to call proactively. The cleanest fix:

- Change the closure in components.go to NOT use os.Getwd() but instead resolve from context
- The tool's Execute(ctx, args) receives a context.Context — check if the loop injects workingDir into ctx
- If no ctx-based approach exists, use the SetWorkingDirFunc pattern

**Recommended:** Add `SetWorkingDirFunc` to ProjectInfoTool. Wire it in `sessionLoop()` after GetOrCreateWired.

## FIX 3: Status bar — distinguish git vs local, show branch

**File:** `ui/flutter_ui/lib/widgets/status_bar.dart` `_projectPart()`

**Problem:** When reading from `session.projectPath`, the code always returns `[local:basename]` regardless of whether the project is a git repo. It doesn't have branch/dirty info.

**Fix:** Two options:
- **A (simple):** When session.projectPath is available, show `[basename]` (no `local:` prefix). This is cleaner and doesn't claim it's local when it might be git.
- **B (correct):** Fetch project status from the daemon for the session's projectId (like currentProjectProvider does, but keyed by session projectId instead of globally-active). Use a FutureProvider that takes projectId.

**Recommended:** Option B — add a `projectStatusProvider` (FutureProvider.family keyed by projectId) that fetches `/api/v1/projects/{id}/status`. The status bar reads from this when session.projectId is available. Falls back to session.projectPath basename display when no projectId, then to currentProjectProvider as last resort.

## FIX 4: Ensure session-scoped loop gets correct project info

**File:** `internal/agent/handler.go` `sessionLoop()` (~line 1557)

**Problem:** When `sess.ProjectPath == ""`, sessionLoop falls back to the singleton loop whose workingDir is daemon-CWD. This means the system prompt's `# Current Project` section shows the daemon's directory.

**Fix:** This is partially addressed by the daemon CWD fallback in session_service.go CreateSession (which should now set ProjectPath to the real repo). But for sessions created before that fix, ProjectPath may still be empty.

**Mitigation:** In sessionLoop(), when ProjectPath is empty but ProjectID is set, look up the project's LocalPath from the project manager and use that as workingDir. This catches legacy sessions.

---

## Implementation Order

1. **FIX 1** (security allowlist) — 2 one-line additions, unblocks the tool immediately
2. **FIX 2** (tool closure) — SetWorkingDirFunc pattern, ~30 lines across 2 files
3. **FIX 3** (status bar) — projectStatusProvider + status_bar update, ~50 lines
4. **FIX 4** (legacy session fallback) — handler.go sessionLoop enhancement, ~10 lines

## Testing

- FIX 1: Ask "what project is this?" — project_info should return data, not "permission denied"
- FIX 2: Switch to rebellion session, call project_info — should return rebellion path/branch, not meept
- FIX 3: Check status bar for both projects — meept should show git info, rebellion should show git info
- FIX 4: Old sessions (pre-fix) should still resolve correctly
