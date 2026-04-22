# Plan: Custom Lightweight Mouse Selection Highlighter for Chat Viewport

**Status:** Implemented
**Date:** 2026-04-20

## Overview

Implement mouse text selection with visual feedback in the Meept TUI chat viewport, preserving ANSI formatting from markdown rendering and syntax highlighting.

## Requirements

1. **Mouse wheel scrolling** - 5 lines per event (configurable)
2. **Mouse text selection with visual feedback:**
   - Single click: place cursor
   - Double-click: select word under cursor
   - Triple-click: select entire line
   - Drag: arbitrary selection
3. **Selection confined to viewport area** - Not affecting sidebar or other UI elements
4. **Preserve text formatting** - Don't strip ANSI codes when highlighting
5. **Manual copy** - User uses cmd-c or 'c' key, no auto-copy on release

## Implementation Status

### Completed Steps

1. **Mouse Mode Configuration** (`internal/tui/app.go:909`)
   - `MouseModeAllMotion` captures all mouse events

2. **Mouse Event Handling** (`internal/tui/models/chat.go:663-716`)
   - Wheel scrolling, click, drag, release handlers

3. **Coordinate Calculation** (`internal/tui/models/chat_selection.go:87-111`)
   - Converts viewport Y,X to character offsets

4. **Selection Highlighting** (`internal/tui/models/chat_selection.go:258-314`)
   - Uses `\033[7m` (reverse video) for highlighting
   - Works on stripped content, applies uniform highlighting

5. **View() Integration** (`internal/tui/models/chat.go:1586-1590`)
   - Applies highlighting when selection active

6. **Auto-Copy Disabled** (`config/client.json5:60`)
   - `auto_copy_on_release: false`

7. **Copy Key Bindings**
   - 'c' key copies selection
   - Ctrl+C copies selection when viewport focused

8. **Copy Hint Overlay** (`internal/tui/models/chat.go:1595-1603`)
   - Orange "press 'c' to copy" hint

## Key Files

| File | Purpose |
|------|---------|
| `internal/tui/app.go:909` | Mouse mode configuration |
| `internal/tui/models/chat.go:663-716` | Mouse event routing |
| `internal/tui/models/chat.go:1582-1603` | View rendering with highlighting |
| `internal/tui/models/chat.go:1841-1926` | Mouse handlers |
| `internal/tui/models/chat_selection.go` | Selection utilities |
| `config/client.json5:57-64` | Chat configuration |

## Verification Checklist

- [x] Mouse wheel scrolls viewport (not terminal buffer)
- [x] Scroll speed configurable via `scroll_speed`
- [x] Single click places cursor
- [x] Double-click selects word
- [x] Triple-click selects line
- [x] Drag extends selection
- [x] Selection visually highlighted (reverse video)
- [x] Selection confined to viewport
- [x] 'c' key copies selection
- [x] Ctrl+C copies selection when viewport focused
- [x] Copy hint overlay appears during selection
- [x] No auto-copy on release (config: `false`)
- [x] Sidebar clicks don't trigger viewport selection
- [x] Formatting preserved outside selection region

## Build & Test

```bash
# Build
go build -o bin/meept ./cmd/meept
go build -o bin/meept-daemon ./cmd/meept-daemon

# Test
go test ./internal/tui/... -v
```

Build status: **Success**
Test status: **Pass** (TUI and models tests)

## Architecture Notes

### Mouse Coordinate System

Viewport positioned after:
- Header bar: 1 line
- Header newline: 1 line
- Viewport top border: 1 line

Coordinate adjustment:
```go
adjustedY := mouse.Y - 2  // Account for header
adjustedX := mouse.X - 1  // Account for left border
```

### Selection Highlighting Approach

Uses simpler approach (stripped content with uniform highlighting):
1. Strip ANSI codes for position calculation
2. Calculate overlapping lines
3. Insert highlight style (`\033[7m`) before selection, reset after

 Loses existing styling within selected region but preserves all formatting outside selection.

### Double-Click Detection

400ms timeout, same Y coordinate:
```go
if now.Sub(m.lastClickTime) < 400*time.Millisecond && m.lastClickY == mouse.Y {
    m.clickCount++
}
```

## Related

- Previous plan: `docs/plans/archive/plan-mouse-scrolling-and-selection.md`
- Bubbletea v2 mouse handling: Uses `msg.Mouse()` to access mouse data
