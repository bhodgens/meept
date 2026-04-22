# Plan: Slash Command Completion Without Popup

**Status:** Research
**Date:** 2026-04-22

---

## Problem

Provide visible typeahead/autocomplete for slash commands (`/help`, `/clear`, etc.) WITHOUT using a popup menu, due to terminal UI overlay limitations.

---

## Alternative Patterns

### Pattern 1: Inline Ghost Completion (Fish Shell Style)

Show the completion inline with the input, ghosted/faded, accepted by Tab or Right Arrow.

**Visual:**
```
> /he│lp          ← "lp" is ghosted/faded
```

**Behavior:**
- User types `/he`
- Best matching command (`/help`) appears ghosted after cursor
- Press Tab or → to accept ghosted text
- Press Esc to dismiss suggestion

**Implementation:**
```go
// In ChatModel.RenderInput()
input := m.textarea.Value()
ghost := findBestMatch(input)
if ghost != "" {
    styledInput := defaultStyle.Render(input) +
                   ghostStyle.Render(ghost[len(input):])
    return styledInput
}
```

**Pros:**
- No layout shift
- Immediate visual feedback
- Feels responsive
- Single-line, no popup needed

**Cons:**
- Only shows ONE suggestion (best match)
- User doesn't see all options
- Requires ghost text styling support

**Implementation Location:** `internal/tui/models/chat.go:renderInputWithStyles()`

---

### Pattern 2: Status Bar Completions

Show available completions in the status bar at the bottom of the screen.

**Visual:**
```
> /he│
─────────────────────────────────────────
help | hello | hey    ← status bar shows matches
```

**Behavior:**
- User types `/he`
- Matching commands appear in status bar
- Arrow keys cycle through matches
- Enter accepts current selection

**Implementation:**
```go
// In App.renderStatusBar()
if a.slashAutocomplete.IsVisible() {
    commands := a.slashAutocomplete.GetFilteredCommands()
    selected := commands[a.slashAutocomplete.GetSelectedIndex()]
    statusContent = strings.Join(commands, " | ") +
                    " [↑↓ select, Enter accept]"
}
```

**Pros:**
- No layout shift
- Shows ALL matching commands
- Familiar pattern (like browser status bar link preview)

**Cons:**
- Status bar may be far from input (eye travel)
- Status bar is typically 1 line (limited space)
- Competes with other status messages

**Implementation Location:** `internal/tui/app.go:renderStatusBar()`

---

### Pattern 3: Expandable Input Hints

Show completions as tags/chips ABOVE the input line, flowing upward.

**Visual:**
```
[help] [hello] [hey]  ← completion chips
> /he│                 ← input
```

**Behavior:**
- User types `/he`
- Matching commands appear as styled chips above input
- Chips are selectable (tab/arrow to cycle)
- Selected chip is highlighted

**Implementation:**
```go
// In ChatModel.View()
if m.slashAutocomplete.IsVisible() {
    commands := m.slashAutocomplete.GetFilteredCommands()
    for i, cmd := range commands {
        style := chipStyle
        if i == selectedIndex {
            style = selectedChipStyle
        }
        b.WriteString(style.Render("[" + cmd + "]"))
        b.WriteString(" ")
    }
    b.WriteString("\n")
}
// Then render input...
```

**Pros:**
- Near input (minimal eye travel)
- Shows multiple options
- Feels like natural input extension

**Cons:**
- Takes vertical space (pushes viewport up)
- Limited by horizontal space (wraps awkwardly)
- Complex selection rendering

**Implementation Location:** `internal/tui/models/chat.go:View()` (before input rendering)

---

### Pattern 4: Tab-Cycling Completion

Press Tab to cycle through completions inline.

**Visual:**
```
> /help│   ← first Tab
> /hello│  ← second Tab
> /hey│    ← third Tab
```

**Behavior:**
- User types `/he` + Tab
- First matching command inserted
- Press Tab again to cycle to next match
- Press Enter to accept, Esc to cancel

**Implementation:**
```go
// In App.Update() for Tab key
if strings.HasPrefix(input, "/") && hasMatches(input) {
    m.tabCycleIndex++
    if m.tabCycleIndex >= len(matches) {
        m.tabCycleIndex = 0
    }
    m.textarea.SetValue("/" + matches[m.tabCycleIndex])
}
```

**Pros:**
- Minimal UI change
- Familiar pattern (like shell tab completion)
- No extra rendering

**Cons:**
- User doesn't see all options at once
- Requires discovering the cycling behavior
- Can be disorienting if many matches

**Implementation Location:** `internal/tui/app.go:Update()` (Tab key handler)

---

### Pattern 5: Command Palette Style

Show completions in a centered modal overlay (like VS Code's Ctrl+P).

**Visual:**
```
                    ┌─────────────────┐
                    │ Type command... │
                    │ > /help         │
                    │   /hello        │
                    │   /hey          │
                    └─────────────────┘
```

**Behavior:**
- User types `/` or Ctrl+P
- Centered palette appears with all commands
- Type to filter, arrows to navigate
- Enter to select

**Note:** This IS a popup, but uses terminal-native centering rather than cursor alignment.

**Pros:**
- Shows all commands
- Clear focus state
- Familiar to VS Code users

**Cons:**
- Covers screen content
- Requires modal state management
- Still subject to terminal overlay limitations

**Implementation:** Would require new modal component similar to existing session picker.

---

### Pattern 6: Prefix Completion Chips

As user types, show potential completions as faded prefix bubbles.

**Visual:**
```
> /help ─┐
         ├─ or: /hello, /hey, /status
```

**Behavior:**
- After user stops typing (~300ms)
- Show "or:" hint with other matches
- Tab accepts first alternative

**Pros:**
- Non-intrusive
- Shows alternatives
- Feels helpful, not pushy

**Cons:**
- Delayed feedback (timing-based)
- Less discoverable

---

## Recommendations

### Best Overall: **Pattern 1 + Pattern 2 (Inline Ghost + Status Bar)**

Combine inline ghost completion (for immediate feedback) with status bar matches (for full options).

**Why:**
- Ghost text provides instant "this will complete" feedback
- Status bar shows all options without popup
- Both work within Bubble Tea's model
- Minimal code changes

**Implementation Priority:**
1. Add ghost completion to `renderInputWithStyles()`
2. Add match list to status bar when autocomplete active

---

### Best Single Pattern: **Pattern 1 (Inline Ghost)**

If implementing one pattern, inline ghost completion provides the best UX for the least complexity.

---

### Most Familiar: **Pattern 4 (Tab-Cycling)**

Shell users expect Tab completion. This is the most "terminal-native" pattern.

---

## Implementation Complexity

| Pattern | Complexity | Files to Modify | Estimated Time |
|---------|------------|-----------------|----------------|
| 1. Inline Ghost | Low | chat.go | 2-4 hours |
| 2. Status Bar | Low | app.go | 1-2 hours |
| 3. Input Hints | Medium | chat.go | 4-6 hours |
| 4. Tab-Cycling | Low | app.go | 2-3 hours |
| 5. Command Palette | High | new component | 8-16 hours |
| 6. Prefix Chips | Medium | chat.go | 3-5 hours |

---

## Next Steps

1. **Decide on pattern(s)** based on UX priorities
2. **Implement chosen pattern(s)** in priority order
3. **Test with users** to validate discoverability
4. **Iterate** based on feedback

---

## Related

- `docs/plan-meept-slashcommands-options.md` - Popup vs. non-popup analysis
- `docs/plan-slash-command-autocomplete.md` - Original popup implementation plan
