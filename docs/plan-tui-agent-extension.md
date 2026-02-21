# TUI Agent Extension & Enhancement Plan

**Status:** Draft
**Created:** 2026-02-20
**Builds on:** Phase 2 multi-agent framework (commit 4a3d70a)

---

## Executive Summary

This plan extends the meept TUI to expose the new multi-agent orchestration system while simultaneously improving the overall user experience with markdown rendering, syntax highlighting, vim keybindings, and real-time agent workflow visibility.

### Goals

1. **Agent Visibility** - Real-time view of agent activity, tool calls, reasoning cycles
2. **Task Orchestration UI** - Track tasks, memory inheritance, agent assignments
3. **Rich Text Rendering** - Markdown interpretation, code syntax highlighting
4. **Vim Integration** - Modal editing with vim keybindings throughout
5. **Information Density** - Maximize useful data per screen area

---

## Part 1: Agent Activity Panel

### 1.1 New Sidebar Panel: "Agent Activity"

Add a fifth sidebar panel showing real-time agent execution state.

**Location:** `internal/tui/sidebar.go`

**Panel Structure:**
```
┌─ Agent Activity ────────────────┐
│ ● coder [iteration 3/10]        │
│   ├─ file_read: src/main.go     │
│   ├─ shell_execute: go build    │
│   └─ ◐ file_write: src/fix.go   │
│                                 │
│ Memory Context:                 │
│   inherited: 2 │ refs: 3        │
└─────────────────────────────────┘
```

**Data Sources:**
- Subscribe to `agent.action` bus events for tool calls in-flight
- Subscribe to `agent.result` for completions
- Poll `worker.list` for current agent assignments

**New RPC Method Required:**
```go
// Add to internal/rpc/proxy.go
"agent.activity": {topic: "agent.activity", timeout: 10 * time.Second}
```

**Implementation Tasks:**
1. Add `AgentActivityPanel` to sidebar (new panel type)
2. Create `AgentActivity` struct in `internal/tui/types/types.go`:
   ```go
   type AgentActivity struct {
       AgentID      string        `json:"agent_id"`
       Role         string        `json:"role"`
       Iteration    int           `json:"iteration"`
       MaxIter      int           `json:"max_iterations"`
       ToolCalls    []ToolCall    `json:"tool_calls"`
       MemoryRefs   int           `json:"memory_refs"`
       Inherited    int           `json:"inherited_memories"`
       State        string        `json:"state"` // "reasoning", "tool_exec", "waiting"
   }

   type ToolCall struct {
       Name   string `json:"name"`
       Args   string `json:"args"`   // Truncated display
       State  string `json:"state"`  // "pending", "running", "done", "error"
       Result string `json:"result"` // Truncated
   }
   ```
3. Implement bus subscription for real-time updates
4. Add progress indicator (iteration X/Y with mini progress bar)

### 1.2 Agent Iteration Visualization

Show reasoning cycle progress in the chat stream.

**Inline Progress Display:**
```
┌─────────────────────────────────────────────────────┐
│ [coder] Analyzing request...                        │
│ ├─ Iteration 1: Reading files (3 files)             │
│ ├─ Iteration 2: Planning changes                    │
│ └─ Iteration 3: ◐ Executing modifications...        │
│     └─ file_write: internal/tui/chat.go [running]   │
└─────────────────────────────────────────────────────┘
```

**Implementation:**
1. Add `AgentProgressMsg` bubbletea message type
2. Update `ChatModel` to render inline agent progress
3. Collapse progress into summary on completion:
   ```
   ✓ [coder] Completed in 3 iterations (4.2s)
   ```

---

## Part 2: Task Orchestration View

### 2.1 Enhanced Tasks View

Redesign `internal/tui/models/tasks.go` with richer data display.

**New Layout:**
```
┌─ Tasks ─────────────────────────────────────────────────────────────┐
│ Filter: [All ▼] [Active ○] [Mine ○]          Sort: [Updated ▼]     │
├─────────────────────────────────────────────────────────────────────┤
│ ID              State      Agent     Progress   Memory   Updated   │
├─────────────────────────────────────────────────────────────────────┤
│ ▶ task-0220... ● exec     coder     ████░░ 4/6  ⚡3 ⬅2  2m ago    │
│   task-0220... ✓ done     debugger  ██████ 3/3  ⚡1 ⬅0  15m ago   │
│   task-0219... ◐ plan     planner   ░░░░░░ 0/2  ⚡0 ⬅1  1h ago    │
│   task-0219... ✗ fail     coder     ███░░░ 2/5  ⚡2 ⬅0  2h ago    │
└─────────────────────────────────────────────────────────────────────┘
│ [r]efresh  [n]ew task  [Enter] details  [c]ancel  [R]etry failed   │
└─────────────────────────────────────────────────────────────────────┘
```

**Legend:**
- `⚡N` = Memory refs count
- `⬅N` = Inherited memories count
- Progress bar = CompletedJobs / TotalJobs

**New Fields from Task struct:**
- `AssignedAgent` - Show which agent is handling
- `MemoryRefs` - Count of explicit memory references
- `InheritedFrom` - Show parent task relationship
- `CreatedMemories` - Count of memories produced

### 2.2 Task Detail Modal

Press `Enter` on a task to open detailed view.

**Modal Layout:**
```
┌─ Task: Implement user authentication ───────────────────────────────┐
│                                                                     │
│ ID:          task-20260220143022.123456789                          │
│ State:       ● executing                                            │
│ Agent:       coder (executor)                                       │
│ Created:     2026-02-20 14:30:22                                    │
│ Updated:     2 minutes ago                                          │
│                                                                     │
│ Progress:    ████████░░░░ 4/6 jobs (67%)                            │
│              ✓ 4 completed  ○ 2 pending  ✗ 0 failed                 │
│                                                                     │
│ ─── Memory Context ──────────────────────────────────────────────── │
│ Inherited from: task-20260220140000.987654321 (2 memories)          │
│ Explicit refs:  mem-abc123, mem-def456, mem-ghi789                  │
│ Context query:  "authentication patterns golang"                    │
│ Created:        mem-new001, mem-new002                              │
│                                                                     │
│ ─── Linked Sessions ─────────────────────────────────────────────── │
│ • session-main (active)                                             │
│ • session-debug                                                     │
│                                                                     │
│ ─── Jobs ────────────────────────────────────────────────────────── │
│ job-001  ✓ done     Parse requirements                              │
│ job-002  ✓ done     Design schema                                   │
│ job-003  ✓ done     Implement handlers                              │
│ job-004  ● running  Write tests                                     │
│ job-005  ○ pending  Integration tests                               │
│ job-006  ○ pending  Documentation                                   │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│ [Esc] close  [c]ancel task  [r]etry failed  [l]ink session          │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.3 Task Lineage View

New view showing task inheritance tree.

**Keybinding:** `t` in Tasks view to toggle lineage mode

```
┌─ Task Lineage ──────────────────────────────────────────────────────┐
│                                                                     │
│ task-0220-root "Build auth system"                                  │
│ ├── task-0220-plan "Plan architecture" ✓                            │
│ │   └── [2 memories inherited →]                                    │
│ ├── task-0220-impl "Implement handlers" ●                           │
│ │   ├── [inherited: schema.md, patterns.md]                         │
│ │   └── [created: handler-design.md]                                │
│ └── task-0220-test "Write tests" ○                                  │
│     └── [will inherit from: task-0220-impl]                         │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Part 3: Markdown Rendering

### 3.1 Glamour Integration

Add the Charm glamour library for markdown rendering.

**Dependency:** `github.com/charmbracelet/glamour`

**Location:** New file `internal/tui/render/markdown.go`

```go
package render

import (
    "github.com/charmbracelet/glamour"
)

type MarkdownRenderer struct {
    renderer *glamour.TermRenderer
    width    int
}

func NewMarkdownRenderer(width int, darkMode bool) (*MarkdownRenderer, error) {
    style := glamour.DarkStyleConfig
    if !darkMode {
        style = glamour.LightStyleConfig
    }

    r, err := glamour.NewTermRenderer(
        glamour.WithStyles(style),
        glamour.WithWordWrap(width),
        glamour.WithEmoji(),
    )
    if err != nil {
        return nil, err
    }

    return &MarkdownRenderer{renderer: r, width: width}, nil
}

func (m *MarkdownRenderer) Render(markdown string) (string, error) {
    return m.renderer.Render(markdown)
}
```

### 3.2 Chat Message Rendering Pipeline

Update `internal/tui/models/chat.go` to detect and render markdown.

**Rendering Rules:**
1. Detect markdown indicators (```, #, *, -, etc.)
2. For messages with code blocks: render with glamour
3. For plain text: use existing word-wrap
4. Cache rendered output to avoid re-rendering on scroll

**Message Struct Extension:**
```go
type ChatMessage struct {
    Role         string
    Content      string
    Timestamp    time.Time
    State        MessageState

    // Rendering cache
    rendered     string    // Cached glamour output
    renderedAt   int       // Width when rendered
    hasMarkdown  bool      // Detected markdown
}
```

### 3.3 Code Block Detection

```go
func detectMarkdown(content string) bool {
    patterns := []string{
        "```",           // Fenced code block
        "\n# ",          // Heading
        "\n## ",         // Heading
        "\n- ",          // List
        "\n* ",          // List
        "\n1. ",         // Ordered list
        "\n> ",          // Blockquote
        "**",            // Bold
        "__",            // Bold
        "`",             // Inline code
    }
    for _, p := range patterns {
        if strings.Contains(content, p) {
            return true
        }
    }
    return false
}
```

---

## Part 4: Syntax Highlighting

### 4.1 Chroma Integration

Use the Chroma library for syntax highlighting within code blocks.

**Dependency:** `github.com/alecthomas/chroma/v2`

**Location:** `internal/tui/render/syntax.go`

```go
package render

import (
    "github.com/alecthomas/chroma/v2"
    "github.com/alecthomas/chroma/v2/formatters"
    "github.com/alecthomas/chroma/v2/lexers"
    "github.com/alecthomas/chroma/v2/styles"
)

type SyntaxHighlighter struct {
    style     *chroma.Style
    formatter chroma.Formatter
}

func NewSyntaxHighlighter() *SyntaxHighlighter {
    return &SyntaxHighlighter{
        style:     styles.Get("monokai"),
        formatter: formatters.Get("terminal256"),
    }
}

func (s *SyntaxHighlighter) Highlight(code, language string) (string, error) {
    lexer := lexers.Get(language)
    if lexer == nil {
        lexer = lexers.Analyse(code)
    }
    if lexer == nil {
        lexer = lexers.Fallback
    }

    iterator, err := lexer.Tokenise(nil, code)
    if err != nil {
        return code, err
    }

    var buf strings.Builder
    err = s.formatter.Format(&buf, s.style, iterator)
    return buf.String(), err
}
```

### 4.2 Glamour + Chroma Integration

Configure glamour to use chroma for code blocks:

```go
func NewMarkdownRenderer(width int) (*MarkdownRenderer, error) {
    r, err := glamour.NewTermRenderer(
        glamour.WithStyles(customStyleConfig()),
        glamour.WithWordWrap(width),
        glamour.WithEmoji(),
        glamour.WithPreservedNewLines(),
    )
    // ...
}

func customStyleConfig() glamour.StyleConfig {
    style := glamour.DarkStyleConfig
    // Chroma handles code block styling
    style.CodeBlock.Chroma = &glamour.Chroma{
        Theme: "monokai",
    }
    return style
}
```

### 4.3 Language Auto-Detection

For code blocks without language hints:

```go
func detectLanguage(code string) string {
    // Use chroma's analysis
    lexer := lexers.Analyse(code)
    if lexer != nil {
        return lexer.Config().Name
    }
    return "text"
}
```

---

## Part 5: Vim Keybindings

### 5.1 Modal Editing System

Implement vim-style modal editing with Normal/Insert/Visual modes.

**Location:** New file `internal/tui/vim/mode.go`

```go
package vim

type Mode int

const (
    ModeNormal Mode = iota
    ModeInsert
    ModeVisual
    ModeCommand  // For : commands
)

type VimState struct {
    Mode        Mode
    Register    string    // Yank register
    Count       int       // Numeric prefix (e.g., 5j)
    Pending     string    // Partial command (e.g., "d" waiting for motion)
    LastSearch  string    // For n/N
    Marks       map[rune]Position
    JumpList    []Position
    JumpIndex   int
}

type Position struct {
    Line   int
    Column int
}
```

### 5.2 Keybinding Configuration

Extend `internal/tui/config.go`:

```go
type KeybindingsConfig struct {
    // Existing...
    CommandMode    string `json:"command_mode"`
    Quit           string `json:"quit"`
    CommandPalette CommandPaletteKeys `json:"command_palette"`

    // New vim section
    Vim VimKeybindings `json:"vim"`
}

type VimKeybindings struct {
    Enabled       bool   `json:"enabled"`        // Master toggle
    EscapeInsert  string `json:"escape_insert"`  // Default: "jk" or "jj"
    LeaderKey     string `json:"leader"`         // Default: " " (space)

    // Mode-specific overrides
    Normal map[string]string `json:"normal"`  // e.g., {"<leader>f": "find_file"}
    Insert map[string]string `json:"insert"`
    Visual map[string]string `json:"visual"`
}
```

### 5.3 Normal Mode Keybindings (Chat View)

| Key | Action | Description |
|-----|--------|-------------|
| `j` / `k` | scroll down/up | Move through messages |
| `gg` | go to top | First message |
| `G` | go to bottom | Latest message |
| `Ctrl+d` / `Ctrl+u` | half-page down/up | Fast scroll |
| `/` | search | Search messages |
| `n` / `N` | next/prev match | Navigate search results |
| `y` | yank | Copy selected message |
| `i` | insert mode | Focus input |
| `a` | append | Focus input at end |
| `o` | open below | New message (focus input) |
| `:` | command mode | Open command prompt |
| `<leader>t` | tasks | Switch to Tasks view |
| `<leader>q` | queue | Switch to Queue view |
| `<leader>m` | memory | Switch to Memory view |
| `<leader>s` | sidebar | Toggle sidebar |
| `<leader>p` | palette | Open command palette |

### 5.4 Insert Mode (Input Textarea)

| Key | Action |
|-----|--------|
| `Esc` or `jk` | Return to normal mode |
| `Ctrl+w` | Delete word |
| `Ctrl+u` | Delete to start of line |
| `Ctrl+a` | Go to start of line |
| `Ctrl+e` | Go to end of line |
| Standard typing | Insert text |

### 5.5 Visual Mode (Message Selection)

| Key | Action |
|-----|--------|
| `j` / `k` | Extend selection |
| `y` | Yank selection |
| `Esc` | Exit visual mode |

### 5.6 Command Mode

| Command | Action |
|---------|--------|
| `:q` | Quit |
| `:w` | Save session |
| `:wq` | Save and quit |
| `:set wrap` | Toggle word wrap |
| `:set number` | Toggle line numbers |
| `:clear` | Clear chat history |
| `:session <name>` | Switch session |
| `:task <id>` | View task details |
| `:help` | Show help |

### 5.7 Mode Indicator

Display current mode in status line:

```
┌─ Chat ──────────────────────────── NORMAL ─┐
│ ...                                        │
├────────────────────────────────────────────┤
│ -- INSERT --                               │
│ > _                                        │
└────────────────────────────────────────────┘
```

---

## Part 6: Real-Time Backend Visibility

### 6.1 Bus Event Subscription

Add RPC method for event streaming.

**New RPC Method:** `bus.subscribe`

```go
// internal/rpc/server.go
func (s *Server) handleBusSubscribe(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        Topics []string `json:"topics"`
    }
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }

    // Create subscription
    sub := s.bus.Subscribe("rpc-client", req.Topics...)

    // Return subscription ID and initial state
    return map[string]any{
        "subscription_id": sub.ID,
        "topics":          req.Topics,
    }, nil
}
```

### 6.2 Event Stream in TUI

Create background goroutine for event consumption.

**Location:** `internal/tui/events.go`

```go
type EventStream struct {
    rpc     *RPCClient
    subID   string
    topics  []string
    events  chan BusEvent
    done    chan struct{}
}

type BusEvent struct {
    Topic   string          `json:"topic"`
    Payload json.RawMessage `json:"payload"`
    Time    time.Time       `json:"timestamp"`
}

func (e *EventStream) Start(topics []string) error {
    // Subscribe via RPC
    resp, err := e.rpc.Call("bus.subscribe", map[string]any{
        "topics": topics,
    })
    // ...

    // Poll for events (until we have true streaming)
    go e.pollLoop()
    return nil
}

func (e *EventStream) pollLoop() {
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-e.done:
            return
        case <-ticker.C:
            events, _ := e.rpc.Call("bus.poll", map[string]any{
                "subscription_id": e.subID,
            })
            // Dispatch to e.events channel
        }
    }
}
```

### 6.3 Activity Feed Panel

New panel showing real-time event stream.

```
┌─ Activity Feed ─────────────────────────────┐
│ 19:42:31 agent.action   coder → file_read   │
│ 19:42:32 agent.result   ✓ file_read done    │
│ 19:42:33 queue.claimed  job-123 → worker-1  │
│ 19:42:35 task.update    task-abc → exec     │
│ 19:42:36 agent.action   coder → shell_exec  │
│ 19:42:38 worker.status  idle:2 busy:2       │
└─────────────────────────────────────────────┘
```

### 6.4 Sparkline Metrics

Show activity trends with sparklines.

**Dependency:** `github.com/charmbracelet/bubbles/sparkline` (or custom)

```
┌─ Metrics ───────────────────────────────────┐
│ Queue depth: ▁▂▃▅▆▇█▆▅▃▂▁  peak: 12         │
│ Worker busy: ▃▃▅▅▇▇██▇▅▃▃  avg: 2.3         │
│ Agent iter:  ▁▁▂▃▅▇█▆▃▂▁▁  total: 47        │
│ Memory ops:  ▁▂▁▃▂▁▅▂▁▂▁▁  writes: 8        │
└─────────────────────────────────────────────┘
```

---

## Part 7: UX Improvements

### 7.1 Responsive Layout

Adapt layout based on terminal dimensions.

```go
func (a *App) calculateLayout() Layout {
    switch {
    case a.width < 80:
        return LayoutCompact      // No sidebar, minimal chrome
    case a.width < 120:
        return LayoutStandard     // Sidebar collapsed by default
    default:
        return LayoutWide         // Full sidebar, split panels
    }
}
```

### 7.2 Quick Actions Bar

Context-sensitive action hints at bottom.

```
┌────────────────────────────────────────────────────────────────────┐
│ [Enter] send  [Ctrl+X] palette  [Tab] focus  [/] search  [?] help │
└────────────────────────────────────────────────────────────────────┘
```

Actions change based on current view/mode:
- Chat (normal): `[j/k] navigate  [y] copy  [i] insert  [/] search`
- Chat (insert): `[Esc] normal  [Enter] send  [↑/↓] history`
- Tasks: `[Enter] details  [n] new  [c] cancel  [R] retry`

### 7.3 Session Header Enhancement

Show more context in session header.

```
┌─ Chat: "Implement auth feature" ─ task-0220 ─ coder ─ iter 3/10 ───┐
```

Components:
- Session description (editable)
- Linked task ID (clickable)
- Current agent
- Iteration progress

### 7.4 Message Threading

Group related messages visually.

```
┌────────────────────────────────────────────────────────────────────┐
│ You (14:32)                                                        │
│ │ Implement a login endpoint                                       │
│ └───────────────────────────────────────────────────────────────── │
│ coder (14:32-14:35) [3 iterations]                                 │
│ │ I'll create the login endpoint. First, let me...                 │
│ │ [collapsed: 2 tool calls]                                        │
│ │                                                                  │
│ │ Done! I've created:                                              │
│ │ • `internal/api/login.go` - Handler                              │
│ │ • `internal/auth/jwt.go` - Token generation                      │
│ └───────────────────────────────────────────────────────────────── │
│ You (14:36)                                                        │
│ │ Add password hashing                                             │
```

### 7.5 Notification System

Toast notifications for background events.

```go
type Notification struct {
    Level   NotificationLevel  // Info, Success, Warning, Error
    Title   string
    Message string
    Action  string            // Optional action command
    TTL     time.Duration     // Auto-dismiss after
}

// Display in top-right corner
┌────────────────────────────────────────────────────────────────────┐
│                                    ┌─ Task Completed ─────────────┐│
│                                    │ ✓ task-0220 finished         ││
│                                    │ [Enter] view  [Esc] dismiss  ││
│                                    └───────────────────────────────┘│
```

### 7.6 Fuzzy Finder

Add fuzzy search for commands, sessions, tasks.

**Trigger:** `Ctrl+P` or `<leader>f`

```
┌─ Find ──────────────────────────────────────────────────────────────┐
│ > auth                                                              │
├─────────────────────────────────────────────────────────────────────┤
│ ▶ task-0220... "Implement auth feature" (active)                    │
│   session-03   "Auth debugging session"                             │
│   mem-abc123   "JWT implementation notes"                           │
│   :auth        Command: toggle authentication                       │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Part 8: Implementation Phases

### Phase 1: Foundation (Week 1-2)

**Priority: Critical**

1. **Markdown rendering** (`internal/tui/render/`)
   - [ ] Add glamour dependency
   - [ ] Create MarkdownRenderer
   - [ ] Integrate into ChatModel message rendering
   - [ ] Add rendering cache to ChatMessage

2. **Syntax highlighting**
   - [ ] Add chroma dependency
   - [ ] Configure glamour to use chroma for code blocks
   - [ ] Test with common languages (Go, Python, JS, SQL)

3. **Agent activity panel**
   - [ ] Add AgentActivityPanel to sidebar
   - [ ] Create AgentActivity type
   - [ ] Subscribe to agent.action/result events
   - [ ] Display tool calls in-flight

### Phase 2: Task Integration (Week 2-3)

**Priority: High**

1. **Enhanced tasks view**
   - [ ] Update TasksModel with new columns
   - [ ] Add memory ref indicators
   - [ ] Show assigned agent
   - [ ] Add progress bars

2. **Task detail modal**
   - [ ] Create TaskDetailModal component
   - [ ] Show memory context section
   - [ ] Display job list with status
   - [ ] Add action buttons (cancel, retry, link)

3. **Real-time task updates**
   - [ ] Subscribe to task.* bus events
   - [ ] Update task list on state changes
   - [ ] Show notifications for completions

### Phase 3: Vim Integration (Week 3-4)

**Priority: Medium**

1. **Modal editing system**
   - [ ] Create vim package with Mode, VimState
   - [ ] Implement mode transitions
   - [ ] Add mode indicator to status line

2. **Normal mode**
   - [ ] Implement j/k navigation
   - [ ] Add gg/G, Ctrl+d/u scrolling
   - [ ] Implement search with /
   - [ ] Add yank functionality

3. **Insert mode**
   - [ ] Configure escape sequences (jk, jj)
   - [ ] Add Ctrl+w, Ctrl+u editing
   - [ ] Smooth transition to/from normal

4. **Command mode**
   - [ ] Implement : command prompt
   - [ ] Add core commands (:q, :w, :help)
   - [ ] Add meept-specific commands

### Phase 4: Real-Time Visibility (Week 4-5)

**Priority: Medium**

1. **Bus subscription RPC**
   - [ ] Add bus.subscribe method
   - [ ] Add bus.poll method
   - [ ] Implement subscription cleanup

2. **Event stream**
   - [ ] Create EventStream component
   - [ ] Background polling goroutine
   - [ ] Event dispatch to UI

3. **Activity feed**
   - [ ] Add ActivityFeedPanel
   - [ ] Format events for display
   - [ ] Add filtering by topic

4. **Metrics sparklines**
   - [ ] Implement sparkline component
   - [ ] Track queue depth history
   - [ ] Track worker utilization
   - [ ] Display in sidebar

### Phase 5: Polish (Week 5-6)

**Priority: Low**

1. **Responsive layout**
   - [ ] Implement layout modes
   - [ ] Test at various terminal sizes
   - [ ] Add graceful degradation

2. **Quick actions bar**
   - [ ] Context-sensitive hints
   - [ ] Update per view/mode

3. **Message threading**
   - [ ] Group messages by conversation turn
   - [ ] Collapse tool call details
   - [ ] Visual thread indicators

4. **Notifications**
   - [ ] Toast notification component
   - [ ] Event-triggered notifications
   - [ ] Action support

5. **Fuzzy finder**
   - [ ] Create FuzzyFinder component
   - [ ] Index sessions, tasks, memories
   - [ ] Implement scoring algorithm

---

## Part 9: File Changes Summary

### New Files

| File | Purpose |
|------|---------|
| `internal/tui/render/markdown.go` | Glamour-based markdown rendering |
| `internal/tui/render/syntax.go` | Chroma syntax highlighting |
| `internal/tui/vim/mode.go` | Vim modal editing state |
| `internal/tui/vim/commands.go` | Vim command implementations |
| `internal/tui/vim/keymap.go` | Keybinding definitions |
| `internal/tui/events.go` | Bus event subscription |
| `internal/tui/components/sparkline.go` | Sparkline charts |
| `internal/tui/components/notification.go` | Toast notifications |
| `internal/tui/components/fuzzy.go` | Fuzzy finder |

### Modified Files

| File | Changes |
|------|---------|
| `internal/tui/sidebar.go` | Add AgentActivityPanel, MetricsPanel |
| `internal/tui/models/chat.go` | Markdown rendering, vim mode, threading |
| `internal/tui/models/tasks.go` | New columns, detail modal, memory display |
| `internal/tui/types/types.go` | AgentActivity, BusEvent, ToolCall types |
| `internal/tui/config.go` | VimKeybindings configuration |
| `internal/tui/styles.go` | Code block, syntax colors |
| `internal/tui/app.go` | Vim mode integration, notifications |
| `internal/rpc/proxy.go` | agent.activity, bus.subscribe methods |
| `go.mod` | Add glamour, chroma dependencies |

---

## Part 10: Testing Plan

### Unit Tests

1. **Markdown detection** - Test detectMarkdown with various inputs
2. **Syntax highlighting** - Test language detection, output formatting
3. **Vim state machine** - Test mode transitions, command parsing
4. **Event stream** - Test subscription, polling, dispatch

### Integration Tests

1. **RPC bus subscription** - End-to-end event delivery
2. **Task view updates** - React to task.* events
3. **Agent panel updates** - React to agent.* events

### Manual Testing

1. **Markdown rendering** - Verify rendering of:
   - Code blocks (fenced and indented)
   - Headers, lists, blockquotes
   - Inline code, bold, italic
   - Links (with accessibility)

2. **Vim keybindings** - Test all mappings in each mode

3. **Real-time updates** - Verify:
   - Agent activity appears within 500ms
   - Task state changes reflect immediately
   - No memory leaks in long sessions

### Performance Testing

1. **Large message rendering** - 10KB+ messages with code
2. **Event throughput** - 100+ events/second handling
3. **Memory usage** - Monitor during extended sessions

---

## Appendix A: Dependencies

```go
// go.mod additions
require (
    github.com/charmbracelet/glamour v0.7.0
    github.com/alecthomas/chroma/v2 v2.12.0
)
```

## Appendix B: Configuration Example

```json5
// ~/.meept/client.json5
{
  "keybindings": {
    "command_mode": "ctrl+x",
    "quit": "ctrl+c",
    "vim": {
      "enabled": true,
      "escape_insert": "jk",
      "leader": " ",
      "normal": {
        "<leader>f": "fuzzy_find",
        "<leader>t": "view_tasks",
        "<leader>s": "toggle_sidebar"
      }
    }
  },
  "rendering": {
    "markdown": true,
    "syntax_highlighting": true,
    "theme": "monokai",
    "word_wrap": true
  },
  "activity_feed": {
    "enabled": true,
    "max_events": 100,
    "topics": ["agent.*", "task.*", "worker.*"]
  }
}
```

## Appendix C: Color Scheme

Extend `internal/tui/styles.go`:

```go
// Syntax highlighting colors (monokai-inspired)
var (
    ColorKeyword    = lipgloss.Color("#F92672") // Pink
    ColorString     = lipgloss.Color("#E6DB74") // Yellow
    ColorNumber     = lipgloss.Color("#AE81FF") // Purple
    ColorComment    = lipgloss.Color("#75715E") // Gray
    ColorFunction   = lipgloss.Color("#A6E22E") // Green
    ColorType       = lipgloss.Color("#66D9EF") // Cyan
    ColorOperator   = lipgloss.Color("#F92672") // Pink
)

// Agent state colors
var (
    ColorAgentReasoning = lipgloss.Color("#F59E0B") // Amber (thinking)
    ColorAgentToolExec  = lipgloss.Color("#3B82F6") // Blue (executing)
    ColorAgentWaiting   = lipgloss.Color("#6B7280") // Gray (idle)
    ColorAgentError     = lipgloss.Color("#EF4444") // Red
)

// Task state colors
var (
    ColorTaskPending   = lipgloss.Color("#6B7280") // Gray
    ColorTaskPlanning  = lipgloss.Color("#F59E0B") // Amber
    ColorTaskExecuting = lipgloss.Color("#3B82F6") // Blue
    ColorTaskTesting   = lipgloss.Color("#8B5CF6") // Purple
    ColorTaskCompleted = lipgloss.Color("#10B981") // Green
    ColorTaskFailed    = lipgloss.Color("#EF4444") // Red
)
```
