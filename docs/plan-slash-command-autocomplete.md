# Plan: Slash Command Type-Ahead Autocomplete

**Status:** Complete
**Date:** 2026-04-21
**Updated:** 2026-04-21
**Last Verified:** Build and TUI tests pass

## Problem

Previously:
1. Slash commands (`/help`, `/clear`, etc.) only executed on Enter, with no type-ahead popup
2. When typing `/` at the start of input, no autocomplete popup appeared
3. Command output (like `/help`) printed to the status bar area instead of the chat transcript
4. The "available commands:" message appeared beneath the shortcut info bar, not in the chat

## Requirements

1. **Type-ahead popup** when typing `/` as the first character in the input
2. **Filter commands** as user types (e.g., `/he` shows only `/help`)
3. **Navigate suggestions** with arrow keys or Ctrl+J/Ctrl+K
4. **Select with Enter** to insert the command
5. **Command output in chat** - Results should appear as system messages in the chat viewport, not the status bar

## Implementation

### New Component: SlashAutocomplete

Created `internal/tui/slash_autocomplete.go` - a type-ahead autocomplete popup for slash commands.

Features:
- Shows when `/` is typed at the start of input
- Filters commands as user types
- Supports navigation with arrow keys, Ctrl+J/Ctrl+K, and Tab
- Closes on Escape or when command is selected
- Highlights matched portion of command names

### Files Modified

| File | Changes |
|------|---------|
| `internal/tui/slash_autocomplete.go` | New file - SlashAutocomplete component |
| `internal/tui/app.go` | Added `slashAutocomplete` field, initialization, key handling, rendering |
| `internal/tui/models/chat.go` | Added `AddSystemMessage()` method |
| `internal/tui/app.go` | Modified `CommandResultMsg` handler to use chat messages |

### Implementation Details

#### SlashAutocomplete Component

```go
type SlashAutocomplete struct {
    visible    bool
    commands   []string // All available commands
    filtered   []string // Commands matching current filter
    selected   int      // Currently selected index
    filter     string   // Current filter text (what user typed after /)
    maxHeight  int      // Maximum visible items before scrolling
    styles     *Styles
}
```

#### Key Handling in app.go

When in chat view with focus on input:
1. `/` typed with empty input → Show autocomplete with empty filter
2. Typing after `/` → Update filter and recompute matches
3. Arrow keys/Tab → Navigate suggestions
4. Enter → Insert selected command
5. Escape → Close popup

#### Command Output Routing

Changed from status bar to chat transcript:

```go
// Before (status bar):
a.statusMessage = msg.Result.Output

// After (chat system message):
a.chat.AddSystemMessage(msg.Result.Output)
```

## Verification Checklist

- [x] Build succeeds
- [x] Typing `/` shows autocomplete popup
- [x] Typing `/he` filters to `/help`
- [x] Arrow keys navigate suggestions
- [x] Ctrl+J/Ctrl+K navigate suggestions
- [x] Enter inserts selected command
- [x] Escape closes popup
- [x] `/help` output appears in chat transcript
- [x] `/status` output appears in chat transcript
- [x] Tab cycles viewports when not in slash command context
- [x] Popup doesn't interfere with normal typing
- [x] Popup positioned above input textarea (rendered by ChatModel)
- [x] Matched portion of commands highlighted in orange (#F97316)
- [x] `/` character in input textarea styled orange

## Implementation Notes

### Orange Styling

1. **HelpKey style updated** (`internal/tui/styles.go:207-210`):
   - Changed from `ColorMuted` (gray) to `ColorPrimary` (orange #F97316)
   - Affects: command popup matches, keybinding hints, slash prefix

2. **Input slash styling** (`internal/tui/models/chat.go:renderInputWithStyles`):
   - Custom renderer checks if input starts with `/`
   - Renders first character with orange foreground style
   - Falls back to default textarea rendering for non-slash input

3. **Popup command matching** (`internal/tui/app.go:generateAutocompletePopup`):
   - Uses `a.styles.HelpKey.Render()` for matched portion
   - Orange highlighting on filter match (e.g., `/he` in `/help`)

### Files Modified

| File | Changes |
|------|---------|
| `internal/tui/styles.go:207-210` | HelpKey style: orange (ColorPrimary) |
| `internal/tui/models/chat.go` | Added `renderInputWithStyles()` method |
| `internal/tui/models/chat.go` | Added `AddSystemMessage()` method |
| `internal/tui/app.go` | Nil checks for slashAutocomplete in tests |

## Known Issues / TODO

1. **Filter updates**: Filter only updates on keypress, may not handle paste correctly
2. **Skill commands**: Currently only shows built-in commands, not installed skills
