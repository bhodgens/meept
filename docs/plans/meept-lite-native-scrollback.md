# Meept-Lite Native Scrollback Research

## Problem

The current meept-lite implementation uses `termbox-go` which operates in the terminal's **alt screen buffer**. This prevents:
- Native terminal text selection (Shift+drag)
- Native scrollback (Shift+PageUp/PageDown)
- Copy/paste from terminal history

## Root Cause

```go
// cmd/meept-lite/tui.go:207
if err := termbox.Init(); err != nil {
    return err
}
```

`termbox.Init()` switches to the alt screen buffer, which is separate from the main terminal scrollback buffer.

## Architecture Analysis

### Current Implementation

```
termbox-go (alt screen)
    ├── Clear() - clears alt screen
    ├── SetCell() - draws to alt screen
    └── Flush() - renders alt screen
```

**Pros:**
- Fixed prompt at bottom stays in place
- Full control over terminal rendering
- Mouse support built-in

**Cons:**
- No native scrollback access
- No text selection across sessions
- Terminal history inaccessible

### Option A: Main Buffer Mode (Not Viable)

termbox-go has no "main buffer" mode. The library is designed for full-screen TUI apps.

### Option B: Replace termbox-go with Direct ANSI Codes

**Pattern:**
```go
// Instead of termbox.Init()
fmt.Print("\x1b[?1049h")  // DON'T switch to alt screen

// Render to main buffer
for _, line := range scrollback {
    fmt.Println(line)  // Writes to main scrollback
}

// Position cursor for fixed prompt
fmt.Printf("\x1b[%d;%dH", height, 1)  // Move to bottom-left
```

**Key ANSI sequences needed:**

| Sequence | Purpose |
|----------|---------|
| `\x1b[H` | Home cursor |
| `\x1b[2J` | Clear screen |
| `\x1b[Y;XH` | Position cursor (Y=row, X=col) |
| `\x1b[K` | Clear line from cursor |
| `\x1b[?25h` | Show cursor |
| `\x1b[?25l` | Hide cursor |
| `\x1b[?2004h` | Enable bracketed paste |
| `\x1b[?2004l` | Disable bracketed paste |

**Implementation approach:**

```go
type TUI struct {
    // Keep existing fields
    scrollback   []string
    cursorX      int
    inputBuffer  strings.Builder
    // ... menus, etc.

    // Terminal state
    out          *os.File  // stdout for rendering
    width, height int      // terminal size
}

func (t *TUI) Run() error {
    // DON'T call termbox.Init() - stay in main buffer

    // Enable raw mode for input
    oldState, err := termbox.RawMode()
    if err != nil {
        return err
    }
    defer termbox.CloseMode()  // Restore terminal on exit

    // Enable mouse for scrolling
    fmt.Print("\x1b[?1000h")  // Simple mouse mode
    fmt.Print("\x1b[?1002h")  // Button drag mode

    t.render()

    // Event loop (poll for input)
    for !t.quitting {
        ev := termbox.PollEvent()  // Can still use for input polling
        t.handleEvent(ev)
    }

    return nil
}

func (t *TUI) render() {
    // Clear screen using ANSI
    fmt.Print("\x1b[H\x1b[2J")

    width, height := termbox.Size()  // Can still use for size detection
    scrollbackHeight := height - 1   // Leave room for prompt

    // Render scrollback lines
    startIdx := 0
    if len(t.scrollback) > scrollbackHeight {
        startIdx = len(t.scrollback) - scrollbackHeight
    }

    for i := 0; i < scrollbackHeight && startIdx+i < len(t.scrollback); i++ {
        line := t.scrollback[startIdx+i]
        fmt.Printf("\x1b[%d;1H\x1b[K%s", i+1, line)  // Position + clear line + text
    }

    // Render prompt at bottom
    fmt.Printf("\x1b[%d;1H\x1b[K", height)  // Position at bottom, clear line
    fmt.Print("you> ")
    fmt.Print(t.inputBuffer.String())

    // Position cursor after input
    fmt.Printf("\x1b[%d;%dH", height, 5+t.cursorX)

    // Flush stdout
    os.Stdout.Sync()
}
```

### Option C: Use a Lower-Level TUI Library

**Alternative: `tcell`**

```go
import "github.com/gdamore/tcell/v2"

screen, _ := tcell.NewScreen()
screen.Init()
screen.SetStyle(tcell.StyleDefault)
screen.Clear()
```

**Pros:**
- More control over buffer mode
- Better cross-platform support
- Active maintenance

**Cons:**
- Still uses alt screen by default
- Same fundamental limitation

### Option D: Hybrid Approach (Recommended)

Keep termbox-go for input handling but render to main buffer:

```go
func (t *TUI) Run() error {
    // Use termbox for size + input, not rendering
    if err := termbox.Init(); err != nil {
        return err
    }

    // Immediately switch to main buffer
    fmt.Print("\x1b[?1049l")  // Switch to main buffer

    // ... rest of loop
}

func (t *TUI) render() {
    // DON'T use termbox.Clear/SetCell/Flush
    // Use direct ANSI sequences to main buffer

    // Clear main screen
    fmt.Print("\x1b[H\x1b[2J")

    // Render scrollback
    for i, line := range t.scrollback {
        fmt.Printf("\x1b[%d;1H%s", i+1, line)
    }

    // Fixed prompt at bottom
    height, _ := termbox.Size()
    fmt.Printf("\x1b[%d;1H> %s", height, t.inputBuffer.String())
    fmt.Printf("\x1b[%d;%dH", height, 3+t.cursorX)

    // Force flush
    fmt.Print("\x1b[0m")  // Reset colors
    os.Stdout.Sync()
}
```

**Issue:** This approach conflicts with termbox's internal state management.

### Option E: Rewrite Without termbox-go (Cleanest)

Complete rewrite using only ANSI sequences + raw mode:

```go
package main

import (
    "bufio"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "golang.org/x/term"
)

type TUI struct {
    oldState *term.State
    in       *bufio.Reader
    out      *os.File
}

func (t *TUI) Init() error {
    // Save terminal state
    t.oldState, _ = term.MakeRaw(int(os.Stdin.Fd()))

    // Enable mouse, bracketed paste
    fmt.Print("\x1b[?1000h\x1b[?2004h")

    // Handle SIGWINCH for resize
    go t.handleResize()

    return nil
}

func (t *TUI) Close() {
    // Restore terminal
    fmt.Print("\x1b[?1000l\x1b[?2004l")
    term.Restore(int(os.Stdin.Fd()), t.oldState)
}

func (t *TUI) Render() {
    // Clear screen
    fmt.Print("\x1b[H\x1b[2J")

    // Render to main scrollback buffer
    for _, line := range t.scrollback {
        fmt.Println(line)  // Goes to scrollback!
    }

    // Fixed prompt using cursor positioning
    height := t.getHeight()
    fmt.Printf("\x1b[%d;1H\x1b[Kyou> %s", height, t.inputBuffer.String())
    fmt.Printf("\x1b[%d;%dH", height, 5+t.cursorX)
}
```

## Recommended Implementation

**Option E** - Full rewrite without termbox-go:

1. Use `golang.org/x/term` for raw mode
2. Direct ANSI escape sequences for rendering
3. Main buffer for scrollback lines
4. Cursor positioning for fixed prompt

### Migration Steps

1. **Phase 1: Input handling**
   - Replace `termbox.PollEvent()` with custom reader
   - Handle key codes, mouse, bracketed paste

2. **Phase 2: Rendering**
   - Replace `termbox.Clear/SetCell/Flush` with ANSI sequences
   - Use main buffer for scrollback
   - Use cursor positioning for fixed prompt

3. **Phase 3: Testing**
   - Verify text selection works (Shift+drag)
   - Verify native scrollback (Shift+PageUp)
   - Verify fixed prompt stays at bottom

### Sample Implementation

See: `cmd/meept-lite/main.go` (new file)

```go
// Minimal example: main buffer + fixed prompt
package main

import (
    "fmt"
    "os"
    "golang.org/x/term"
)

func main() {
    oldState, _ := term.MakeRaw(0)
    defer term.Restore(0, oldState)

    // Main buffer rendering
    for i := 0; i < 100; i++ {
        fmt.Printf("Line %d\n", i)  // Goes to scrollback
    }

    // Fixed prompt at bottom
    _, h, _ := term.GetSize(0)
    fmt.Printf("\x1b[%d;1H\x1b[K> input here", h)
    fmt.Printf("\x1b[%d;%dH", h, 12)  // Position cursor
}
```

## Trade-offs

| Approach | Text Selection | Fixed Prompt | Implementation Cost |
|----------|---------------|--------------|---------------------|
| Alt screen (current) | ❌ | ✅ | Done |
| Main buffer | ✅ | ❌ (scrolls) | Low |
| Main buffer + cursor positioning | ✅ | ✅ | Medium |
| Full rewrite (Option E) | ✅ | ✅ | High |

## Conclusion

**Recommended: Option E** - Full rewrite without termbox-go.

This provides:
- Native terminal scrollback (user can select text)
- Fixed prompt at bottom via cursor positioning
- Simpler codebase (direct ANSI, no library abstraction)
- Better long-term maintainability

**Estimated effort:** 4-6 hours for complete rewrite

## Related Files

- `cmd/meept-lite/tui.go` - Main TUI implementation (rewrite target)
- `internal/sharedclient/` - Shared components (stay unchanged)
- `internal/sharedclient/menus/` - Menu implementations (need cursor position updates)
