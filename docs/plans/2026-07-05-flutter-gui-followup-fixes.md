# Flutter GUI Follow-up Fixes — 2026-07-05

## Overview

Follow-up issues discovered after the 2026-07-04 fixes. The prior fixes were
partially effective; these address remaining gaps.

## Issues

### Issue 1: 'f' key STILL not entering text in chat input

**Symptom:** Pressing 'f' in chat input still does nothing.

**Root Cause:** The prior fix in `shortcuts.dart:163-170` calls
`_focusIsTextInput(primaryFocus)`, which walks the focus tree checking
`current.context?.widget is EditableText`. However:

1. Flutter's `TextField` widget is NOT itself an `EditableText` — it builds an
   internal `EditableText` child via `_TextFieldState`. The focus node owned
   by `TextField` is the `TextField`'s focus node, not the `EditableText`'s
   focus node. So `current.context?.widget` returns the `TextField` widget,
   not `EditableText`.

2. The check `widget is TextField || widget is EditableText` should work for
   TextField — BUT `current.context?.widget` returns the widget at the focus
   node's context, which may be a `Focus` or `_FocusBuilder` wrapper, not the
   `TextField` itself.

**Fix:** Replace the unreliable widget-type check with a robust check that
walks the focus tree and inspects each `FocusNode`'s context widget tree for
`EditableText` descendants. The most reliable approach: use
`FocusManager.instance.primaryFocus?.context?.findAncestorWidgetOfType<EditableText>()`
OR check if the primary focus node has an `EditableText` ancestor via
`context.findAncestorStateOfType<_EditableTextState>()`.

The simplest robust fix: import `package:flutter/widgets.dart` and use
`EditableText` (which is exported from `material.dart`), then check:

```dart
static bool _focusIsTextInput(FocusNode node) {
  final ctx = node.context;
  if (ctx == null) return false;
  // Walk the widget tree looking for an EditableText.
  // This handles TextField (which contains an EditableText) and bare
  // EditableText widgets.
  EditableText? editable = ctx.findAncestorWidgetOfType<EditableText>();
  if (editable != null) return true;
  // Also check the widget itself (rare case).
  final widget = ctx.widget;
  return widget is EditableText || widget is TextField;
}
```

Even more reliable: just check if the `primaryFocus`'s context has any
`EditableText` in its ancestry. If `findAncestorWidgetOfType<EditableText>()`
returns non-null, we're in a text field.

**Alternative (simpler):** Remove `_isGlobalSearchTrigger` entirely.
Single-key shortcuts are inherently dangerous in apps with text inputs. The
global search can be invoked via the command palette (Ctrl+X → search) or via
`Ctrl+F` (which already exists for in-session find). Removing the single-key
'f' trigger eliminates the entire class of bug.

**Decision:** Remove the `_isGlobalSearchTrigger` handler. It's a footgun.
The command palette and Ctrl+F cover the same affordance.

### Issue 2: Background image missing on empty sessions/plans/tasks right pane

**Symptom:**
- Sessions tab: when no session is selected, right pane has no background image
- Plans tab: actually HAS background image on empty state (verified in
  `plans_tab.dart:74-84` — `BackgroundImage` wraps the empty state correctly)
- Tasks tab: when no task is selected, right pane shows plain "select a task"
  without background image

**Root Cause:**
- `sessions_overview_tab.dart:23-28` only wraps the detail pane in
  `BackgroundImage` when `activeSession != null`. When null, no right pane
  renders at all.
- `tasks_tab.dart:33-43` has the same pattern — `BackgroundImage` only when
  `selectedTask != null`.
- `plans_tab.dart:74-84` is correct — `BackgroundImage` wraps the empty
  state too.

**Fix:**
- `sessions_overview_tab.dart`: Always render the right pane (wrap in
  `Expanded(child: BackgroundImage(child: ...))`). When no active session,
  show a "select a session" placeholder inside BackgroundImage.
- `tasks_tab.dart`: Same pattern — always render right pane with
  BackgroundImage.

### Issue 3: /project command behavior (must be robust)

**Symptom:** Typing `/project` and pressing Enter does nothing visible.

**Root Causes (multiple compound):**

1. **Double-Enter consumes first Enter:** When user types `/project` (no
   args) and presses Enter, the double-Enter detection consumes the first
   Enter (sets `_lastEnterTime`). The second Enter (within 300ms) calls
   `_sendSteer(_controller.text)` — NOT `_sendNormal`. `_sendSteer` calls
   `_preparePayload` then `chatProvider.sendSteer` WITHOUT checking
   `_tryHandleSlashCommand`. So `/project` is sent to daemon as a steer,
   bypassing local handling.

2. **No `setProject`/`registerProject` in sdk_client.dart:** Even when the
   command IS handled locally, the daemon's `/project <path>` flow requires
   calling `project.set` RPC (or POST /api/v1/projects/{id}/set) to bind
   the project to the current session. The Flutter SDK client lacks this
   method entirely — there's no way for `/project <path>` to actually take
   effect.

3. **Empty project list edge case:** `_loadProjectPaths()` returns from
   `listProjects()` which only returns REGISTERED projects. If user has no
   registered projects, the typeahead is empty. The `/project <path>`
   command should accept ARBITRARY paths (not just registered ones) — the
   daemon's `project.set` handler calls `DetectFromPath` for path-only
   invocations, auto-registering.

4. **No feedback on success/failure:** The command silently does nothing
   visible — no status message, no error toast.

**Robust Fix (multi-part):**

**3a. Dispatch slash commands on first Enter (no debounce):**
In `_handleKeyEvent`, when first Enter is pressed AND text starts with `/`,
call `_sendNormal(text)` immediately. Don't wait for double-Enter. Slash
commands are explicit user intent and shouldn't be debounced.

**3b. Add `setProject` method to sdk_client.dart:**
```dart
/// Bind a project to a session via path-based detection.
/// Calls project.set RPC which auto-registers the path if not already
/// registered (via DetectFromPath on the daemon side).
Future<Map<String, dynamic>> setProject({
  required String sessionId,
  String? projectId,
  String? path,
}) async {
  final body = <String, dynamic>{
    'session_id': sessionId,
    if (projectId != null) 'project_id': projectId,
    if (path != null) 'path': path,
  };
  return _post('/api/v1/sessions/$sessionId/project', body: body);
}
```

NOTE: Check daemon's HTTP routes — `project.set` may be RPC-only. If no
HTTP route exists, use the RPC bridge (POST /api/v1/rpc with method
`project.set`). Verify the actual endpoint name in
`internal/comm/http/server.go`.

**3c. Implement `/project <path>` end-to-end:**
- In `_tryHandleSlashCommand`, handle `/project <path>` (with args):
  1. Call `sdkClient.setProject(sessionId: currentSessionId, path: path)`.
  2. On success: refresh `currentProjectProvider`, show success status.
  3. On failure: show error status.
  4. Reset input state.
- For `/project` (no args): trigger the typeahead popup by setting
  `_showSlashAutocomplete = true` and `_slashQuery = '/project '` so the
  existing autocomplete UI shows project paths.

**3d. Show feedback:**
After successful project set, show a transient status message
("project set: <path>") via `statusMessageProvider`. On failure, show
the error.

**3e. Edge cases to handle:**
- Empty path arg: show typeahead popup.
- Path doesn't exist: daemon returns error — surface it.
- Path is already active: idempotent — show success.
- No active session: show error "create or select a session first".
- Path with trailing slash: normalize via `path.normalize()` before send.
- Daemon unreachable: surface connection error.

### Issue 4: Status bar project path not displayed

**Symptom:** Status bar doesn't show project path.

**Root Cause:** No project is marked `status: "active"` in the daemon.
The code in `project_provider.dart` and `status_bar.dart` is correct —
`currentProjectProvider.refresh()` calls `listProjects()` and looks for
`status == 'active'`. If none exists, returns empty.

**Fix:** No code change needed for this issue per se. The fix is operational:
- The `/project <path>` command (once Issue 3 is fixed) should let the user
  select a project, which the daemon will mark as active.
- Verify the daemon's project-add endpoint sets the project as active.

**Action:** Add a fallback to `_projectPart` in `status_bar.dart` that shows
`[no project]` when no project is active, so the user knows the state.

### Issue 5: Auto-create new session on startup

**Symptom:** User wants meept to always default to a new session on startup.

**Root Cause:** `home_screen.dart:276-289` (`_onConnectionChanged`) calls
`sessionProvider.loadSessions()` but does NOT create a new session. The chat
tab uses `widget.sessionId` which is `'default'` (from the router) — the
daemon lazily creates the default session on first message.

**Fix:** In `_onConnectionChanged`, after loading sessions, create a new
session if none is active. Set it as `activeSessionProvider`.

### Issue 6: Session title auto-derivation (LLM-generated)

**Symptom:** New sessions don't get auto-derived titles.

**Root Cause:** The TUI calls `rpc.GenerateSessionDescription(sessionID,
firstUserContent, "")` after the first user message
(`internal/tui/models/chat.go:1826-1877`). The Flutter GUI does NOT call
this — there's no `generateSessionDescription` method in
`sdk_client.dart` or any hook in `chat_provider.dart` to trigger it after
the first message.

This is a **client-side behavior** — the daemon provides the API
(`session.generate_description`), but the client must call it. The daemon
does NOT auto-trigger it server-side.

**Fix:**
1. Add `generateSessionDescription` to `sdk_client.dart` — call
   `session.generate_description` RPC.
2. In `chat_provider.dart` after the first user message in a session, call
   `generateSessionDescription` and update the session title from the result.

### Issue 7: "+" button on sessions tab should auto-create + switch to chat

**Symptom:** User wants the "+" button to auto-create a new session and
switch to chat tab without prompting for a title.

**Root Cause:** `sessions_list.dart:286` calls `_showCreateSessionDialog()`
which shows a dialog asking for a title.

**Fix:** Replace `_showCreateSessionDialog()` with `_createQuickSession()`
that:
1. Generates a placeholder title (e.g. "session" — will be auto-derived
   after first message via Issue 6 fix).
2. Calls `sessionProvider.createSession(title)`.
3. Sets the new session as active.
4. Switches to chat tab (set `_selectedTab = HomeTab.chat` via
   `tabActivationProvider` and `context.go('/')`).

## Implementation Plan

### Phase 1: Remove single-key 'f' global search trigger
**File:** `ui/flutter_ui/lib/core/shortcuts.dart`

Remove or neuter `_isGlobalSearchTrigger` and its call in `handleKeyEvent`.
Also remove `onGlobalSearch` field and the listener in `home_screen.dart`
that references it (or keep the field for future use but don't call it).

The simplest change: in `handleKeyEvent`, delete the entire block:
```dart
if (_isGlobalSearchTrigger(event)) {
  ...
}
```

Keep `_isGlobalSearchTrigger` and `_focusIsTextInput` as dead code for now
(or delete them — they have no other callers).

### Phase 2: Always show background image on right pane
**Files:**
- `ui/flutter_ui/lib/features/sessions/sessions_overview_tab.dart`
- `ui/flutter_ui/lib/features/tasks/tasks_tab.dart`

Restructure to always render the right pane with `BackgroundImage`:
```dart
Expanded(
  child: BackgroundImage(
    child: active != null
      ? DetailPane(...)
      : Center(child: Text('select a ...')),
  ),
)
```

### Phase 3: Fix /project command — instant slash dispatch + typeahead
**File:** `ui/flutter_ui/lib/features/chat/chat_input.dart`

In `_handleKeyEvent` Enter handling:
```dart
// Detect slash commands early — they don't need double-Enter debounce.
if (event.logicalKey == LogicalKeyboardKey.enter) {
  if (ref.read(chatProvider).isLoading) return KeyEventResult.ignored;
  if (isShiftPressed) { /* newline */ return handled; }
  if (_showSlashAutocomplete) { /* accept autocomplete */ return handled; }

  final text = _controller.text;
  if (text.startsWith('/')) {
    // Slash commands dispatch immediately, no debounce.
    _sendNormal(text);
    return KeyEventResult.handled;
  }

  // Regular messages use double-Enter.
  ... existing debounce logic ...
}
```

### Phase 4: Auto-create session on startup
**File:** `ui/flutter_ui/lib/features/home/home_screen.dart`

In `_onConnectionChanged`:
```dart
if (connected && !_initialLoadDone) {
  _initialLoadDone = true;
  await ref.read(sessionProvider.notifier).loadSessions();
  // ... existing loads ...
  // Auto-create a new session if none active.
  final active = ref.read(activeSessionProvider);
  if (active == null) {
    final session = await ref.read(sessionProvider.notifier)
        .createSession('new session');
    if (session != null) {
      ref.read(activeSessionProvider.notifier).state = session;
    }
  }
}
```

Make `_onConnectionChanged` async to await the loads.

### Phase 5: Session title auto-derivation
**Files:**
- `ui/flutter_ui/lib/services/sdk_client.dart` — add `generateSessionDescription`
- `ui/flutter_ui/lib/providers/chat_provider.dart` — trigger after first message

Add to `sdk_client.dart`:
```dart
Future<Map<String, dynamic>> generateSessionDescription({
  required String sessionId,
  required String firstMessage,
  String projectName = '',
}) async {
  return _callRpc('session.generate_description', {
    'session_id': sessionId,
    'first_message': firstMessage,
    'project_name': projectName,
  });
}
```

In `chat_provider.dart`, after sending the first user message in a session,
call `generateSessionDescription` and update the active session's title.

### Phase 6: "+" button auto-create + switch to chat
**File:** `ui/flutter_ui/lib/features/sessions/sessions_list.dart`

Replace `_showCreateSessionDialog` (called from `+` IconButton) with a new
`_createQuickSession`:
```dart
Future<void> _createQuickSession() async {
  final notifier = ref.read(sessionProvider.notifier);
  final session = await notifier.createSession('new session');
  if (session != null) {
    ref.read(activeSessionProvider.notifier).state = session;
    ref.read(chatProvider.notifier).clearMessages();
    ref.read(tabActivationProvider.notifier).state = HomeTab.chat;
    if (context.mounted) context.go('/');
  }
}
```

Update the `+` IconButton onPressed to call `_createQuickSession`.

Keep `_showCreateSessionDialog` for the command-palette path
(`createSessionRequestProvider`) or remove it entirely if the palette should
also use quick-create.

### Phase 7: Status bar fallback
**File:** `ui/flutter_ui/lib/widgets/status_bar.dart`

In `_projectPart`:
```dart
String? _projectPart(WidgetRef ref) {
  final p = ref.watch(currentProjectProvider);
  if (!p.isActive) return '[no project]';  // Show this instead of null
  ... existing logic ...
}
```

## Verification

After all fixes:
1. `flutter analyze` — 0 errors
2. Manual testing:
   - Type 'f' repeatedly in chat input — characters should appear
   - Sessions tab with no selection — right pane shows background image
   - Tasks tab with no selection — right pane shows background image
   - Type `/project` + Enter — autocomplete popup appears with paths
   - Status bar shows `[no project]` initially, then real path after /project
   - On startup, a new session is created automatically
   - After first message in a session, title auto-derives
   - Click `+` on sessions tab — new session created, switches to chat tab
