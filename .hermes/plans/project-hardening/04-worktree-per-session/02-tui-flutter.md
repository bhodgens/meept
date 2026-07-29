# Leaf: Worktree TUI + Flutter Wiring

DISPATCH INSTRUCTION: Implement this leaf. Do NOT commit. Do NOT run git add. Write code, run tests, report results only.

## Parent
`04-worktree-per-session/orchestrator.md`

## Dependencies
Leaf `01-go-backend.md` must be completed first (needs RPC endpoints).

## Scope
Flutter UI: slash command support, status bar worktree indicator, sidebar display.

## Tasks

### Task 1: Add /worktree to Flutter slash commands
File: `ui/flutter_ui/lib/core/slash_commands.dart`

Add to `_defaultCommands`:
```dart
SlashCommand(
  name: '/worktree',
  description: 'create a session-scoped worktree',
  usage: '/worktree [create|remove]',
),
```

### Task 2: Handle /worktree in chat input
File: `ui/flutter_ui/lib/features/chat/chat_input.dart`

Add handling in `_tryHandleSlashCommand` (or wherever slash commands are processed):

For `/worktree` (no args or `create`):
1. Call `sdk.setProject` or a new `sdk.createWorktree(sessionId, projectId)` method
2. Add the command to session history
3. Refresh session list (loadSessions)
4. Show a status message: "worktree created: <branch>"

For `/worktree remove`:
1. Call `sdk.removeWorktree(sessionId)`
2. Add to history
3. Refresh session list
4. Show: "worktree removed, reverted to main project"

File: `ui/flutter_ui/lib/services/sdk_client.dart`
Add methods:
```dart
Future<Map<String, dynamic>> createWorktree({required String sessionId, required String projectId}) async {
  return _call('project.worktree.create', {'session_id': sessionId, 'project_id': projectId});
}

Future<void> removeWorktree({required String sessionId}) async {
  await _call('project.worktree.remove', {'session_id': sessionId});
}
```

Use the existing RPC call pattern (search for how `setProject` calls the daemon via RPC).

### Task 3: Status bar worktree indicator
File: `ui/flutter_ui/lib/widgets/status_bar.dart`

In `_projectPart`, when the session has a WorktreePath (check `session.worktreePath` if the Session model has it), show a worktree indicator:

```dart
// If session has a worktree, show [wt:branch-name] instead of the project name
if (session?.worktreePath != null && session!.worktreePath!.isNotEmpty) {
  final branch = session.worktreePath!.split('/').last;
  return '[wt:$branch]';
}
```

File: `ui/flutter_ui/lib/models/api_models.dart`
Add `worktreePath` field to Session if not already present:
```dart
@JsonKey(name: 'worktree_path') String? worktreePath,
```

### Task 4: Sidebar — show worktree sessions distinctly
File: `ui/flutter_ui/lib/features/home/sidebar_home_screen.dart`

In the session list item rendering, if a session has a WorktreePath, show a small icon or prefix (e.g. a branch icon or "wt:" prefix) next to the session name.

This is a minor visual enhancement — keep it simple.

## Self-Verification Checklist
- [ ] `flutter analyze lib/` — no new errors
- [ ] `/worktree` slash command appears in autocomplete
- [ ] `/worktree` create calls the RPC and refreshes the session
- [ ] `/worktree remove` reverts the session
- [ ] Status bar shows worktree indicator when active
- [ ] Session model parses `worktree_path` from API response
