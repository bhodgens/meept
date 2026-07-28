# Analysis: Project Scoping Issues in Meept GUI Sessions

Date: 2026-07-27
Status: Analysis only — no fixes applied

## Executive Summary

Four user-reported symptoms trace to **six distinct root causes** across the Go daemon and Flutter GUI. The previous round of fixes addressed the TUI and daemon RPC layer but left the Flutter GUI session-creation path, the agent loop wiring, and the chat provider architecture unpatched. The issues are compounding: the project indicator shows stale data because the provider is global; the agent can't identify the project because sessions are created without ProjectPath; JSON leaks because the project_info tool is invisible to the LLM and terminating tools bypass LLM formatting; and responses cross sessions because the chat provider is a global singleton with no session guard.

---

## Issue 1: Global Project Indicator

**Symptom:** The `[project]` indicator in the bottom status bar is global — switching sessions in different sidebar project groups doesn't change it.

### Root Cause: Flutter status bar reads from a global provider, not the session

**Code path:**
- `ui/flutter_ui/lib/widgets/status_bar.dart:130` — `_projectPart()` calls `ref.watch(currentProjectProvider)`
- `ui/flutter_ui/lib/providers/project_provider.dart:50-60` — `refresh()` fetches `client.listProjects()` and picks the entry with `status == 'active'` — the daemon's single global active project
- `ui/flutter_ui/lib/providers/project_provider.dart:105-108` — declared as a plain `StateNotifierProvider` (not `.family`, no session arg)

**Session switching doesn't trigger refresh:**
- `ui/flutter_ui/lib/features/home/sidebar_home_screen.dart:404-408` — `_onSessionSelected()` only sets `activeSessionProvider`; never touches `currentProjectProvider`
- The only refresh sites for `currentProjectProvider` are: connect/reconnect, explicit `/project` slash command. None fire on session selection.

**Session model has the data but it's unused:**
- `ui/flutter_ui/lib/models/api_models.dart:482-483` — `Session` has `projectId` and `projectPath` fields
- The status bar never reads from the session; it only reads from the global provider

**Contrast with TUI:** The Go TUI fix in `internal/tui/app.go` `fetchCurrentProject` was updated to read from `a.currentSession.ProjectID`. The Flutter port never got the equivalent change.

### Fix Direction
Make `_projectPart` in `status_bar.dart` derive from `ref.watch(activeSessionProvider)`'s `projectPath`/`projectId`, OR refresh `currentProjectProvider` inside `_onSessionSelected` keyed on the session's project binding.

---

## Issue 2: Agent Cannot Determine the Project

**Symptom:** Asking "what is this project called?" returns confused responses — the agent has no idea what project it's in.

### Root Cause: Three compounding bugs

**BUG 2a (primary): EnsureDefault creates a synthetic project, not the user's actual repo**

**CORRECTION from initial analysis:** `SetProjectManager` IS wired at `internal/services/service.go:129-131`. Sessions created via POST /api/v1/sessions DO go through `SessionService.CreateSession` which calls `EnsureDefault`. The initial analysis incorrectly traced the RPC bus path (`session.Handler.handleCreate`) instead of the HTTP path (`api_handlers.go:647 → SessionService.CreateSession`).

The real problem: `EnsureDefault` (`internal/project/manager.go:394`) creates a **synthetic default project** under `~/.meept/projects/<uuid>`, NOT the user's actual repo. So sessions DO get a ProjectPath, but it points to an empty directory, not `/Users/caimlas/git/meept`. The agent loop's `workingDir` is set to this synthetic path, so `resolveProjectInfo` derives a meaningless project name.

Additionally, the Flutter client's `SessionNotifier.createSession` (`session_notifier.dart:120`) does not pass `cwd` during session creation, even though the API supports it (`sdk_client.dart:507-512`). The cleanest fix: Flutter passes the platform working directory when creating sessions.

**BUG 2b: Empty ProjectPath → singleton loop with daemon-CWD**

- `internal/agent/handler.go:1557-1564` — `sessionLoop()` returns the singleton `h.loop` when `sess.ProjectPath == ""`
- `internal/daemon/components.go:1059-1060` — the singleton loop is created with `wd, _ := os.Getwd()` (the daemon's CWD, likely `/` or a system directory)
- `internal/agent/loop.go:4166-4179` — `buildSystemPromptWithContextAndSkills` calls `resolveProjectInfo(workingDir)`. For the singleton, `workingDir` is daemon-CWD, so `filepath.Base()` yields a meaningless name

The system prompt's `# Current Project` section either shows the daemon's directory (useless) or is absent entirely.

**BUG 2c: project_info tool hardcoded to os.Getwd()**

- `internal/daemon/components.go:4562-4581` — the `projectInfoFunc` closure calls `os.Getwd()`, never receives session context
- `internal/agent/loop.go:5141` — `ConfigSnapshot()` copies the same tool registry pointer into session-scoped loops. The `project_info` closure is captured once at daemon startup and never re-bound per session.
- Even if session-scoped loops existed with correct workingDir, `project_info` would still return daemon-CWD data

### Fix Direction
1. Wire `SessionService.CreateSession` (with EnsureDefault) into the `session.create` RPC path, OR add ProjectManager access to `session.Handler.handleCreate`
2. Make the `project_info` closure resolve from the loop's workingDir, not `os.Getwd()`
3. Ensure the singleton loop gets a sensible default workingDir (user's home or last-used project)

---

## Issue 3: Race Condition — Responses Cross Sessions

**Symptom:** Send a message in session A, rapidly switch to session B → response appears in B. Switch back to A → sent message may be missing initially (self-corrects).

### Root Cause: Global ChatNotifier singleton with no session guard

**BUG 3a: chatProvider is a global singleton**

- `ui/flutter_ui/lib/providers/chat_provider.dart:720-726` — `StateNotifierProvider<ChatNotifier, ChatState>`, NOT `.family`-scoped by sessionId
- Every `ChatTab`/`ChatMessageList` watches the same notifier instance
- When session A's response arrives, it's written into the one shared `ChatState`, which session B's UI is currently rendering

**BUG 3b: addStreamMessage has NO session guard**

- `chat_provider.dart:533-619` — appends incoming WS messages to `state.messages` unconditionally
- Never checks `message['session_id'] == _sessionId`
- If the user switched A→B, session A's assistant reply is appended to the list session B displays

**BUG 3c: WS subscription cancellation doesn't retract in-flight events**

- `chat_provider.dart:251` — old subscription cancelled on session switch
- `websocket_service.dart:524-528` — stream is correctly filtered by sessionId at the `.where()` level
- BUT: cancelling a `StreamSubscription` does not retract events already dispatched past the filter
- A `chat_message` for session A already past the filter lands in `addStreamMessage` after `_sessionId` has changed to B

**BUG 3d: loadMessages clears state on every session switch**

- `chat_provider.dart:240-244` — `state = const ChatState(messages: [], isLoading: true)` on every `loadMessages` call
- `chat_message_list.dart:45-52` — `didUpdateWidget` triggers `loadMessages` on session switch
- The user message is appended optimistically at send time, but switching away and back triggers a fresh HTTP fetch
- If the daemon hasn't persisted the exchange yet (`persistExchange` at `handler.go:803` runs after the response), the reloaded history won't include the just-sent message → "not visible initially, self-corrects later"

**Daemon side (defense-in-depth gap, not primary cause):**
- `internal/comm/http/server.go:497-511` — `handleWSEvent` broadcasts `chat_message` to ALL connected WS clients unconditionally
- Unlike `handleWSProgress` (line 515-580) which calls `ShouldSendProgress(wc, event.SessionID)`, `handleWSEvent` has no per-connection session filter
- The daemon DOES include `session_id` in the response payload (`server.go:624-626`), so the client has the data to filter on but doesn't use it

### Fix Direction
1. Add `message['session_id'] == _sessionId` guard at top of `addStreamMessage`
2. OR (preferred): make `chatProvider` a `.family` keyed by sessionId so each session has isolated state
3. Add generation check inside the WS callback (not just after HTTP fetch)
4. Server-side: add session filtering in `handleWSEvent` for defense-in-depth

---

## Issue 4: Raw JSON Responses

**Symptom:** Asking about the project returns raw JSON: `{"context":[],"count":0,"query":"..."}` and `{"status":"running","uptime_seconds":...}`

### Root Cause: Two JSON-leak paths, both bypass the LLM

**BUG 4a: project_info tool is invisible to the LLM**

- `internal/daemon/components.go:4582` — `project_info` IS registered in the tool registry
- `internal/agent/spec.go:114-126` — but `project_info` is NOT in `BaselineTools`. The LLM only sees tools the agent spec advertises.
- Unless an agent's spec explicitly lists `project_info` in `AdditionalTools`, the LLM never knows it exists
- Result: the LLM falls back to `memory_get_context` and `platform_status`, both of which return empty/unhelpful structured data

**BUG 4b: TerminateHint short-circuit bypasses LLM formatting**

- `internal/tools/builtin/platform.go:566-573` — `PlatformStatusTool`, `PlatformAgentsTool`, `PlatformToolsTool` all implement `TerminateHint() == true`
- `internal/agent/executor.go:1050-1055` — executor sets `terminate = true` when any tool's `TerminateHint(args)` returns true
- `internal/agent/loop.go:3143-3158` — the loop checks `if result.Terminate` and skips the LLM follow-up, calling `buildTerminateResponse(results)` instead
- `internal/agent/loop.go:5686-5708` — `buildTerminateResponse` JSON-marshals non-string tool results directly (`json.Marshal(r.Result)`) and joins them as the final user-facing response. No LLM reformatting.

The flow:
1. LLM sees no `project_info` tool → calls `memory_get_context` and/or `platform_status`
2. `platform_status` has `TerminateHint == true` → executor signals termination
3. Loop skips LLM reformatting → `buildTerminateResponse` dumps raw JSON
4. User sees `{"status":"running","uptime_seconds":6317...}`

**The anti-JSON guideline IS in the system prompt** (`baseline.go:26`, confirmed in `loop.go:4112`), but it's irrelevant because the `TerminateHint` path never calls the LLM again.

**Note on the `{"context":[],"count":0,...}` shape:**
- `internal/tools/builtin/memory.go:279-283` — `MemoryGetContextTool.Execute` returns exactly `{"context":[...],"count":N,"query":"..."}` 
- `MemoryGetContextTool` does NOT have `TerminateHint == true`, so this leak comes from a co-call: the LLM called both `memory_get_context` and `platform_status` in the same batch, `platform_status` triggered termination, and `buildTerminateResponse` dumped both results as raw JSON

### Fix Direction
1. Add `project_info` to `BaselineTools` in `internal/agent/spec.go` so the LLM can actually use it
2. Remove `TerminateHint() == true` from `PlatformStatusTool` (and similar introspection tools), OR
3. Make `buildTerminateResponse` format results as natural language instead of raw JSON (at minimum, for tools that return structured data)

---

## Dependency Map

```
Issue 1 (global indicator) ────── Flutter-only, independent
Issue 2 (agent blindness) ─────── BUG 2a → BUG 2b → BUG 2c (chain)
Issue 3 (race condition) ──────── BUG 3a → BUG 3b (primary), 3c/3d (secondary)
Issue 4 (JSON leak) ───────────── BUG 4a + BUG 4b (both required for the symptom)
```

Issues 2 and 4 are related: fixing 2a (bind ProjectPath at session creation) would give the agent loop the correct workingDir, which would populate the `# Current Project` system prompt section. Fixing 4a (add project_info to BaselineTools) would give the LLM the right tool. But even with both fixes, BUG 4b (TerminateHint bypass) would still leak JSON from any terminating tool call — so 4b must be fixed independently.

## Files Involved

| File | Issue(s) |
|------|----------|
| ui/flutter_ui/lib/widgets/status_bar.dart | 1 |
| ui/flutter_ui/lib/providers/project_provider.dart | 1 |
| ui/flutter_ui/lib/features/home/sidebar_home_screen.dart | 1 |
| ui/flutter_ui/lib/providers/chat_provider.dart | 3 |
| ui/flutter_ui/lib/services/websocket_service.dart | 3 |
| internal/session/session.go | 2 |
| internal/services/session_service.go | 2 |
| internal/rpc/proxy.go | 2 |
| internal/daemon/session_rpc.go | 2 |
| internal/agent/handler.go | 2, 3 |
| internal/daemon/components.go | 2, 4 |
| internal/agent/loop.go | 2, 4 |
| internal/agent/spec.go | 4 |
| internal/tools/builtin/platform.go | 4 |
| internal/tools/builtin/memory.go | 4 |
| internal/agent/executor.go | 4 |
| internal/comm/http/server.go | 3 |
