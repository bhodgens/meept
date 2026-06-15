# Flutter and TUI UI Fixes Design Document

**Date:** 2026-06-15
**Status:** Draft
**Author:** Claude Code

## Overview

This document specifies fixes for multiple UI issues in both the Flutter client and Go-based TUI. The fixes address navigation, rendering, session management, and crash bugs.

---

## Issue 1: Tab Transition Slide Effect (Flutter)

### Problem
When clicking tabs at the top of the Flutter UI, the entire content slides from left to right. The tab bar should remain fixed while only the content area changes instantly.

### Root Cause
GoRouter's default `PageBuilder` creates `MaterialPage` objects which apply platform-default page transitions. On macOS/mobile, this defaults to a slide animation.

### Solution
Override the page builder in `router.dart` to use `noTransition` for all tab routes. The `_HomeShell` wrapper should swap tab content without animation.

### Implementation Details
**File:** `ui/flutter_ui/lib/core/router.dart`

1. Add a custom `PageBuilder` that returns `MaterialPage` with `transition: PageTransitionType.none`
2. Apply this to all tab routes (`/`, `/sessions`, `/plans`, `/tasks`, `/agents`)
3. Keep full-screen routes (`/settings`, `/tools/*`) with default transitions if desired

```dart
// Add to router.dart
GoRoute(
  path: '/',
  name: 'chat',
  pageBuilder: (context, state) => const MaterialPage(
    child: _HomeShell(initialTab: HomeTab.chat),
    transition: PageTransitionType.fade, // or none
  ),
),
```

### Success Criteria
- Clicking tabs results in instant content swap
- No sliding animation visible
- Tab bar remains completely stationary

---

## Issue 2: Connection Status Popup (Flutter)

### Problem
Clicking the "connected"/"disconnected" indicator in the upper right does nothing. Users need contextual actions to disconnect or reconnect.

### Root Cause
`_ConnectionDot` widget in `home_screen.dart` is a simple display widget with no interaction handlers.

### Solution
Wrap the connection indicator in a popup menu that shows context-appropriate actions:
- When connected: "Disconnect" option
- When disconnected: "Reconnect" option

### Implementation Details
**File:** `ui/flutter_ui/lib/features/home/home_screen.dart`

1. Replace the plain `Row` in `_ConnectionDot` with a `PopupMenuButton` or `GestureDetector` + `showDialog`
2. Dialog options:
   - Connected state: "Disconnect from daemon" → calls `websocket.disconnect()`
   - Disconnected state: "Reconnect to daemon" → calls `websocket.connect()`
3. Add visual feedback (tooltip or hint text)

```dart
// Example approach
PopupMenuButton<String>(
  onSelected: (value) {
    if (value == 'disconnect') {
      ref.read(websocketProvider).disconnect();
    } else if (value == 'reconnect') {
      ref.read(websocketProvider).connect();
    }
  },
  itemBuilder: (context) => connected
    ? [const PopupMenuItem(value: 'disconnect', child: Text('disconnect'))]
    : [const PopupMenuItem(value: 'reconnect', child: Text('reconnect'))],
)
```

### Success Criteria
- Clicking status indicator opens contextual popup
- Disconnect action closes WebSocket connection
- Reconnect action re-establishes connection
- Status text updates to reflect new state

---

## Issue 3: Chat Input Auto-Focus (Flutter)

### Problem
User must click the chat input box before typing. Input should be focused automatically when the chat tab is active.

### Root Cause
`ChatInput` uses a `FocusNode` but only requests focus when `focusInputRequestProvider` is set (triggered by keyboard shortcuts). The chat tab does not auto-focus on activation.

### Solution
Add focus logic to `ChatTab` or `ChatView` that requests focus whenever the tab becomes active.

### Implementation Details
**Files:**
- `ui/flutter_ui/lib/features/chat/chat_tab.dart`
- `ui/flutter_ui/lib/features/chat/chat_input.dart`

**Approach A (Recommended):** Modify `ChatInput` to accept an `autoFocus` parameter and use `FocusScope` to request focus when the tab activates.

```dart
// In ChatInput
@override
void didChangeDependencies() {
  super.didChangeDependencies();
  // Request focus when widget is first built in active tab
  WidgetsBinding.instance.addPostFrameCallback((_) {
    if (!_hasFocused) {
      _focusNode.requestFocus();
      _hasFocused = true;
    }
  });
}
```

**Approach B:** Use `FocusTraversalPolicy` to set the input as the default focus target for the chat tab.

### Success Criteria
- Chat input has focus immediately when chat tab is selected
- User can start typing without clicking
- Focus returns to input after sending a message (optional)

---

## Issue 4: Markdown Rendering (Flutter)

### Problem
Markdown text from the LLM is displayed as raw text with no formatting - no headers, bold, lists, or line breaks. Appears as a massive block of text.

### Root Cause
Two potential issues:
1. Message content from backend may be arriving as a single line (no newlines)
2. `MarkdownBody` style sheet may not have proper paragraph spacing/line breaks

Looking at `chat_message_bubble.dart`, `MarkdownBody` is used with `buildCyberpunkMarkdownStyle`, but the `p` style doesn't specify `height` (line-height) which could cause collapsed lines.

### Solution
1. Verify backend message format - check if newlines are preserved
2. Update `markdown_style.dart` to ensure proper paragraph spacing
3. Ensure `MarkdownBody` has `selectable: true` and proper `syntaxHighlighter`

### Implementation Details
**File:** `ui/flutter_ui/lib/theme/markdown_style.dart`

```dart
p: CyberpunkTypography.bodyMedium.copyWith(
  color: CyberpunkColors.orangeGlow,
  fontSize: 14,
  height: 1.5, // Add line height
),
```

**File:** `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart`

Ensure `MarkdownBody` is configured correctly:
```dart
MarkdownBody(
  data: message.content,
  styleSheet: buildCyberpunkMarkdownStyle(context),
  selectable: true,
  syntaxHighlighter: CyberpunkSyntaxHighlighter(),
  // Add explicit builder for code blocks if needed
  builders: {
    'codeblock': CodeBlockBuilder(),
  },
)
```

### Debugging Steps
1. Print `message.content` to verify it contains newlines
2. Check if backend is sending streaming chunks vs complete messages
3. Verify WebSocket message parsing in `chat_provider.dart`

### Success Criteria
- Headers (`#`, `##`, `###`) render with appropriate styling
- Bold (`**text**`) and italic (`*text*`) render correctly
- Lists show bullets/numbers
- Line breaks are preserved
- Code blocks are visually distinct

---

## Issue 5: Code Block Styling (Flutter)

### Problem
Code blocks should have distinct visual styling (black background, different text color, proper indentation).

### Root Cause
The `codeblockDecoration` exists in `markdown_style.dart` but may not be applied, or the `CyberpunkSyntaxHighlighter` isn't being invoked properly.

### Solution
Enhance code block styling with:
- Darker background (#111827 or darker)
- Monospace font (SourceCodePro)
- Syntax highlighting via `flutter_highlight`
- Proper padding and indentation

### Implementation Details
**File:** `ui/flutter_ui/lib/theme/markdown_style.dart`

```dart
codeblockDecoration: BoxDecoration(
  color: Color(0xFF111827),
  borderRadius: BorderRadius.all(Radius.circular(4)),
  border: Border.all(
    color: CyberpunkColors.midGray,
    width: 1,
  ),
),
code: CyberpunkTypography.bodyMedium.copyWith(
  color: const Color(0xFF10B981),
  backgroundColor: const Color(0xFF1F2937),
  fontFamily: 'SourceCodePro',
  fontSize: 12,
),
```

**File:** `ui/flutter_ui/lib/theme/syntax_highlighter.dart`

Verify `CyberpunkSyntaxHighlighter` supports language detection from markdown code fences.

### Success Criteria
- Code blocks have dark background with border
- Syntax highlighting applied based on language
- Monospace font used for all code
- Indentation preserved

---

## Issue 6: Sessions Not Displaying/Persisting (Flutter)

### Problem
When creating a new session, it appears in chat but doesn't show in the sessions list. Sessions appear temporary and aren't persisted.

### Root Cause
Looking at `sessions_list.dart`:
1. `loadSessions()` is called in `initState` but may not be called after session creation
2. After creating a session via `notifier.createSession()`, the session list isn't refreshed
3. The API may not be returning persisted sessions

### Solution
1. After creating a session, explicitly reload the session list
2. Ensure `SessionNotifier.createSession()` triggers a list refresh
3. Verify backend API persists and returns sessions correctly

### Implementation Details
**File:** `ui/flutter_ui/lib/features/sessions/sessions_list.dart`

```dart
onPressed: () async {
  if (controller.text.isNotEmpty) {
    final session = await notifier.createSession(controller.text);
    if (session != null) {
      // Reload session list after creation
      await notifier.loadSessions();
      if (!context.mounted) return;
      Navigator.pop(context);
      ref.read(activeSessionProvider.notifier).state = session;
      context.go('/');
    }
  }
}
```

**File:** `ui/flutter_ui/lib/providers/session_provider.dart`

Ensure `createSession()` method triggers list reload or that the session list is reactive.

### Debugging Steps
1. Check API response from `POST /api/v1/sessions` - does it return a valid session ID?
2. Verify `GET /api/v1/sessions` returns persisted sessions
3. Check if `activeSessionProvider` is being set correctly

### Success Criteria
- New sessions appear in sessions list immediately after creation
- Sessions persist across app restarts
- Clicking a session in the list switches to it

---

## Issue 7: Token Budget / LLM Error Messages (Flutter)

### Problem
When token limits are exhausted, TUI receives a "non-chat" system message indicating the error. Flutter UI shows only "thinking..." indefinitely.

### Root Cause
In `chat_provider.dart` lines 242-262, error responses are only captured when `chatResp['error']` exists. Token budget errors may be sent via a different mechanism (e.g., WebSocket system messages, not HTTP response errors).

### Solution
1. Handle WebSocket system messages for token budget errors
2. Display "non-chat" messages (budget, system events) in the chat stream
3. Add visual distinction for system messages

### Implementation Details
**File:** `ui/flutter_ui/lib/providers/chat_provider.dart`

```dart
void addStreamMessage(Map<String, dynamic> data) {
  final message = ChatMessage.fromBackendMessage(data);

  // Handle system/non-chat messages (token budget, etc.)
  if (message.role == 'system' || data['type'] == 'non-chat') {
    // Add system message with distinct styling
    state = ChatState(
      messages: [...state.messages, message],
      isLoading: false,
      error: message.content,
    );
    return;
  }

  // ... rest of message handling
}
```

**File:** `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart`

Add styling for system messages:
```dart
if (message.role == 'system') {
  return Container(
    color: CyberpunkColors.redAlert.withValues(alpha: 0.1),
    child: Text(message.content, style: errorStyle),
  );
}
```

### Debugging Steps
1. Log WebSocket messages to see if token budget error is received
2. Check backend message format for "non-chat" messages
3. Verify `ChatMessage.fromBackendMessage()` parses system messages

### Success Criteria
- Token budget errors displayed prominently in chat
- System messages visually distinct from user/assistant messages
- Chat returns to normal state after error

---

## Issue 8: TUI Fuzzy Finder Panic (Ctrl+P)

### Problem
Pressing Ctrl+P causes a panic:
```
runtime error: invalid memory address or nil pointer dereference
github.com/caimlas/meept/internal/tui.(*FuzzyFinderModal).Show(...)
    internal/tui/modal.go:972
```

### Root Cause
Looking at `modal.go` line 972 and `app.go` line 685:
- `a.fuzzyFinder.Show()` is called but `a.fuzzyFinder` may be nil
- The fuzzyFinder is initialized at line 262 in `app.go`, but could be nil if:
  - Initialization failed
  - Modal is accessed before `NewApp()` completes
  - Race condition in tea.Model lifecycle

### Solution
Add nil check before accessing fuzzyFinder in the key handler.

### Implementation Details
**File:** `internal/tui/app.go`

```go
// Check for Ctrl+P to open fuzzy finder
if msg.String() == "ctrl+p" {
    if a.fuzzyFinder == nil {
        a.statusMessage = "fuzzy finder not initialized"
        a.statusMessageTime = time.Now()
        return a, nil
    }
    a.activeModal = ModalFuzzyFinder
    a.fuzzyFinder.Show()
    return a, tea.Batch(a.fuzzyFinder.FetchSessions(), a.fuzzyFinder.FetchTasks())
}
```

Also add nil checks to other fuzzyFinder access points (lines 905, 911, 1679, 1680, 1685, 1699, 1880).

### Success Criteria
- No panic when pressing Ctrl+P
- Either fuzzy finder opens or a graceful error message is shown
- All nil checks prevent crashes

---

## Issue 9: TUI Session Management Consolidation

### Problem
Session management is split between two keybindings:
- `^X S` goes to tabbed sessions view (new)
- `^S` goes to old session picker box (deprecated)

User wants:
- `^X S` should go to the sessions tab where sessions can be created and managed
- `^S` keybinding should be removed (replaced by tabbed view)

### Root Cause
Legacy session picker modal wasn't removed when tabbed sessions view was added.

### Solution
1. Remove or redirect `^S` keybinding to sessions tab
2. Ensure sessions tab has full session management (create, delete, switch)
3. Remove deprecated session picker modal

### Implementation Details
**File:** `internal/tui/app.go`

```go
// Remove or modify the ^S handler
// OLD:
if msg.String() == "ctrl+s" {
    a.activeModal = ModalSessionPicker
    // ...
}

// NEW: Navigate to sessions tab
if msg.String() == "ctrl+s" {
    a.currentView = ViewSessions
    return a, nil
}
```

**File:** `internal/tui/session_picker.go` (if exists)
- Can be removed if no longer needed

### Success Criteria
- `^S` navigates to sessions tab
- `^X S` also navigates to sessions tab (or removed)
- Sessions tab has "Create New Session" button
- Existing sessions are listed and clickable

---

## Testing Plan

### Flutter Tests
1. **Tab transitions**: Record screen, verify no slide animation
2. **Connection popup**: Click status, verify dialog opens and actions work
3. **Auto-focus**: Click between tabs, verify keyboard is ready
4. **Markdown**: Send messages with various markdown, verify rendering
5. **Sessions**: Create session, verify it appears in list

### TUI Tests
1. **Ctrl+P**: Press in various states, verify no crash
2. **Session management**: Create/switch sessions via new flow

---

## Implementation Order

**Priority 1 (Critical Bugs):**
1. Issue 8: TUI Fuzzy Finder Panic
2. Issue 4: Markdown Rendering (usability)

**Priority 2 (Core UX):**
3. Issue 6: Sessions Not Displaying
4. Issue 3: Chat Input Auto-Focus
5. Issue 7: Token Budget Messages

**Priority 3 (Polish):**
6. Issue 1: Tab Transitions
7. Issue 2: Connection Popup
8. Issue 5: Code Block Styling
9. Issue 9: TUI Session Consolidation

---

## Files to Modify

| File | Issues |
|------|--------|
| `ui/flutter_ui/lib/core/router.dart` | 1 |
| `ui/flutter_ui/lib/features/home/home_screen.dart` | 2 |
| `ui/flutter_ui/lib/features/chat/chat_input.dart` | 3 |
| `ui/flutter_ui/lib/features/chat/chat_tab.dart` | 3 |
| `ui/flutter_ui/lib/theme/markdown_style.dart` | 4, 5 |
| `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart` | 4, 5, 7 |
| `ui/flutter_ui/lib/features/sessions/sessions_list.dart` | 6 |
| `ui/flutter_ui/lib/providers/chat_provider.dart` | 7 |
| `ui/flutter_ui/lib/providers/session_provider.dart` | 6 |
| `internal/tui/app.go` | 8, 9 |
| `internal/tui/modal.go` | 8 |
