# Bubbletea v2 Migration Plan

**Date:** 2026-04-18
**Target:** `github.com/caimlas/meept` (meept CLI binary)
**Scope:** `cmd/meept/`, `internal/tui/`

---

## Release Summary

| Package | Current | Target | Released |
|---------|---------|--------|----------|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | **v2.0.6** | 2026-04-16 |
| `github.com/charmbracelet/bubbles` | v1.0.0 | **v2.1.0** | 2026-03-26 |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | **v2.0.3** | 2026-04-13 |

---

## Key v2 Changes

1. **Import paths** moved to vanity domain: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`
2. **`View()` returns `tea.View`** instead of `string` — the biggest API change
3. **Declarative views** — `AltScreen`, `WindowTitle`, `MouseMode`, etc. are View fields, not program options or commands
4. **Key messages** — `tea.KeyMsg` is now an interface; use `tea.KeyPressMsg` for presses; `msg.Type`→`msg.Code`, `msg.Runes`→`msg.Text`, `msg.Alt`→`msg.Mod`; space bar returns `"space"` not `" "`
5. **Mouse messages** — `tea.MouseMsg` is now an interface; split into `MouseClickMsg`, `MouseReleaseMsg`, `MouseWheelMsg`, `MouseMotionMsg`; button constants renamed (`MouseButtonLeft`→`MouseLeft`); access coords via `msg.Mouse().X`
6. **Paste messages** — no longer come as `KeyMsg` with `.Paste` flag; now `tea.PasteMsg`, `tea.PasteStartMsg`, `tea.PasteEndMsg`
7. **Removed commands** — `tea.EnterAltScreen`, `tea.ExitAltScreen`, `tea.SetWindowTitle`, `tea.HideCursor`, `tea.ShowCursor`, etc.
8. **Removed program options** — `tea.WithAltScreen()`, `tea.WithMouseCellMotion()`, `tea.WithReportFocus()`, etc.
9. **New Cursed Renderer** — ncurses-based, highly optimized (automatic)
10. **bubbles widgets** — getter/setter methods replace exported fields; functional options in constructors; `DefaultKeyMap()` is now a function

---

## Affected Files

### 14 files need changes

| File | Changes Required | Effort |
|------|-----------------|--------|
| `cmd/meept/chat.go` | Import path, `tea.WithAltScreen()` removal, program init | Low |
| `internal/tui/app.go` | Import, `View()` return type, `tea.KeyMsg`→`tea.KeyPressMsg`, `tea.EnterAltScreen`→View field, `tea.SetWindowTitle`→View field, `isPrintableKey` uses `msg.Type`→`msg.Code`, mouse handling | **High** |
| `internal/tui/events.go` | Import path only | Low |
| `internal/tui/modal.go` | Import, `tea.MouseMsg`→`tea.MouseClickMsg`, button constant renames | Medium |
| `internal/tui/sidebar.go` | Import path, `View()` return type if applicable | Medium |
| `internal/tui/vim/mode.go` | Import, `tea.KeyMsg`→`tea.KeyPressMsg`, `msg.Type`→`msg.Code` | Medium |
| `internal/tui/viz/dispatch.go` | Import path | Low |
| `internal/tui/models/chat.go` | Import, `View()` return type, `tea.KeyMsg`→`tea.KeyPressMsg`, `tea.WindowSizeMsg` handling, widget API changes (viewport/textarea setters), `msg.Type`→`msg.Code` | **High** |
| `internal/tui/models/tasks.go` | Import, `tea.KeyMsg`→`tea.KeyPressMsg`, table widget setter API | Medium |
| `internal/tui/models/queue.go` | Import, `tea.KeyMsg`→`tea.KeyPressMsg`, table widget setter API | Medium |
| `internal/tui/models/memory.go` | Import, `tea.KeyMsg`→`tea.KeyPressMsg`, textinput/list widget setter API | Medium |
| `internal/tui/models/status.go` | Import, `tea.KeyMsg`→`tea.KeyPressMsg` | Low |
| `internal/tui/models/input_selection.go` | Import, `tea.MouseMsg`→interface, `msg.Action`→type switch, `msg.X`→`msg.Mouse().X` | **High** |
| `internal/tui/models/chat_selection.go` | No direct bubbletea imports (uses `ansi` package) | None |

---

## Detailed Migration Steps

### Step 1: Update Dependencies

```bash
go get charm.land/bubbletea/v2@v2.0.6
go get charm.land/bubbles/v2@v2.1.0
go get charm.land/lipgloss/v2@v2.0.3
go mod tidy
```

### Step 2: Update Import Paths (all 14 files)

Every file needs import path updates:

```go
// Before
import tea "github.com/charmbracelet/bubbletea"
import "github.com/charmbracelet/lipgloss"
import "github.com/charmbracelet/bubbles/textarea"
import "github.com/charmbracelet/bubbles/viewport"
import "github.com/charmbracelet/bubbles/table"
import "github.com/charmbracelet/bubbles/list"
import "github.com/charmbracelet/bubbles/textinput"
import "github.com/charmbracelet/bubbles/key"

// After
import tea "charm.land/bubbletea/v2"
import "charm.land/lipgloss/v2"
import "charm.land/bubbles/v2/textarea"
import "charm.land/bubbles/v2/viewport"
import "charm.land/bubbles/v2/table"
import "charm.land/bubbles/v2/list"
import "charm.land/bubbles/v2/textinput"
import "charm.land/bubbles/v2/key"
```

### Step 3: Change `View()` Return Type

Every model with a `View() string` method must return `tea.View`:

**app.go** (line 794):
```go
// Before
func (a *App) View() string {
    ...
    return b.String()
}

// After
func (a *App) View() tea.View {
    ...
    var v tea.View
    v.SetContent(b.String())
    v.AltScreen = true
    v.WindowTitle = "Meept"  // replaces tea.SetWindowTitle in Init()
    return v
}
```

Same pattern for `ChatModel.View()`, `TasksModel.View()`, `QueueModel.View()`, `MemoryModel.View()`, `StatusModel.View()`, and `SidebarModel.View()`.

### Step 4: Remove Program Options

**cmd/meept/chat.go** (line 72-76):
```go
// Before
p := tea.NewProgram(app,
    tea.WithAltScreen(),
)

// After
p := tea.NewProgram(app)
```

### Step 5: Remove Imperative Commands from Init()

**app.go** (line 179-185):
```go
// Before
func (a *App) Init() tea.Cmd {
    return tea.Batch(
        a.connectDaemon,
        a.loadSession,
        tea.EnterAltScreen,
        tea.SetWindowTitle("Meept"),
    )
}

// After — AltScreen and WindowTitle are now View fields (Step 3)
func (a *App) Init() tea.Cmd {
    return tea.Batch(
        a.connectDaemon,
        a.loadSession,
    )
}
```

### Step 6: Replace `tea.KeyMsg` with `tea.KeyPressMsg`

**All Update() methods.** Global search and replace pattern:

```go
// Before
case tea.KeyMsg:

// After
case tea.KeyPressMsg:
```

### Step 7: Update Key Field Access

**app.go** — `isPrintableKey` function (line 1082-1090):
```go
// Before
func isPrintableKey(msg tea.KeyMsg) bool {
    switch msg.Type {
    case tea.KeyRunes:
        return true
    case tea.KeySpace:
        return true
    }
    return false
}

// After — KeyMsg is now an interface, KeyPressMsg is the struct
func isPrintableKey(msg tea.KeyPressMsg) bool {
    // In v2, any key with text content is a printable key
    return len(msg.Text) > 0
}
```

**vim/mode.go** — Any usage of `msg.Type`, `msg.Runes`, `msg.Alt`:
```go
// Before
if msg.Type == tea.KeyRunes { ... }
if msg.Alt { ... }
runes := string(msg.Runes)

// After
if len(msg.Text) > 0 { ... }
if msg.Mod.Contains(tea.ModAlt) { ... }
text := msg.Text
```

### Step 8: Update Space Bar Matching

Search for `case " "` and replace with `case "space"`:

```go
// Before
case " ":

// After
case "space":
```

### Step 9: Update Mouse Message Handling

**modal.go** — `HandleMouse` method (line 253):
```go
// Before
func (s *SessionPickerModal) HandleMouse(msg tea.MouseMsg, screenW, screenH int) tea.Cmd {
    if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionRelease {
        return nil
    }
    if msg.X < modalX || msg.X >= modalX+s.width {
        return nil
    }
    relY := msg.Y - modalY - headerLines
    ...
}

// After
func (s *SessionPickerModal) HandleMouse(msg tea.MouseMsg, screenW, screenH int) tea.Cmd {
    // In v2, MouseMsg is an interface — get the underlying mouse data
    click, ok := msg.(tea.MouseClickMsg)
    if !ok || click.Button != tea.MouseLeft {
        return nil
    }
    mouse := click.Mouse()
    if mouse.X < modalX || mouse.X >= modalX+s.width {
        return nil
    }
    relY := mouse.Y - modalY - headerLines
    ...
}
```

**input_selection.go** — `HandleInputMouse` (line 14):
```go
// Before
func (m *ChatModel) HandleInputMouse(msg tea.MouseMsg) tea.Cmd {
    switch msg.Action {
    case tea.MouseActionPress:
        return m.handleInputMousePress(msg)
    case tea.MouseActionRelease:
        return m.handleInputMouseRelease(msg)
    case tea.MouseActionMotion:
        ...
    }
}

// After — split into separate message types
func (m *ChatModel) HandleInputMouse(msg tea.MouseMsg) tea.Cmd {
    switch msg := msg.(type) {
    case tea.MouseClickMsg:
        return m.handleInputMousePress(msg)
    case tea.MouseReleaseMsg:
        return m.handleInputMouseRelease(msg)
    case tea.MouseMotionMsg:
        if m.inputMouseDown {
            return m.handleInputMouseDrag(msg)
        }
    }
    return nil
}
```

**input_selection.go** — Mouse coordinate access throughout:
```go
// Before
adjustedX := msg.X - 1
adjustedY := msg.Y

// After
mouse := msg.Mouse()  // for MouseClickMsg
// or
mouse := msg.Mouse()  // for MouseMotionMsg
adjustedX := mouse.X - 1
adjustedY := mouse.Y
```

### Step 10: Update bubbles Widget APIs

**All widgets** now use getter/setter methods and functional options:

#### textarea
```go
// Before
ti := textarea.New()
ti.Placeholder = "..."
ti.CharLimit = 256
ti.Width = 50
ti.Focus()
ti.Blur()
ti.SetValue("...")
w := ti.Width

// After
ti := textarea.New(
    textarea.WithPlaceholder("..."),
    textarea.WithCharLimit(256),
    textarea.WithWidth(50),
)
ti.Focus()
ti.Blur()
ti.SetValue("...")
w := ti.Width()  // getter method
```

#### viewport
```go
// Before
vp := viewport.New(width, height)
vp.Width = 80
vp.SetContent("...")
y := vp.YOffset

// After
vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(24))
vp.SetContent("...")
y := vp.YOffset()
```

#### table
```go
// Before
t := table.New(table.WithColumns(columns), table.WithFocused(true), table.WithHeight(10))
t.SetHeight(20)
h := t.Height

// After
t := table.New(table.WithColumns(columns), table.WithFocused(true), table.WithHeight(10))
t.SetHeight(20)
h := t.Height()  // getter method
```

#### list
```go
// Before
l := list.New(items, delegate, 40, 10)
l.SetShowStatusBar(false)
l.Styles.Title = style

// After — same constructor, but Styles may use getter
l := list.New(items, delegate, 40, 10)
l.SetShowStatusBar(false)
l.Styles().Title = style  // or l.SetStyles(s)
```

#### textinput
```go
// Before
ti := textinput.New()
ti.Placeholder = "..."
ti.CharLimit = 256
ti.Focus()
ti.Width = 50
w := ti.Width

// After
ti := textinput.New(
    textinput.WithPlaceholder("..."),
    textinput.WithCharLimit(256),
)
ti.Focus()
ti.SetWidth(50)
w := ti.Width()  // getter method
```

#### key
```go
// Before
km := textinput.DefaultKeyMap
key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))

// After
km := textinput.DefaultKeyMap()  // function call now
key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))  // same
```

### Step 11: Update Lip Gloss Usage

**AdaptiveColor removal** — lipgloss v2 removed `AdaptiveColor`. If used anywhere, replace with:

```go
// Before
lipgloss.AdaptiveColor{Light: "#fff", Dark: "#333"}

// After — Option A: use compat package
import "charm.land/lipgloss/v2/compat"
color := compat.AdaptiveColor{Light: lipgloss.Color("#fff"), Dark: lipgloss.Color("#333")}

// After — Option B: query background in bubbletea and pick explicitly
```

**Print functions** — lipgloss is now "pure" (no I/O). Use `lipgloss.Println()` instead of `fmt.Println()` for lipgloss-styled output. In meept's case, bubbletea handles rendering, so this is unlikely to affect much.

### Step 12: Remove Custom Clipboard Code

**app.go** — The `copyToClipboard` function (line 1100-1126) and OSC52 manual escape sequences can be replaced with bubbletea v2's built-in clipboard support:

```go
// Before — manual OSC52 + platform fallback
func copyToClipboard(text string) error {
    encoded := base64.StdEncoding.EncodeToString([]byte(text))
    osc52 := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
    fmt.Print(osc52)
    // platform fallbacks...
}

// After — use bubbletea's native clipboard
// In Update:
case tea.KeyPressMsg:
    if msg.String() == "ctrl+c" && m.hasSelection() {
        return m, tea.SetClipboard(m.extractSelectedText())
    }
```

### Step 13: Update teatest Usage

**app_test.go and other test files** using `charmbracelet/x/exp/teatest`:

The teatest package may need updating for v2 compatibility. Check if `charm.land/bubbletea/v2` provides a compatible teatest.

### Step 14: Update Terminal Title Setting

**app.go** — `setTerminalTitle` (line 923-935) uses manual OSC escape sequence. Replace with View field:

```go
// Before — manual OSC escape
func (a *App) setTerminalTitle() {
    fmt.Fprintf(os.Stdout, "\033]0;%s\007", title)
}

// After — declarative via View field
// Set in View():
v.WindowTitle = title
// Remove setTerminalTitle() calls from Update()
```

---

## Execution Order

1. Create branch `feat/bubbletea-v2`
2. Step 1: Update go.mod dependencies
3. Step 2: Update all import paths (mechanical, all files)
4. Step 3: Change View() return types (all model files)
5. Step 4-5: Remove program options and imperative commands
6. Step 6-8: Update key message handling
7. Step 9: Update mouse message handling
8. Step 10: Update bubbles widget APIs
9. Step 11: Update lip gloss usage
10. Step 12-14: Clipboard, teatest, terminal title
11. Build: `go build ./...`
12. Test: `go test ./...`
13. Manual: `./bin/meept chat` — verify all views, modals, keybindings
14. Update CLAUDE.md, docs

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| `charm.land` vanity domain resolution issues | Medium | High | Try `GOPROXY=direct` if proxy doesn't have it yet |
| bubbles v2 widget API differences beyond documented | Medium | Medium | Check examples in bubbletea v2 repo |
| teatest incompatibility | Medium | Low | May need to pin or update teatest |
| `go.sum` / transitive dependency conflicts | Low | Medium | `go mod tidy` should resolve; check `charmbracelet/x/ansi` compat |
| Subtle rendering differences with Cursed Renderer | Low | Medium | Visual testing of all views |

---

## References

- [bubbletea v2.0.0 release](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0)
- [bubbletea v2.0.6 release](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.6)
- [bubbletea v2 Upgrade Guide](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md)
- [bubbles v2.0.0 release](https://github.com/charmbracelet/bubbles/releases/tag/v2.0.0)
- [bubbles v2.1.0 release](https://github.com/charmbracelet/bubbles/releases/tag/v2.1.0)
- [lipgloss v2.0.0 release](https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.0)
