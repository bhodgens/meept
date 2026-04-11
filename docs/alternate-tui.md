# Draft: Alternate TUI Plan

## Requirements (from user)

### Core Concept
- **Hybrid CLI/terminal**: Interactive bash-like keyboard capability
- **Looks like normal shell prompt** - no special libraries for output
- **Built-in scrollback**: Page backwards by response block (like bash history)

### Prompt Design
- **2-line prompt** (like bash):
  - Line 1: Model/token usage/duration/etc. information
  - Line 2: Prompt input area
- **Shell-like navigation**:
  - Ctrl+Left/Right: Move by words
  - Standard bash keybindings
- **Agent information**: Display below prompt area (like Hermes)

### Menu System
- **Special command sequence**: Ctrl+X or `/` to open menu
- **Keyboard navigation**: Quick access to sessions, tasks, agent status
- **Ctrl key combos**: Can overwrite terminal temporarily
- **Unified hierarchy**:
  - 2-level deep: menu/submenu/option
  - Integrate TUI configuration editor

### Shell Expansion
- **Slash commands**: Expand with shell features
  - Inherit `~` expansion
  - File path index/completion
  - `/` for commands
- **Hermes inspiration**: Steal how Hermes does this

### Key Features
- Sessions management
- Tasks management
- Agent status display
- Configuration editor
- Scrollback buffer

## Research Findings

### Hermes Agent

**Source:** https://github.com/nousresearch/hermes-agent

Hermes is Nous Research's self-improving AI agent with a full terminal TUI. Key patterns to borrow:

**UI Layout (3 components):**
1. Welcome banner (model, terminal backend, working dir, tools, skills)
2. Conversation area (streaming output with tool execution feedback)
3. Fixed input prompt with **status bar above it**

**Status Bar Design:**
```
+---------------------------------------------------------------------+
| claude-sonnet | 2.3k/128k [--------] 18% | $0.02 | 3m42s           |
+---------------------------------------------------------------------+
```
- Model name, token usage (current/max), context fill bar with color coding, cost, duration
- Color thresholds: Green <50%, Yellow 50-80%, Orange 80-95%, Red >=95%
- Responsive: full at >=76 cols, compact at 52-75, minimal below 52

**Keybindings:**
| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Alt+Enter` / `Ctrl+J` | New line (multiline input) |
| `Ctrl+C` | Interrupt agent (double-press to force exit) |
| `Ctrl+D` | Exit |
| `Tab` | Autocomplete slash commands |

**Slash Commands with Autocomplete:**
- `/help`, `/model`, `/tools`, `/skills`
- `/new` / `/reset` - fresh conversation
- `/retry`, `/undo` - redo last turn
- `/compress`, `/usage` - context management
- `/background <prompt>` - isolated background session
- `/<skill-name>` - invoke installed skill

**Agent Status Display:**
- Thinking animation with elapsed time during API calls
- Tool execution feed with icons: `terminal`, `search`, `extract`
- Progress modes: off -> new -> all -> verbose

### Current Meept TUI

**Library Stack:**
- BubbleTea v1.3.10 (MVU pattern)
- Bubbles (textarea, viewport components)
- Lipgloss (styling)
- Glamour (markdown rendering)

**Key Components:**
| Component | Implementation |
|-----------|----------------|
| Scrollback | BubbleTea viewport - full message history in memory |
| Input | Bubbles textarea (3 lines, Enter sends, Shift+Enter newline) |
| Daemon Comms | Unix socket + JSON-RPC 2.0 (length-prefixed) |
| Keybindings | Configurable via `~/.meept/client.json5` |
| Menus | Ctrl+X command palette (modal-based) |
| Vim Mode | Optional, opt-in via config |

**What EXISTS:**
- Viewport scrollback (j/k, Ctrl+D/U, mouse wheel)
- Command palette (Ctrl+X) with 8 items
- Session management (create/list/switch/rename/delete)
- Task management UI with filters
- Mouse text selection with auto-copy
- Markdown + syntax highlighting
- Configurable keybindings

**What does NOT exist:**
- Slash commands (/) in TUI input
- In-TUI configuration editor
- Paste detection ("[pasted X lines]" format)
- Shell expansion (~) in input
- 2-line prompt design

### Slash Command Status

**Dispatcher level:** Slash commands exist for skill invocation (`/skill-name args`)
**TUI level:** NOT implemented - input goes directly to chat, no prefix parsing

## Technical Decisions

### 1. TUI Library Choice

**Decision**: BubbleTea with custom input component

**Rationale:**
- Already used in current meept TUI - team familiarity
- Supports viewport scrollback, textarea input
- Bubbles components provide building blocks
- Lipgloss for styling, Glamour for markdown

### 2. Scrollback Implementation
**Decision**: Use same mechanism as existing meept client
**Binary Name**: meept-lite (new lightweight CLI)
**Implication**: Must explore current meept client's scrollback mechanism to understand architecture

### 3. Menu Activation
**Decision**: Both Ctrl+X and `/`
**Context Detection**:
- `/` activates menu only when at start of line (position 0)
- Otherwise `/` is just a character input
- Ctrl+X always activates menu

### 4. Implementation Priority
**Decision**: Full feature parity from first iteration
**Scope**: All features implemented together, not phased

### 5. Architecture Approach
**Decision**: New binary `meept-lite`
**Reuses**: Meept client's scrollback mechanism as functional framework
**Presentation**: New/different, but functional layer similar

### 6. Keybindings

| Key | Action |
|-----|--------|
| Enter | Send message |
| Alt+Enter / Ctrl+J | Insert newline |
| Ctrl+C | Interrupt (2x to quit) |
| Ctrl+D | Exit |
| Ctrl+X | Open menu |
| Tab | Autocomplete slash command |
| Ctrl+Left/Right | Move by word |
| Ctrl+A / Ctrl+E | Start/end of line |
| Ctrl+K | Delete to end |
| Ctrl+U | Delete to start |
| Up/Down | Input history |
| Page Up/Down | Scroll output |

### 7. Prompt Layout

```
+-----------------------------------------------------------+
| claude-sonnet | 2.3k tokens | $0.02 | 3m42s              |  <- Line 1 (configurable)
+-----------------------------------------------------------+
| > _                                                       |  <- Line 2 (input)
+-----------------------------------------------------------+

Agent info displays below prompt when active:
+-----------------------------------------------------------+
| thinking... (2.3s) | running shell command                |
+-----------------------------------------------------------+
```

### 8. Slash Commands

Core built-in commands (type `/` at prompt start):
- `/help` - Show available commands
- `/new` / `/clear` - Start fresh conversation
- `/model [name]` - Show/change model
- `/retry` - Retry last response
- `/undo` - Remove last exchange
- `/usage` - Show token/cost stats
- `/session [name]` - List/switch sessions
- `/task [id]` - List/view tasks
- `/<skill-name>` - Invoke installed skill

Tab autocomplete for command names.

### 9. Menu Categories (Ctrl+X)

Extend current TUI palette:

| Key | Category | Items |
|-----|----------|-------|
| 1 | Views | Chat, Tasks, Queue, Memory |
| 2 | Sessions | List, New, Rename, Delete |
| 3 | Agent | Status, Stop, Model |
| 4 | Tasks | List, Create, Cancel |
| 5 | Memory | Search, Recent, Clear |
| 6 | Config | Edit, Reload |
| y | Sidebar | Toggle visibility |
| ? | Help | Keybindings, Commands |

## Scope Boundaries

### INCLUDE:
- Interactive prompt with shell-like editing
- 2-line prompt with metadata + input
- Built-in scrollback navigation
- Keyboard-driven menu system
- Unified slash command hierarchy
- Agent status display
- Session/task navigation
- Configuration editor integration
- Mouse text selection only

### EXCLUDE (explicitly out of scope):
- Full terminal emulator (just prompt area)
- Mouse navigation (selection only, no clicking)
- Color theming system (use terminal defaults)
- Multi-pane layout (simple prompt-only)
- Remote sessions (local only for now)
- Shell expansion (~, paths) - deferred
- Paste detection ("[pasted X lines]") - deferred
