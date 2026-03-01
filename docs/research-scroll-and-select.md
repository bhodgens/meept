# Research: Scrolling and Text Selection in Terminal TUIs

## Executive Summary

This document investigates how **OpenCode** (opencode.ai) and **Charmbracelet Crush** implement scrolling and text selection in terminal-based AI coding agents, with analysis of applicable techniques for Go-based TUI applications like Meept.

### Key Findings

| Project | Architecture | Chat History Scrolling | Text Selection | Terminal Scrollback |
|---------|--------------|----------------------|----------------|-------------------|
| **OpenCode** | xterm.js (browser) | ✅ CSS/JS native | ✅ DOM API | ✅ Preserved |
| **Crush** | BubbleTea (Go) | ✅ Viewport (works great) | ⚠️ Custom OSC52 | ❌ Lost (but irrelevant for chat) |
| **BubbleTea** | Pure Go TUI | ⚠️ Viewport component | ❌ Must implement | ❌ Lost in alt-screen |

### The Core Technical Constraint

**Terminal alternate screen mode** (used by most TUIs for a clean UI) **disables**:
1. Native terminal text selection
2. Terminal scrollback history

This is a fundamental limitation of the ANSI terminal protocol, not something that can be easily worked around in application code.

### Your Options

1. **Don't use alternate screen** → Native selection works, but UI is messier
2. **Use alternate screen** → Clean UI, but must implement selection yourself and lose scrollback
3. **Use xterm.js** → Everything works, but it's not a "pure" terminal app

## The Core Problem: Terminal Alternate Screen Limitations

### What is Alternate Screen Mode?

When TUI applications (like vim, less, tmux, BubbleTea apps) run, they use the **alternate screen buffer** via ANSI escape sequences (`\x1b[?1049h`). This:

1. Saves the current terminal contents
2. Switches to a fresh buffer for the application
3. **Disables native terminal text selection**

### Why Native Selection Doesn't Work

| State | Mouse Mode | Text Selection | Scrollback |
|-------|-----------|----------------|------------|
| Normal terminal | Off | ✅ Native | ✅ Full history |
| Alternate screen | Off | ✅ Native | ❌ No history |
| Alternate screen | On | ❌ Disabled | ❌ No history |

When an application enables **mouse tracking mode**, the terminal forwards mouse events as escape sequences instead of using them for selection. This is why:

- Vim/tmux disable native text selection
- BubbleTea apps don't have "free" text selection
- You need to hold Shift to bypass and select text

## OpenCode's Approach: xterm.js

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Browser / Electron Context                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  xterm.js Terminal Emulator                           │  │
│  │  - Renders terminal output in <canvas>                │  │
│  │  - Full DOM API access for selection                  │  │
│  │  - Native clipboard via navigator.clipboard           │  │
│  │  - Smooth scrolling via CSS/JS                        │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Why This Works

xterm.js runs in a browser context where:

- **Text selection** is handled by the DOM (`window.getSelection()`)
- **Scrolling** is standard CSS/overflow behavior
- **Clipboard** access via `navigator.clipboard` API
- **Mouse events** don't conflict with terminal protocols

This is **NOT a pure terminal TUI**—it's a web application that emulates a terminal.

## Pure Terminal TUI Options (Go)

### 1. tcell + Custom Implementation

**Library:** `github.com/gdamore/tcell/v2`

**Pros:**
- Low-level control over everything
- Full mouse event support (click, drag, wheel)
- Cross-platform

**Cons:**
- Must implement selection rendering yourself
- Must calculate text positions manually
- Must handle clipboard operations per-platform

**Code Pattern:**
```go
// Enable mouse support
screen.EnableMouse()

// Track selection state
type Selection struct {
    StartX, StartY int
    EndX, EndY     int
    Active         bool
}

// Handle mouse events
case *tcell.EventMouse:
    x, y := ev.Position()
    switch ev.Buttons() {
    case tcell.Button1:
        // Mouse down - start selection
        selection.StartX, selection.StartY = x, y
        selection.Active = true
    case tcell.ButtonNone:
        // Mouse up - end selection
        if selection.Active {
            selection.EndX, selection.EndY = x, y
            copyToClipboard(selection)
        }
    }
```

### 2. tview (Recommended for Go)

**Library:** `github.com/rivo/tview`

**Pros:**
- **TextArea component has built-in text selection**
- Mouse drag selection already implemented
- Copy/paste support included
- Higher-level API

**Cons:**
- Widget-based architecture (may not fit all designs)
- Selection is component-scoped, not application-wide

**Key Components:**
- `TextView` - Display-only with regions/highlights
- `TextArea` - Editable with selection support
- `Form` - Form fields with navigation

### 3. BubbleTea + Bubbles

**Library:** `github.com/charmbracelet/bubbletea`

**Current State:**
- ✅ Mouse event support (`tea.MouseMsg`)
- ✅ Viewport component with scrolling
- ❌ **No built-in text selection component**
- ❌ **Textarea component lacks selection**

**What You Must Implement:**
```go
type model struct {
    viewport      viewport.Model
    selection     struct {
        active    bool
        start, end position
    }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        switch msg.Action {
        case tea.MouseActionPress:
            m.selection.active = true
            m.selection.start = mouseToPosition(msg)
        case tea.MouseActionRelease:
            m.selection.active = false
            m.selection.end = mouseToPosition(msg)
        }
    }
}
```

## Cross-Language Alternatives

### Rust: tui-textarea

**Crate:** `tui-textarea` on crates.io

**Features:**
- Multi-line text editing
- Mouse selection support
- Scrollable viewport
- Copy/paste integration

This is **mature and production-ready** for Rust TUIs.

## Implementation Strategies

### Strategy A: Use tview TextArea (Fastest for Go)

If you can work within tview's widget model:

```go
import "github.com/rivo/tview"

area := tview.NewTextArea()
area.SetChangedFunc(func() {
    // Handle selection changes
})
```

### Strategy B: Build on BubbleTea (Maximum Flexibility)

1. Enable mouse: `tea.WithMouseAllMotion()`
2. Track selection state in model
3. Render highlights using lipgloss reverse styles
4. Implement clipboard (platform-specific)

**Clipboard Libraries:**
- Go: `github.com/atotto/clipboard`
- Cross-platform wrappers needed for Wayland/X11/macOS/Windows

### Strategy C: Hybrid Approach

Use BubbleTea for UI but embed a tview TextArea for the chat transcript:

```go
// Embed tview in BubbleTea application
// Let tview handle its own mouse events
// Sync state between frameworks
```

## Charmbracelet Crush: BubbleTea-Based Approach

**Repository:** [github.com/charmbracelet/crush](https://github.com/charmbracelet/crush)

Crush is a terminal AI coding agent built with **BubbleTea** that faces the same terminal limitations as any pure TUI. Here's how it handles scrolling and text selection:

### Crush's Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Pure Terminal (Alternate Screen Mode)                       │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  BubbleTea Application                                │  │
│  │  - viewport.Model for scrolling content               │  │
│  │  - Custom "select to copy" feature                    │  │
│  │  - OSC52 clipboard escape sequences                   │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### How Crush Handles Text Selection

**The "Select to Copy" Feature:**
1. Tracks mouse drag events via `tea.MouseMsg`
2. Calculates selected text region
3. Copies to clipboard automatically
4. Shows "OKAY!" confirmation message

**Known Issues ([Issue #661](https://github.com/charmbracelet/crush/issues/661)):**
- Auto-copy doesn't work on some terminals (e.g., GNOME Terminal on Ubuntu)
- Selection resets after a few milliseconds on some systems
- Users cannot disable auto-copy behavior
- Users request ability to select without copying

### Important Clarification: Two Types of "Scrollback"

**Terminal Scrollback** (lost in alt-screen mode):
- The terminal emulator's history of commands/output BEFORE your app
- After exiting a TUI app, you can't scroll up to see previous commands
- Only matters if users frequently reference terminal history

**Application-Internal Scrolling** (works fine via viewport):
- The chat transcript/history while the app is running
- Users CAN scroll back to see old messages
- This is what Crush implements and what actually matters for chat apps

```
┌────────────────────────────────────────────────────────────────────┐
│  BEFORE APP                         |  DURING APP (Alt Screen)        │
│  ┌─────────────────────────────┐    │  ┌──────────────────────────┐  │
│  │ $ ls                        │    │  │ │ Chat Transcript         │  │
│  │ $ git status                │    │  │ │ User: hi                │  │
│  │ $ crush ◄──────┐            │    │  │ │ Agent: hello            │  │
│  └────────────────│────────────┘    │  │ │ [viewport scrolling]    │  │
│                   │                 │  │ └──────────────────────────┘  │
│                   ▼                 │                                  │
│  AFTER APP        │                 │  ↑ Users can scroll chat HERE  │
│  ┌─────────────────────────────┐    │     This works perfectly       │
│  │ $ ls                        │    │                                  │
│  │ $ git status                │    │                                  │
│  │ (crush output here)          │    │                                  │
│  │ ◄──── Can't scroll past this │    │                                  │
│  └─────────────────────────────┘    │                                  │
│      Terminal history was cleared   │                                  │
└────────────────────────────────────────────────────────────────────────┘
```

### BubbleTea's Two Modes

BubbleTea supports two fundamentally different operating modes:

| Mode | Initialization | Scrollback | Native Selection | Use Case |
|------|----------------|------------|------------------|----------|
| **Inline** | `tea.NewProgram(m)` | ✅ Terminal history | ✅ Works | CLI tools, output |
| **Alt Screen** | `tea.NewProgram(m, tea.WithAltScreen())` | ❌ App-internal only | ❌ Disabled | Interactive TUI apps |

**Most BubbleTea apps use `WithAltScreen()`** for a clean UI experience, but this loses terminal scrollback.

### Internal Scrolling with Viewport

Crush uses `github.com/charmbracelet/bubbles/viewport` for internal scrolling:

```go
import "github.com/charmbracelet/bubbles/viewport"

vp := viewport.New(width, height)
vp.SetContent(longContent)
vp.GotoBottom() // Auto-scroll to new content
vp.MouseWheelEnabled = true
```

The viewport handles:
- Line-by-line scrolling (↑/↓ arrows, mouse wheel)
- Page-by-page scrolling (Page Up/Down)
- Programmatic scrolling (GotoTop, GotoBottom)
- **All chat history is preserved** in the viewport's content

**Important clarification:** The viewport maintains the FULL content buffer. Users can scroll back to see any message in the chat history. The "lost" scrollback only refers to the terminal's history of commands run BEFORE the app started—which is irrelevant for a chat application.

## Key Findings Summary

1. **OpenCode uses xterm.js** (browser-based) — gets text selection and scrollback "for free"
2. **Crush uses BubbleTea** (pure terminal) — must implement selection internally, loses terminal scrollback
3. **BubbleTea has NO built-in text selection** — Crush implemented its own
4. **Terminal alternate screen** disables native selection by design
5. **Shift key bypass** works but is inconsistent across terminals
6. **The fundamental trade-off:** Beautiful TUI (alt screen) OR native scrollback/selection — you can't have both in pure terminal mode

## Recommendations for Meept

Based on research of OpenCode, Crush, and pure terminal TUI limitations:

### For Chat/Agent Applications: Alt-Screen Mode is Fine

**Key insight:** For a chat application like Meept, the "scrollback lost" trade-off is irrelevant because:

1. ✅ **Chat history** is preserved in the viewport buffer (users can scroll back anytime while using the app)
2. ❌ **Terminal command history** is lost (but users don't need this while chatting)
3. ✅ **Clean UI** with alternate screen provides the best experience

**Recommendation:** Use `tea.WithAltScreen()` like Crush does—this is the standard for interactive TUI applications.

### Decision Framework

| Priority | Approach | Best For | Trade-offs |
|----------|----------|----------|------------|
| **Best UX** | xterm.js (like OpenCode) | Production tools, IDE-like experience | Not "pure" terminal, requires web tech |
| **Pure Go** | tview TextArea | Full-featured editors | Widget-based architecture |
| **Flexible** | Extend BubbleTea | Custom TUI designs | Must implement selection yourself |
| **Inline Mode** | BubbleTea WITHOUT alt screen | CLI tools, output-heavy apps | Messier output, loses clean TUI feel |

**For Meept specifically:** BubbleTea with `WithAltScreen()` is the right choice—it gives you a clean UI, viewport handles chat scrolling perfectly, and you just need to implement text selection (like Crush did).

### Option 1: Inline BubbleTea (Recommended for Chat)

**Key Insight:** Run BubbleTea **without** `WithAltScreen()` to preserve terminal scrollback and native text selection.

```go
// DON'T use WithAltScreen()
p := tea.NewProgram(initialModel())  // NOT: tea.WithAltScreen()
```

**Pros:**
- Native terminal text selection works (click-drag to select)
- Terminal scrollback preserves all chat history
- No custom selection code needed
- Shift+scroll works naturally

**Cons:**
- Output intermixes with previous terminal content
- No "clean slate" UI
- Less "app-like" experience

**This is how many CLI tools work** (e.g., `git log` with pager, `man` pages).

### Option 2: BubbleTea with Custom Selection (Like Crush)

Use `WithAltScreen()` for clean UI, implement selection like Crush:

```go
p := tea.NewProgram(initialModel(),
    tea.WithAltScreen(),
    tea.WithMouseCellMotion(),
)

// Track selection in model
type selectionState struct {
    active bool
    startRow, startCol int
    endRow, endCol int
}

// Copy to clipboard via OSC52 escape sequence
// (works over SSH, supported by most modern terminals)
```

**Clipboard via OSC52:**
```go
// Escape sequence to copy to clipboard
fmt.Printf("\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
```

**Pros:**
- Clean TUI experience
- Consistent behavior across platforms
- Works over SSH

**Cons:**
- Terminal scrollback is lost
- Selection doesn't work on some terminals (see Crush #661)
- Must handle clipboard edge cases

### Option 3: Toggle Mode (Best of Both Worlds)

Allow users to switch between inline and alt-screen modes:

```go
// Start in inline mode for chat history
// Press a key to toggle to "TUI mode" for focused work
// Press again to return and see full history in terminal

case tea.KeyMsg:
    if msg.String() == "ctrl+t" {
        m.altScreen = !m.altScreen
        // Trigger program restart with different options
    }
```

**Pros:**
- User chooses their experience
- Can have both history and clean UI
- Flexible for different workflows

**Cons:**
- More complex implementation
- Jarring to switch modes
- State management across modes

### Option 4: Follow OpenCode (xterm.js)

If scrollback + selection is critical and you're willing to leave pure terminal:

```
Web/Electron App
├── xterm.js for terminal rendering
├── DOM for text selection (free)
├── navigator.clipboard for copy/paste (free)
└── CSS overflow for scrolling (free)
```

**Pros:**
- Everything works perfectly
- Can add web features (tabs, split panes)
- Easier to add rich UI

**Cons:**
- Heavier than pure Go
- Different deployment model
- Not a "true" terminal experience

## References

### Go Libraries
- [tcell](https://pkg.go.dev/github.com/gdamore/tcell/v2) - Low-level terminal control
- [tview](https://pkg.go.dev/github.com/rivo/tview) - High-level widgets with selection
- [BubbleTea](https://github.com/charmbracelet/bubbletea) - Functional TUI framework
- [bubbles/viewport](https://pkg.go.dev/github.com/charmbracelet/bubbles/viewport) - Scrollable viewport
- [atotto/clipboard](https://github.com/atotto/clipboard) - Clipboard operations

### OpenCode / xterm.js
- [OpenCode Repository](https://github.com/anomalyco/opencode)
- [xterm.js](https://xtermjs.org/) - Browser terminal emulator
- [opencode.ai](https://opencode.ai/) - Official site

### Rust Alternative
- [tui-textarea](https://docs.rs/tui-textarea/) - Rust textarea with selection

### Terminal Background
- [iTerm2 Documentation](https://iterm2.com/documentation.html)
- [ANSI Escape Sequences](https://en.wikipedia.org/wiki/ANSI_escape_code)

### Crush / Charmbracelet
- [Crush Repository](https://github.com/charmbracelet/crush)
- [Issue #661: Copy problems](https://github.com/charmbracelet/crush/issues/661)
- [BubbleTea Discussion #790: Auto-scrolling](https://github.com/charmbracelet/bubbletea/discussions/790)

---

## The Fundamental Technical Limitation

### Why Can't We Have Both?

**The ANSI terminal protocol was designed in the 1970s** with these constraints:

1. **Alternate screen** (`\x1b[?1049h`) was added to allow apps to "take over" the terminal
2. When in alternate screen, the terminal emulator:
   - Stops saving to scrollback buffer (why scroll history disappears)
   - Starts forwarding mouse events to the app (why native selection breaks)
3. This is by **design**—vim/less/tmux need full control

### Modern Workarounds (Each with Trade-offs)

| Workaround | How It Works | Limitation |
|------------|--------------|------------|
| **Hold Shift** | Terminal bypasses app mouse mode | Inconsistent, not discoverable |
| **OSC52 clipboard** | Escape sequence to set clipboard | Not supported by all terminals |
| **Inline mode** | Don't use alternate screen | Output gets messy |
| **xterm.js** | Browser-based, not a real terminal | Different architecture entirely |
| **Mode toggle** | Switch between modes | Jarring UX, complex state |

### The Future?

Some newer terminals are experimenting with:
- **Extended clipboard protocols** (more reliable OSC52)
- ** smarter mouse mode toggling** (auto-detect selection intent)
- **Hybrid screen modes** (partial scrollback preservation)

But these require **terminal emulator changes**, not just application code. For a Go TUI app today, you must choose: **clean UI** OR **native scrollback/selection**.

---

## Summary for Your Decision

- **For chat/agent apps like Meept:** Use `tea.WithAltScreen()`—viewport scrolling provides full chat history access. Terminal scrollback loss is irrelevant.

- **If you want text selection:** You must implement it yourself (track mouse events, render highlights, copy to clipboard via OSC52). Crush has a working reference implementation.

- **If native scrollback is critical:** Run BubbleTea WITHOUT `WithAltScreen()`, or use xterm.js like OpenCode. But this is rarely needed for chat applications.

- **The Shift key workaround** lets users select text with the terminal's native selection, but it's not something you can build into the app—it's a terminal emulator feature.
