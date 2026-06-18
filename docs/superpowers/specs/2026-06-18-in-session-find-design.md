# In-Session Find (TUI + Flutter) — Design Spec

**Date:** 2026-06-18
**Status:** Draft (pending user review)
**Goal:** Add a find bar overlay to the chat view in both the TUI and the Flutter GUI, triggered by `ctrl+f` (TUI) or `cmd+f`/`ctrl+f` (Flutter, platform default). The bar sits at the top of the chat viewport, immediately below the session header. Search is live, substring by default with optional case-sensitivity and regex toggles, and navigates the user through all matches in the current session with auto-scroll and inline highlighting.

---

## Context

Neither the TUI nor the Flutter GUI has an in-session text find feature today. Users scrolling long sessions have no way to jump to a phrase the assistant said earlier.

### Existing pieces this spec builds on

| Component | Path | Role |
|---|---|---|
| Chat viewport | `internal/tui/models/chat.go:90` | `viewport.Model` holding rendered messages |
| Message rendering | `internal/tui/models/chat.go:1772` | `updateViewport()` builds content string with per-message styling |
| Message list accessor | `internal/tui/models/chat.go:2825` | `GetMessages() []ChatMessage` |
| TUI key dispatch | `internal/tui/app.go:680` | Global key handling before focus delegation |
| TUI modal pattern | `internal/tui/modal.go` | Full-screen modal pattern (not used here — find bar is an inline overlay) |
| Flutter chat view | `ui/flutter_ui/lib/features/chat/chat_view.dart` | `Column` with header, `ChatMessageList`, `ChatInput` |
| Flutter message list | `ui/flutter_ui/lib/features/chat/chat_message_list.dart` | `ListView.builder` with `ScrollController` |
| Flutter shortcuts | `ui/flutter_ui/lib/core/shortcuts.dart:49` | `FindIntent` class exists but no key binding |
| Flutter focus pattern | `ui/flutter_ui/lib/core/shortcuts.dart:103` | `Cmd+K` / `Ctrl+K` focuses chat input — same pattern |

---

## Requirements

1. **Trigger**: `ctrl+f` in TUI; `cmd+f` on macOS Flutter, `ctrl+f` elsewhere — uses the OS default find shortcut.
2. **Placement**: find bar appears at the top of the chat viewport, immediately below the session header bar (not centered, not full screen).
3. **Live search**: as the user types, matches update in real time.
4. **Navigation**: `enter`/`down` next match; `shift+enter`/`up` previous match; `esc` closes the bar and clears highlights.
5. **Match indicator**: `current/total` count visible next to the input (e.g., `3/17`).
6. **Highlighting**: all matches visually marked; the current match is emphasized distinctly.
7. **Scroll behavior**: auto-scroll to bring the current match into view, centered when possible.
8. **Toggles**: case sensitivity (`alt+c` TUI / checkbox Flutter) and regex mode (`alt+r` TUI / checkbox Flutter).
9. **Scope**: current session only. No cross-session search.
10. **No backend changes**. Pure client-side feature.

---

## Section 1: TUI Implementation

### Find bar state in `ChatModel`

New fields on `ChatModel` (`internal/tui/models/chat.go`):

```go
type ChatModel struct {
    // ...existing fields...

    // In-session find bar
    findBarVisible   bool
    findInput        textinput.Model  // bubbles/v2 textinput
    findMatches      []findMatch      // all matches in the current viewport
    findCursor       int              // index into findMatches, -1 if none
    findCaseSensitive bool
    findRegex         bool
}

// findMatch points to a span within a rendered message.
type findMatch struct {
    messageIdx int    // index into m.messages
    charStart  int    // byte offset in m.messages[messageIdx].Content
    charEnd    int    // exclusive end
}
```

### Keybinding wiring

In `ChatModel.Update` (currently around `chat.go:869`), add at the top of the `tea.KeyPressMsg` switch:

```go
case "ctrl+f":
    if !m.findBarVisible {
        m.openFindBar()
        return m, nil
    }
    // Already open: focus input, clear current query
    m.findInput.Focus()
    m.findInput.SetValue("")
    m.recomputeFindMatches()
    return m, nil
```

When `m.findBarVisible` is true, key handling is split:
- If `m.findInput.Focused()`: `enter` → next match + blur; `shift+enter` → previous; `esc` → close; `up`/`down` → next/prev; printable chars → text input; `alt+c` → toggle case; `alt+r` → toggle regex.
- Otherwise (bar visible but input blurred, e.g., user clicked into viewport): existing chat keybindings apply, except `ctrl+f` focuses the input again.

### Find bar rendering

In `ChatModel.View()`, the current layout is roughly:

```
[viewport border with content]
[hint line]
```

When `findBarVisible`, insert the find bar above the viewport:

```
[find bar: input field | N/M | Aa | .* | ×]
[viewport border with content + match highlights]
[hint line]
```

The find bar uses `lipgloss.JoinHorizontal` to compose the input, match count, toggle indicators, and close hint, styled to match the existing orange theme (`#F97316` for accents).

### Match highlighting in the viewport

`updateViewport()` at `chat.go:1772` builds the content string. We wrap match spans with ANSI background colors:

- Non-current matches: subtle background (e.g., `#3B3F45` on default text).
- Current match: bright background (e.g., `#F97316` with black text).

This requires recomputing match offsets against the **rendered** content (with role prefixes, timestamps, and separators applied), not the raw `Content`. Implementation approach:

1. After building the rendered string in `updateViewport()`, iterate `m.findMatches`.
2. For each match, locate the corresponding position in the rendered string. Since matches are indexed by `messageIdx` + byte range in `Content`, and we build the rendered string deterministically by iterating messages, we can compute rendered-string offsets by tracking a running byte counter as we emit each message.
3. Wrap the matched span with a lipgloss style using `lipgloss.NewStyle().Background(...).Render(substring)`.
4. After highlighting, call `m.viewport.SetContent(highlightedString)`.

### Auto-scroll on match change

When `findCursor` changes (next/prev pressed), scroll the viewport so the current match is visible:

- Compute the rendered line of the current match (by counting newlines from the start of the rendered string up to the match's rendered offset).
- Call `m.viewport.SetYOffset(matchLine - viewportHeight/2)` to center the match.
- Clamp to `[0, viewport.TotalLineCount()]`.

### Match recomputation

`recomputeFindMatches()` runs on every keystroke in `findInput`. Algorithm:

1. Read `m.findInput.Value()`.
2. If empty, clear matches and return.
3. For case-insensitive mode (default), lowercase both query and content.
4. For regex mode, compile the query with `regexp.Compile`; on compile error, mark `findRegexError` and show in the bar.
5. Iterate `m.messages`, scan each `Content` for matches, append to `m.findMatches`.
6. Set `findCursor` to 0 if matches exist, else -1.
7. Trigger `updateViewport()` to re-apply highlighting.

### Performance

For very long sessions (>500 messages), live re-search on every keystroke could lag. Mitigation:

- Debounce match recomputation by 50ms using a `tea.Cmd` that returns after a timer (pattern already used in `internal/tui/handlers/task_events.go`).
- Cap matches at 1000 to avoid pathological regex backtracking. Display `1000+` in the count when hit.

---

## Section 2: Flutter Implementation

### Find bar widget

New file `ui/flutter_ui/lib/features/chat/find_bar.dart`:

```dart
class FindBar extends ConsumerStatefulWidget {
  final String sessionId;
  const FindBar({super.key, required this.sessionId});
  @override
  ConsumerState<FindBar> createState() => _FindBarState();
}
```

Renders a compact horizontal bar:
- `TextField` for the query (autofocus, no decoration chrome).
- `Text` widget showing `current/total`.
- Two `IconButton`s or checkboxes: case-sensitive toggle, regex toggle.
- A close `IconButton` with the `×` icon.

Styled with the existing app theme. Positioned at the top of the chat content area via the parent `Stack` (see below).

### Placement in `ChatView`

`chat_view.dart` currently builds:

```dart
Column(
  children: [
    HeaderBar(...),
    Expanded(child: ChatMessageList(...)),
    ChatInput(...),
  ],
)
```

Wrap the `Expanded` area in a `Stack` and overlay the `FindBar` at the top:

```dart
Expanded(
  child: Stack(
    children: [
      ChatMessageList(...),
      Positioned(
        top: 0, left: 0, right: 0,
        child: AnimatedSwitcher(
          duration: Duration(milliseconds: 150),
          child: ref.watch(findBarVisibleProvider(sessionId))
              ? FindBar(sessionId: sessionId)
              : const SizedBox.shrink(),
        ),
      ),
    ],
  ),
)
```

The `AnimatedSwitcher` gives a smooth slide-in/out. The bar sits visually below the header bar because the `Stack` is inside the `Expanded` below the `HeaderBar` in the column.

### State: Riverpod providers

New providers in a new file `ui/flutter_ui/lib/features/chat/find_state.dart`:

```dart
final findBarVisibleProvider = StateProvider.family<bool, String>((ref, sessionId) => false);

final findQueryProvider = StateProvider.family<String, String>((ref, sessionId) => '');

final findMatchesProvider = Provider.family<List<MessageMatch>, String>((ref, sessionId) {
  final query = ref.watch(findQueryProvider(sessionId));
  final messages = ref.watch(chatMessagesProvider(sessionId));
  // Compute matches...
});

final findCursorProvider = StateProvider.family<int, String>((ref, sessionId) => 0);
```

The `chatMessagesProvider` is the existing message list already used by `ChatMessageList`. Match computation is synchronous over the in-memory message list (typically small).

### Shortcut wiring

In `ui/flutter_ui/lib/core/shortcuts.dart`, add the key binding for `FindIntent`:

```dart
// In the Shortcuts.shortcuts map:
LogicalKeySet(LogicalKeyboardKey.keyF, meta): FindIntent(),     // macOS: Cmd+F
LogicalKeySet(LogicalKeyboardKey.keyF, control): FindIntent(),  // other: Ctrl+F
```

The `FindIntent` action handler (already exists) should:
1. Get the current session ID from `currentSessionProvider`.
2. Toggle `findBarVisibleProvider(sessionId)` to true.
3. Auto-focus the `TextField` in `FindBar` (via a `FocusNode` and `autofocus: true`).

### Match highlighting in the message list

`ChatMessageBubble` renders each message. When `findQueryProvider` is non-empty:

1. Wrap the message text widget with a `RichText` / `Text.ron` that splits the content into matched and non-matched spans.
2. Matched spans get a background color (`Colors.orange.withOpacity(0.3)` for non-current, `Colors.orange` for the current match).
3. The `findCursorProvider` tracks which global match is current — the bubble checks if any of its matches equal the current index.

### Scroll-to-match

When `findCursorProvider` changes, the `ChatMessageList` uses a `GlobalKey` per message and `ScrollController.position.ensureVisible` with alignment `center` to scroll the matched message into view.

Alternatively, use `ScrollController.animateTo` with a computed offset if `ensureVisible` is awkward with `ListView.builder`.

---

## Section 3: Shared behavior contract

Both TUI and Flutter implement identical semantics:

| Action | TUI | Flutter |
|---|---|---|
| Open find bar | `ctrl+f` | `cmd+f` (macOS), `ctrl+f` (other) |
| Close find bar | `esc` | `esc` or close button |
| Next match | `enter`, `down` | `enter`, `down` arrow |
| Previous match | `shift+enter`, `up` | `shift+enter`, `up` arrow |
| Toggle case | `alt+c` | case toggle in bar |
| Toggle regex | `alt+r` | regex toggle in bar |
| Clear query | (just type) | (just type) |

**Edge cases (both UIs):**
- **No matches**: show `0/0`, no highlights, no scroll.
- **Empty query**: clear all matches and highlights, count hidden.
- **Regex compile error**: show error indicator, no matches applied.
- **Session changes**: close find bar and clear state.
- **New message arrives while bar open**: recompute matches; preserve cursor if still valid, else clamp.

---

## Section 4: Testing

### TUI tests

New file `internal/tui/models/chat_find_test.go`:

- `TestOpenFindBar` — `ctrl+f` toggles `findBarVisible`.
- `TestFindBarNavigation` — next/prev move `findCursor` correctly, wrap or clamp at ends.
- `TestFindBarCaseSensitivity` — default is case-insensitive; toggling changes match count.
- `TestFindBarRegex` — valid regex matches; invalid regex sets error state.
- `TestFindBarEscCloses` — `esc` closes bar and clears matches + highlights.
- `TestRecomputeMatches` — matches recompute on input change.
- `TestFindBarSessionChange` — session switch closes bar.

### Flutter tests

New file `test/features/chat/find_bar_test.dart`:

- `FindBar` renders when `findBarVisibleProvider` is true.
- Typing updates `findQueryProvider`.
- Toggling case/regex updates match computation.
- Close button sets `findBarVisibleProvider` to false.
- `Cmd+F` / `Ctrl+F` shortcut toggles visibility.

New file `test/features/chat/chat_message_list_find_test.dart`:

- Highlights appear on matched spans when query is set.
- `findCursorProvider` changes scroll the list.

---

## Section 5: Out of scope

- **Cross-session search** — handled by Spec B (global semantic search).
- **Search history persistence** — YAGNI; queries live only for the session.
- **Find-and-replace** — not requested.
- **Search in non-chat views** (sessions list, tasks, plans) — only chat is in scope.
- **Mobile-specific find gestures** — desktop-only for now; mobile find would need a separate UX.
