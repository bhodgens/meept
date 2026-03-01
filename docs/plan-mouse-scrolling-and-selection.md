# Plan: Mouse Scrolling and Text Selection for Meept TUI

**Status:** Planning Phase
**Created:** 2025-03-01
**Updated:** 2025-03-01
**Based On:** Analysis of Charmbracelet Crush's mouse selection implementation

## Overview

This plan covers adding mouse text selection to Meept's TUI, borrowing implementation patterns from [Charmbracelet Crush](https://github.com/charmbracelet/crush). The goal is to enable users to select text with mouse drag and copy it to clipboard, similar to how Crush works.

## Current State

**Meept already has:**
- ✅ `tea.EnterAltScreen` enabled (`internal/tui/app.go:190`)
- ✅ `tea.EnableMouseCellMotion()` for mouse events (`internal/tui/app.go:192`)
- ✅ Mouse wheel scrolling in viewport (3 lines per scroll, `internal/tui/models/chat.go:591-601`)
- ✅ Ctrl+M toggle for mouse mode vs native selection (`internal/tui/app.go:359-379`)
- ✅ Click-to-focus for viewport/sidebar/input (`internal/tui/app.go:1239-1259`)
- ✅ **OSC52 clipboard support** (`internal/tui/app.go:1163-1190`)
- ✅ Platform clipboard fallbacks (pbcopy, xclip, xsel)

**What needs to be added:**
- ❌ Mouse drag-to-select text (with visual tracking)
- ❌ Visual highlight of selected text (reverse video)
- ❌ Double-click word selection
- ❌ Triple-click line selection
- ❌ Auto-copy on mouse release
- ❌ Selection state management in ChatModel

## Architecture Decision: Viewport vs List

**Current Meept:** Uses `bubbles/viewport` with rendered chat content
**Crush approach:** Uses custom list component with individual highlightable items

### Two Implementation Options

| Option | Description | Complexity | Compatibility |
|--------|-------------|------------|----------------|
| **A: Keep viewport** | Add selection highlighting to viewport content | High | Custom rendering |
| **B: Switch to list** | Convert to list of highlightable items (like Crush) | Medium | Better pattern match |

**Recommendation:** **Option A (Keep viewport)** - Converting to a list-based architecture would be a major rewrite. Instead, we'll add selection highlighting to the viewport-based approach.

## Implementation Plan

### Phase 1: Mouse Selection State Tracking

**File:** `internal/tui/models/chat.go`

Add mouse state fields to `ChatModel`:

```go
type ChatModel struct {
    // ... existing fields ...

    // Mouse selection state
    mouseDown      bool
    mouseDownY     int  // Viewport Y where mouse was pressed
    mouseDragY     int  // Current viewport Y during drag
    selectionStart int  // Character offset in viewport content
    selectionEnd   int  // Character offset in viewport content
    isSelecting    bool

    // Click tracking for double/triple click
    lastClickTime time.Time
    lastClickY    int
    clickCount    int
}
```

### Phase 2: Coordinate Mapping

**Challenge:** Viewport doesn't provide item-at-coordinate mapping like Crush's list.

**Solution:** Since chat content is pre-rendered into viewport, we need to:

1. Track message positions in the rendered content
2. Map viewport Y coordinate back to message and line
3. Calculate character offsets within lines

**New file:** `internal/tui/models/chat_selection.go`

```go
// MessagePosition tracks where a message appears in rendered content
type MessagePosition struct {
    MsgIdx      int
    LineStart   int  // Line number in rendered content
    LineCount   int  // Number of lines this message spans
    ContentStart int // Character offset in viewport content
}

// Build a position index for the rendered content
func (m *ChatModel) buildPositionIndex() []MessagePosition

// Find message at given viewport Y coordinate
func (m *ChatModel) messageAtY(y int) (msgIdx int, lineInMsg int, charOffset int)
```

### Phase 3: Mouse Event Handling

**File:** `internal/tui/models/chat.go`

Update the mouse handler to track drag:

```go
func (m *ChatModel) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
    // Existing wheel scrolling
    if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
        // ... existing code ...
    }

    // NEW: Left click handling for selection
    if msg.Button == tea.MouseButtonLeft {
        switch msg.Action {
        case tea.MouseActionPress:
            return m.handleMousePress(msg)
        case tea.MouseActionRelease:
            return m.handleMouseRelease(msg)
        case tea.MouseActionMotion:
            if m.mouseDown {
                return m.handleMouseDrag(msg)
            }
        }
    }

    return nil
}

func (m *ChatModel) handleMousePress(msg tea.MouseMsg) tea.Cmd {
    m.mouseDown = true
    m.mouseDownY = msg.Y

    // Check for double/triple click
    now := time.Now()
    if now.Sub(m.lastClickTime) < 400*time.Millisecond {
        m.clickCount++
        if m.clickCount == 2 {
            return m.selectWordAt(msg.Y, msg.X)
        } else if m.clickCount >= 3 {
            return m.selectLineAt(msg.Y)
        }
    } else {
        m.clickCount = 1
    }
    m.lastClickTime = now
    m.lastClickY = msg.Y

    // Start selection at cursor position
    m.selectionStart = m.calculateCursorOffset(msg.Y, msg.X)
    m.selectionEnd = m.selectionStart
    m.isSelecting = true
    return nil
}

func (m *ChatModel) handleMouseDrag(msg tea.MouseMsg) tea.Cmd {
    m.mouseDragY = msg.Y
    m.selectionEnd = m.calculateCursorOffset(msg.Y, msg.X)
    return nil
}

func (m *ChatModel) handleMouseRelease(msg tea.MouseMsg) tea.Cmd {
    m.mouseDown = false

    if m.isSelecting && m.selectionStart != m.selectionEnd {
        // Copy selected text to clipboard
        selectedText := m.extractSelectedText()
        if selectedText != "" {
            m.isSelecting = false
            return tea.Sequence(
                tea.SetClipboard(selectedText),
                m.notifyCopied(),
            )
        }
    }

    m.isSelecting = false
    return nil
}
```

### Phase 4: Selection Highlight Rendering

**File:** `internal/tui/models/chat.go`

Modify the View method to render selection highlight:

```go
func (m ChatModel) View() string {
    content := m.viewport.View()

    if m.isSelecting || m.selectionStart != m.selectionEnd {
        content = m.applySelectionHighlight(content)
    }

    return m.styles.ChatViewport.Width(m.viewport.Width).Render(content)
}

func (m *ChatModel) applySelectionHighlight(content string) string {
    start, end := m.selectionStart, m.selectionEnd
    if start > end {
        start, end = end, start
    }

    // Split content into lines
    lines := strings.Split(content, "\n")

    // Find line positions in content
    linePositions := m.calculateLinePositions(lines)

    // Apply reverse style to selected region
    for i, line := range lines {
        lineStart := linePositions[i]
        lineEnd := linePositions[i+1]

        // Check if selection overlaps this line
        if end > lineStart && start < lineEnd {
            // Calculate highlight region for this line
            highlightStart := max(0, start-lineStart)
            highlightEnd := min(len(line), end-lineStart)

            if highlightStart < highlightEnd {
                // Apply reverse style to highlighted portion
                before := line[:highlightStart]
                highlighted := m.styles.TextSelection.Render(line[highlightStart:highlightEnd])
                after := line[highlightEnd:]
                lines[i] = before + highlighted + after
            }
        }
    }

    return strings.Join(lines, "\n")
}
```

### Phase 5: Text Extraction

**File:** `internal/tui/models/chat.go`

```go
func (m *ChatModel) extractSelectedText() string {
    start, end := m.selectionStart, m.selectionEnd
    if start > end {
        start, end = end, start
    }

    // Get viewport content (stripped of ANSI codes)
    content := ansi.Strip(m.viewport.View())

    if start >= len(content) || end > len(content) {
        return ""
    }

    // Extract selection
    return strings.TrimSpace(content[start:end])
}

func (m *ChatModel) calculateCursorOffset(y, x int) int {
    // Convert viewport Y,X to character offset in rendered content
    // This accounts for line wrapping and viewport scrolling
    // See Crush's ItemIndexAtPosition for reference
    return m.calculateOffset(y, x)
}
```

### Phase 6: Word and Line Selection

**File:** `internal/tui/models/chat.go`

```go
func (m *ChatModel) selectWordAt(y, x int) tea.Cmd {
    offset := m.calculateCursorOffset(y, x)
    content := ansi.Strip(m.viewport.View())

    // Find word boundaries at offset
    wordStart, wordEnd := findWordBoundaries(content, offset)

    m.selectionStart = wordStart
    m.selectionEnd = wordEnd
    m.isSelecting = true
    return nil
}

func (m *ChatModel) selectLineAt(y int) tea.Cmd {
    // Select entire line at viewport Y
    lineIndex := m.viewport.ScrollY() + y
    content := ansi.Strip(m.viewport.View())
    lines := strings.Split(content, "\n")

    if lineIndex >= 0 && lineIndex < len(lines) {
        // Calculate character offsets for this line
        lineStart := 0
        for i := 0; i < lineIndex; i++ {
            lineStart += len(lines[i]) + 1 // +1 for newline
        }
        lineEnd := lineStart + len(lines[lineIndex])

        m.selectionStart = lineStart
        m.selectionEnd = lineEnd
        m.isSelecting = true
    }
    return nil
}

// Find word boundaries using word segmentation
// See: github.com/clipperhouse/uax29/v2/words
func findWordBoundaries(content string, offset int) (start, end int) {
    // Use word boundary detection algorithm
    // For simplicity, we can start with space/punctuation based detection
    // and upgrade to UAX#29 later
}
```

### Phase 7: Styles

**File:** `internal/tui/styles.go`

Add selection style:

```go
type Styles struct {
    // ... existing styles ...

    // Text selection highlight
    TextSelection lipgloss.Style
}
```

In `InitStyles()`:

```go
s.TextSelection = lipgloss.NewStyle().
    Foreground(s.Colors.Bg).
    Background(s.Colors.Primary).
    Reverse(true)  // Invert colors for selection
```

### Phase 8: User Feedback

**File:** `internal/tui/models/chat.go`

```go
func (m *ChatModel) notifyCopied() tea.Cmd {
    // Show temporary "copied" message
    return func() tea.Msg {
        return StatusMsg{Text: "Selected text copied to clipboard", Timeout: 2 * time.Second}
    }
}
```

## File Changes Summary

| File | Changes | Lines (approx) |
|------|---------|----------------|
| `internal/tui/models/chat.go` | Add mouse state, handlers, selection logic | +150 |
| `internal/tui/models/chat_selection.go` | New file for selection utilities | +200 |
| `internal/tui/styles.go` | Add TextSelection style | +5 |
| `internal/tui/app.go` | Update mouse routing | +10 |

## Dependencies

**Existing dependencies (already in go.mod):**
- `github.com/charmbracelet/bubbletea` - TUI framework, `tea.SetClipboard()`
- `github.com/charmbracelet/lipgloss` - Styling
- `github.com/charmbracelet/x/ansi` - ANSI code stripping

**New dependencies needed:**
- `github.com/clipperhouse/uax29/v2/words` - UAX#29 word boundary detection (for double-click)
- `github.com/atotto/clipboard` - Native clipboard fallback (optional, OSC52 preferred)

## Testing Checklist

- [ ] Mouse drag selects text in viewport
- [ ] Selection is visually highlighted (reverse colors)
- [ ] Mouse release copies to clipboard
- [ ] "Copied" confirmation message appears
- [ ] Double-click selects word
- [ ] Triple-click selects line
- [ ] Ctrl+M still toggles mouse mode
- [ ] Shift+drag bypasses selection (native terminal selection)
- [ ] Works over SSH (OSC52)
- [ ] Selection works with wrapped lines
- [ ] Selection works when scrolled

## Implementation Order

1. **Phase 1-2:** State tracking and coordinate mapping (foundation)
2. **Phase 3-4:** Mouse handlers and rendering (core functionality)
3. **Phase 5:** Text extraction and clipboard (end-to-end)
4. **Phase 6-8:** Word/line selection, styles, feedback (polish)

## Alternative Considered: Crush's List-Based Approach

Crush uses a custom list component where each message is a highlightable item. This provides:

- **Pros:**
  - Natural item-at-position mapping
  - Each item handles its own rendering
  - Cleaner separation of concerns

- **Cons:**
  - Requires major architecture change for Meept
  - Custom list component to maintain
  - More complex message rendering

**Decision:** Keep viewport-based approach for Meept to minimize disruption while adding the requested functionality.

## References

- [Crush Repository](https://github.com/charmbracelet/crush)
- [Crush chat.go](https://github.com/charmbracelet/crush/blob/main/internal/ui/model/chat.go) - Mouse handling reference
- [Crush highlight.go](https://github.com/charmbracelet/crush/blob/main/internal/ui/list/highlight.go) - Highlight rendering reference
- [Crush common.go](https://github.com/charmbracelet/crush/blob/main/internal/ui/common/common.go) - Clipboard reference
- [OSC52 Specification](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html#h3-Operating-System-Commands)
