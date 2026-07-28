# Plan: Convert chatProvider to .family (per-session isolation)

## Problem

`chatProvider` is a single global `StateNotifierProvider<ChatNotifier, ChatState>`. All sessions share one ChatNotifier instance. Switching sessions mutates `_sessionId` on the shared notifier, creating race conditions where responses from session A land in session B. The session_id guard I added stops the worst symptom but the architecture is fundamentally wrong.

## Solution

Convert to `StateNotifierProvider.family<ChatNotifier, ChatState, String>` keyed by sessionId. Each session gets its own isolated ChatNotifier with its own state, WS subscription, and progress subscription.

## Call Site Inventory (17 sites across 9 files + 3 test files)

### Files that HAVE sessionId available (straightforward .family migration)

1. **chat_message_list.dart** (4 sites, has `widget.sessionId`)
   - Line 57: `ref.read(chatProvider.notifier).loadMessages(widget.sessionId)` → `ref.read(chatProvider(widget.sessionId).notifier)` (and remove the sessionId arg — loadMessages becomes parameterless, called in initState)
   - Line 58: `ref.read(chatProvider)` → `ref.watch(chatProvider(widget.sessionId))`
   - Line 87: `ref.watch(chatProvider)` → `ref.watch(chatProvider(widget.sessionId))`
   - Line 223: `ref.read(chatProvider.notifier).clearError()` → `ref.read(chatProvider(widget.sessionId).notifier).clearError()`

2. **chat_view.dart** (2 sites, has `widget.sessionId`)
   - Line 44: `ref.listen<ChatState?>(chatProvider, ...)` → `ref.listen<ChatState?>(chatProvider(widget.sessionId), ...)`
   - Line 60: `ref.read(chatProvider.notifier).resolveConfirmation(...)` → `ref.read(chatProvider(widget.sessionId).notifier).resolveConfirmation(...)`

3. **chat_input.dart** (9 sites, has `widget.sessionId`)
   - Line 654: `ref.read(chatProvider.notifier).clearMessages()` → `ref.read(chatProvider(widget.sessionId).notifier).clearMessages()`
   - Line 912: same pattern
   - Line 916: `sendSteer` → same pattern
   - Line 982: `ref.read(chatProvider).messages.isEmpty` → `ref.read(chatProvider(widget.sessionId)).messages.isEmpty`
   - Line 991: `ref.read(chatProvider.notifier)` → same pattern
   - Line 1027: same pattern
   - Line 1094: `sendSteer` → same pattern
   - Line 1107: `ref.read(chatProvider).isLoading` → `ref.read(chatProvider(widget.sessionId)).isLoading`
   - Line 1437: `ref.watch(chatProvider)` → `ref.watch(chatProvider(widget.sessionId))`

4. **search_panel.dart** (1 site, has `session.id` in scope)
   - Line 575: `ref.read(chatProvider.notifier).loadMessages(session.id)` → `ref.read(chatProvider(session.id).notifier)` (auto-loads on creation)

### Files that DON'T have sessionId (need indirection via activeSessionProvider)

5. **home_screen.dart** (1 site)
   - Line 245: `ref.read(chatProvider)` — just triggers initialization. With .family, this becomes a no-op (providers auto-init on first watch). Can remove or replace with a comment.

6. **tab_content.dart** (1 site)
   - Line 86: `ref.read(chatProvider.notifier).clearMessages()` — called after creating a new session. With .family, the new session's provider auto-initializes empty. Can remove this call.

7. **sidebar_home_screen.dart** (1 site)
   - Line 129: `ref.read(chatProvider)` — same as home_screen, just triggers init. Remove.

8. **sessions_list.dart** (2 sites)
   - Line 107: `ref.read(chatProvider.notifier).clearMessages()` — after creating session. Same as tab_content — the new session's provider starts empty. Remove.
   - Line 139: same pattern. Remove.

### Test files (3 files)

9. **session_swap_render_test.dart** — uses `chatProvider.overrideWith(...)`. Needs `.overrideWith((ref, sessionId) => ...)` for family.

10. **chat_message_list_test.dart** — same pattern.

11. **chat_input_test.dart** — same pattern.

## Implementation Steps

### Step 1: Change the provider declaration (chat_provider.dart)

```dart
// OLD:
final chatProvider = StateNotifierProvider<ChatNotifier, ChatState>((ref) {
  ...
});

// NEW:
final chatProvider =
    StateNotifierProvider.family<ChatNotifier, ChatState, String>((ref, sessionId) {
  final client = ref.watch(sdkClientProvider);
  final websocket = ref.watch(websocketProvider);
  final ttsNotifier = ref.read(ttsProvider.notifier);
  return ChatNotifier(client, websocket, ttsNotifier, sessionId);
});
```

### Step 2: Simplify ChatNotifier constructor

ChatNotifier currently stores `_sessionId` and sets it in `loadMessages`. With .family:
- Constructor takes `sessionId` as a required param
- `_sessionId` becomes a final field set at construction
- `loadMessages(String sessionId)` becomes `loadMessages()` — uses the constructor's sessionId
- The generation guard and session-switch state-clearing logic can be simplified (each session has its own state)

### Step 3: Update all 17 call sites

For files with `widget.sessionId` available (chat_message_list, chat_view, chat_input, search_panel): mechanical replacement `chatProvider` → `chatProvider(widget.sessionId)` or `chatProvider(session.id)`.

For files without sessionId (home_screen, tab_content, sidebar_home_screen, sessions_list): remove the call entirely. These were either initialization triggers (unnecessary with .family) or clearMessages calls (unnecessary — new providers start empty).

### Step 4: Remove the session_id guard from addStreamMessage

The guard I added (`data['session_id'] != _sessionId`) becomes unnecessary — each provider instance only subscribes to its own session's WS channel. Keep it as defense-in-depth if desired, but it's no longer load-bearing.

### Step 5: Update tests

All 3 test files use `chatProvider.overrideWith(...)`. Family providers use `.overrideWith((ref, arg) => ...)`. Each test needs to pass a sessionId argument.

### Step 6: Handle auto-dispose (optional optimization)

Without autoDispose, every session ever opened keeps its ChatNotifier alive in memory. For a desktop app with moderate session counts this is fine. If needed later, convert to `.autoDispose.family` with a keepAlive condition.

## Risk Assessment

- **Medium risk**: 17 call sites across 9 files, but the change is mechanical (insert `(sessionId)` after `chatProvider`).
- **Test breakage**: 3 test files need updated override syntax.
- **Memory**: without autoDispose, old session providers stay alive. Acceptable for desktop.
- **Clear/remove calls**: removing clearMessages calls is safe — new .family providers start with empty state.

## Verification

1. `flutter analyze lib/` — no new errors
2. Manual test: create 2 sessions, send message in A, rapidly switch to B, verify response stays in A
3. Manual test: switch back to A, verify message history is intact (no flicker)
4. All existing tests pass with updated overrides
