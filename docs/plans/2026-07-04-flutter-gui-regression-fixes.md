# Flutter GUI Regression Fixes — 2026-07-04

## Overview

Several GUI features are broken or incorrectly implemented due to prior
incomplete/incorrect edits. This plan identifies root causes, cleans up
the bad code, and implements correct fixes for each issue.

## Issues Identified

### Issue 1: Background image positioning broken on Sessions/Tasks tabs
**Symptoms:**
- Sessions tab: right pane shows white box, session selection broken
- Tasks tab: no background image on right pane
- Plans tab: background image covers both panes (should be right only)

**Root Causes:**

1. **`sessions_detail.dart:104`** — Returns `Expanded(child: Container(...))` from its build method. When this widget is then wrapped in `Expanded` again in `sessions_overview_tab.dart:24`, the double-Expanded causes a layout exception, which Flutter renders as a white box. This is why session selection "isn't working correctly" — the layout is broken.

2. **`tasks_tab.dart:30`** — `BackgroundImage` wraps `const TasksDetail()`. Need to verify `TasksDetail` doesn't have its own Expanded/Container conflict. Also check that `BackgroundImage` is properly visible.

3. **`plans_tab.dart`** — Prior fix moved BackgroundImage to the right pane but needs verification.

**Fix Approach:**
- Remove `Expanded` wrapper from `sessions_detail.dart:104` — let the parent handle layout
- Verify all three tabs (sessions, plans, tasks) follow the same pattern:
  ```dart
  Row(
    children: [
      SizedBox(width: 280, child: LeftList()),    // black bg
      Expanded(child: BackgroundImage(child: RightDetail())),  // image bg
    ],
  )
  ```
- Remove any stray Container/Color overlays in detail panes that hide the image

### Issue 2: Status bar project path not displayed
**Symptoms:** Status bar at bottom doesn't show project path

**Root Cause:**
The data flow is: daemon → `listProjects()` → `currentProjectProvider.refresh()` → `StatusBar._projectPart()`.

The JSON key fix (`local_path` snake_case) was already applied. The issue is likely that:
1. No project has `status: "active"` so `currentProjectProvider` returns empty
2. The refresh isn't being triggered at the right time
3. The daemon has no active project set

**Fix Approach:**
- Mirror TUI behavior: TUI uses `os.Getwd()` as a fallback display when no project is active
- In Flutter, we can't get the daemon's CWD directly, but the TUI's project display is `a.projectDir` which is the *client's* working directory
- Show the daemon's project list — if any project is "active", show its `local_path`
- Add a fallback: if no active project, optionally show a default indicator or omit (TUI shows `projectDir` which is the local CWD; in Flutter web this doesn't apply)

The status bar fix is primarily: verify the API call works and the field is read correctly. The code in `project_provider.dart:84` and `status_bar.dart:141` looks correct. May need to check if the daemon even has an active project.

### Issue 3: /project slash command doesn't work
**Symptoms:** Typing `/project` just repopulates the menu, nothing happens

**Root Cause:**
Looking at `chat_input.dart:586`:
```dart
case '/project':
  if (spaceIdx == -1) {
    ref.read(filePickerTriggerProvider.notifier).trigger();
    return true;
  }
  return false;
```

When user types `/project` and presses Enter (double-Enter to send), `_tryHandleSlashCommand` is called with payload `/project`. Since there's no space, it triggers `filePickerTriggerProvider`.

`filePickerTriggerProvider` is set to `true`, then immediately (100ms later) reset to `false`. The listener at `chat_input.dart:837` calls `_handleProjectFilePicker()`.

The issue: `_handleProjectFilePicker()` was rewritten to show a dialog of project paths, but `_projectPaths` may be empty (the `_loadProjectPaths()` call in `initState` may not have loaded yet, or there may be no projects).

Also, the user says "it just repopulates the menu" — this suggests the slash autocomplete is showing for `/project` but the command itself isn't actually being sent. The double-Enter detection may be consuming the Enter key presses before the slash command handler runs.

**Fix Approach:**
- When `/project` is sent with no arguments, show project autocomplete popup with loaded project paths
- If no projects loaded, show "no projects available" message
- Ensure the project list is loaded before showing the popup (call `_loadProjectPaths()` if empty)
- Make `/project` with no args trigger the autocomplete popup directly, not a separate dialog
- For `/project <partial-path>`, show filtered paths

### Issue 4: 'f' key doesn't enter text in chat input
**Symptoms:** Pressing 'f' in the chat text entry does nothing

**Root Cause:**
In `shortcuts.dart:209`, `_isGlobalSearchTrigger()` returns true for any 'f' keypress with no modifiers:

```dart
static bool _isGlobalSearchTrigger(KeyEvent event) {
  if (event is! KeyDownEvent) return false;
  if (event.logicalKey != LogicalKeyboardKey.keyF) return false;
  // ... checks no modifiers
  return true;
}
```

And `handleKeyEvent()` at line 161 calls `onGlobalSearch?.call()` and returns `KeyEventResult.handled` — which **prevents the 'f' keypress from reaching the TextField**.

The `AppShortcuts` Focus widget at line 301 has `autofocus: true` and intercepts keys before the chat input TextField gets them.

The intent of `_isGlobalSearchTrigger` is: when on the sessions tab, single 'f' opens global search. But it's eating the 'f' key globally, including when typing in chat input.

**Fix Approach:**
- Check if focus is currently in a text input (TextField/EditableText). If so, don't intercept the 'f' key.
- OR: Only trigger global search when NOT on the chat tab (since single-key shortcuts are session-tab-specific)
- OR: Remove `_isGlobalSearchTrigger` entirely — single-key shortcuts are dangerous in apps with text inputs. Use a different shortcut (e.g., modifier+f or just Ctrl+F which is already find)

Best approach: Check if primary focus is a text field. If so, ignore the global search trigger.

### Issue 5: Chat multiline text selection doesn't work
**Symptoms:** Can't click and drag to select text across multiple lines/bubbles in the chat transcript. Only single-line selection works.

**Root Cause:**
The `SelectionArea` was added in `chat_message_list.dart:121` and also in `chat_tab.dart:34`. However, nested `SelectionArea` widgets may conflict. Also, the `MarkdownBody` widget in `chat_message_bubble.dart:132` uses `selectable: true` which creates its own selection region — this conflicts with the outer `SelectionArea`.

`MarkdownBody` with `selectable: true` uses `SelectableText` internally, which creates its own `SelectionRegistrar`. When wrapped in an outer `SelectionArea`, the two selection systems conflict, causing selection to only work within a single bubble's text (single-line behavior).

**Fix Approach:**
- Option A: Remove `SelectionArea` wrapper, keep `MarkdownBody(selectable: true)`. This gives per-bubble selection but not cross-bubble.
- Option B: Remove `selectable: true` from MarkdownBody, keep outer `SelectionArea`. This should allow cross-bubble selection. But MarkdownBody may use its own Text widgets that aren't selection-aware.
- Option C (recommended): Use `SelectionArea` only at the chat message list level. Remove `selectable: true` from MarkdownBody. Ensure all text widgets inside the bubbles are plain `Text` widgets (not `SelectableText`). The `SelectionArea` ancestor will make all descendant `Text` widgets selectable.

For full terminal/editor-like selection:
1. Remove `SelectionArea` from `chat_tab.dart:34` (avoid double-nesting)
2. Keep `SelectionArea` in `chat_message_list.dart:121`
3. Remove `selectable: true` from `MarkdownBody` in `chat_message_bubble.dart:141`
4. Verify the `Text` widgets for system messages, timestamps, etc. inherit selection from the ancestor `SelectionArea`

## Cleanup: Prior Incorrect Edits

The following files have leftover code from prior incorrect fix attempts that should be cleaned up:

1. **`chat_tab.dart`** — Has nested `SelectionArea` that conflicts with the one in `chat_message_list.dart`. Remove the one in `chat_tab.dart`.

2. **`sessions_overview_tab.dart`** — Has correct structure now (BackgroundImage on right pane only), but verify no regression.

3. **`sessions_detail.dart:104`** — Returns `Expanded(...)` from build, causing double-Expanded layout error when wrapped in `Expanded` in the parent. Remove the inner Expanded.

4. **`home_screen.dart`** — Verify no syntax issues remain from prior parenthesis fixes.

5. **`tasks_tab.dart`** — Verify BackgroundImage is on the right pane.

6. **`chat_view.dart`** — Uses BackgroundImage wrapper for the chat content. Verify this is correct.

7. **`plans_tab.dart`** — Verify BackgroundImage is on the right pane only.

## Implementation Plan

### Phase 1: Background Image Layout Fixes
Files: `sessions_detail.dart`, `sessions_overview_tab.dart`, `tasks_tab.dart`, `plans_tab.dart`

1. **`sessions_detail.dart`**: Remove `Expanded` wrapper from line 104. Change `return Expanded(child: Container(...))` to `return Container(...)`.

2. **`sessions_overview_tab.dart`**: Verify correct structure:
   ```dart
   Row(
     children: [
       SizedBox(width: 280, child: SessionsList()),
       if (activeSession != null)
         Expanded(child: BackgroundImage(child: SessionsDetailPane(...))),
     ],
   )
   ```

3. **`tasks_tab.dart`**: Verify BackgroundImage wraps only the right pane.

4. **`plans_tab.dart`**: Verify BackgroundImage wraps only the right pane.

### Phase 2: 'f' Key Fix
File: `shortcuts.dart`

Modify `_isGlobalSearchTrigger` or `handleKeyEvent` to NOT intercept the 'f' key when focus is in a text input.

Approach: In `handleKeyEvent`, check if the current primary focus is a text field. If so, skip the global search trigger.

```dart
// In handleKeyEvent, before the _isGlobalSearchTrigger check:
if (_isGlobalSearchTrigger(event)) {
  // Don't intercept 'f' when typing in a text field
  final isEditingText = PrimaryScrollController.of(context) != null
      // or check FocusManager.instance.primaryFocus?.context?.widget is EditableText
  if (!isEditingText) {
    onGlobalSearch?.call();
    return KeyEventResult.handled;
  }
}
```

Better: Check if `FocusManager.instance.primaryFocus` is or descends from an `EditableText`.

### Phase 3: Multiline Text Selection Fix
Files: `chat_tab.dart`, `chat_message_list.dart`, `chat_message_bubble.dart`

1. **`chat_tab.dart`**: Remove the `SelectionArea` wrapper (line 34). Keep `BackgroundImage`.
   ```dart
   return BackgroundImage(
     child: activeTool.isNotEmpty ? _buildToolView(activeTool) : ChatView(...),
   );
   ```

2. **`chat_message_list.dart`**: Keep the `SelectionArea` at line 121. This is the single source of selection for the message list.

3. **`chat_message_bubble.dart`**: Change `MarkdownBody(selectable: true, ...)` to `MarkdownBody(selectable: false, ...)` (or remove the `selectable` parameter). The outer `SelectionArea` will handle selection.

   **IMPORTANT**: Test this — `MarkdownBody` may not work with `SelectionArea` ancestor. If it doesn't, fall back to keeping `selectable: true` on MarkdownBody and accept per-bubble selection as the best achievable behavior (document this as a Flutter limitation).

### Phase 4: /project Command Fix
File: `chat_input.dart`

The `/project` command should:
1. With argument (`/project /some/path`): send to backend as a chat message with the project path
2. Without argument (`/project`): show project autocomplete popup

Current behavior: `/project` with no args triggers `filePickerTriggerProvider` → `_handleProjectFilePicker()` → shows a dialog. But the dialog may not appear if `_projectPaths` is empty.

**Fix:**
1. In `_handleProjectFilePicker()`, if `_projectPaths` is empty, call `_loadProjectPaths()` first, then show the dialog
2. Better: instead of a separate dialog, set `_showSlashAutocomplete = true` with `_slashQuery = '/project '` so the existing autocomplete UI shows the project paths
3. The slash autocomplete already handles `/project <partial>` — make `/project` (no arg) also trigger it

### Phase 5: Status Bar Project Path Verification
Files: `project_provider.dart`, `status_bar.dart`, `home_screen.dart`

1. Verify the API call returns `local_path` field correctly
2. Verify `currentProjectProvider.refresh()` is called after connection
3. Check if any project is actually marked as "active" in the daemon
4. Consider showing a default path (like TUI's `projectDir`) if no project is active

**Action:** Add debug logging to `_projectPart()` in `status_bar.dart` to verify what data is being received. Also check the raw API response.

## Verification

After all fixes:
1. Run `flutter analyze` — must show 0 errors
2. Run `flutter build macos` or `flutter run -d macos` to verify build
3. Manual testing:
   - Sessions tab: left pane black, right pane has background image, session selection works
   - Tasks tab: left pane black, right pane has background image
   - Plans tab: left pane black, right pane has background image
   - Chat tab: background image visible in chat area
   - Type 'f' in chat input — character should appear in text field
   - Select text in chat — should select across multiple lines/bubbles
   - Type `/project` — should show project selection UI
   - Status bar shows project path when a project is active

## File Summary

| File | Changes |
|------|---------|
| `lib/features/sessions/sessions_detail.dart` | Remove `Expanded` from build return |
| `lib/features/sessions/sessions_overview_tab.dart` | Verify BackgroundImage placement |
| `lib/features/tasks/tasks_tab.dart` | Verify BackgroundImage on right pane |
| `lib/features/plans/plans_tab.dart` | Verify BackgroundImage on right pane |
| `lib/core/shortcuts.dart` | Don't intercept 'f' key when typing in text field |
| `lib/features/chat/chat_tab.dart` | Remove duplicate SelectionArea |
| `lib/features/chat/chat_message_list.dart` | Keep SelectionArea, verify structure |
| `lib/features/chat/chat_message_bubble.dart` | Remove `selectable: true` from MarkdownBody |
| `lib/features/chat/chat_input.dart` | Fix `/project` command to show autocomplete |
| `lib/widgets/status_bar.dart` | Add debug logging to verify project data |
| `lib/providers/project_provider.dart` | Verify `local_path` field handling |
