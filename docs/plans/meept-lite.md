# Meept-Lite: Minimalistic Console Client

## Overview

`meept-lite` is a minimalistic alternative to the full Bubble Tea TUI client. It provides a clean, bash-like interface with:

- **Fixed prompt at bottom** - Always visible, even when scrolling
- **Scrollback buffer** - Uses terminal's native scroll buffer
- **Slash commands** - Same as full TUI (`/help`, `/clear`, `/session`, etc.)
- **Ctrl-X key combos** - Same command menus as meept (accessed via `ctrl+x` leader)
- **Session management** - Named sessions via `ctrl-x s` style commands
- **Colored prompt** - `|` orange, `meept` orange, `:` white, `session-name` grey, `#>` white
- **Command menus** - tcell-rendered overlays (modal, blocking input)
- **Slash autocomplete** - Popup box (like full TUI)
- **Pasted text** - Shows `[pasted X lines]` indicator

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│              Shared Library (internal/liteclient/)          │
├─────────────────────────────────────────────────────────────┤
│  client.go         - Transport connection & RPC calls       │
│  slash.go          - Slash command parsing (reuse)          │
│  history.go        - Input history management               │
│  session.go        - Session naming & switching             │
│  keys.go           - Key binding helpers                    │
│  colors.go         - Color definitions (orange/grey)        │
└─────────────────────────────────────────────────────────────┘
                     │
                     ▼
        ┌────────────────────────┐
        │   cmd/meept-lite/      │
        │   main.go + tui.go     │
        │   - tcell terminal     │
        │   - Fixed prompt       │
        │   - Scrollback         │
        └────────────────────────┘
```

## Requirements

### Functional

| ID | Requirement | Priority |
|----|-------------|----------|
| R1 | Prompt format: `| meept:session_name #>` with orange/grey colors | Must |
| R2 | Prompt always fixed at bottom of visible terminal | Must |
| R3 | Scrollback uses native terminal buffer (Page Up/Down, Shift-Page Up/Down) | Must |
| R4 | Slash command parsing (`/help`, `/clear`, `/session`, `/status`, etc.) | Must |
| R5 | Slash command autocomplete popup | Must |
| R6 | Session name via `--session` flag or daemon query | Must |
| R7 | Command mode menus (Ctrl-X patterns for tasks, queue, memory, sessions) | Should |
| R8 | Full transport support (RPC and HTTP) | Must |
| R9 | Same session management as meept (list/create/switch) | Must |
| R10 | Unbounded text entry at prompt | Must |
| R11 | History navigation (↑/↓ for previous commands) | Must |
| R12 | Brace/paste handling (bracketed paste mode) | Should |

### Non-Functional

| ID | Requirement | Rationale |
|----|-------------|-----------|
| N1 | Binary size < 10MB | Minimalistic goal |
| N2 | No Bubble Tea dependency | Simpler terminal control |
| N3 | Reuse transport.Client interface | Consistency with main client |
| N4 | Follow existing code conventions | Go 1.22+, table-driven tests |
| N5 | UI text lowercase (per CLAUDE.md) | Consistency |

## Technical Decisions

### 1. Terminal Library: **tcell**

**Rationale:**
- Direct control over terminal regions (alt screen vs. main buffer)
- Well-maintained, widely used
- Supports bracketed paste mode natively

**Alternatives considered:**
- Bubble Tea: Already used by main TUI, but forces full framework
- termbox-go: Less actively maintained
- Raw ANSI escape codes: Too much manual work

### 2. Prompt Format

```
| meept:session_name #>
```

**Color scheme:**
| Element | Color | Hex |
|---------|-------|-----|
| `|` | Orange | `#F97316` |
| `meept` | Orange | `#F97316` |
| `:` | White | `#E5E7EB` |
| `session_name` | Grey | `#6B7280` |
| ` #>` | White | `#E5E7EB` |

### 3. Session Name Handling

- **Default**: Query daemon for most recent session, fall back to `"default"` if none exists
- **CLI flag**: `meept-lite --session <name>`
- **Runtime**: `/session <name>` command or `ctrl-x s` menu

### 4. Scrollback Behavior

- Use **tcell's alt screen** with manual region management
- Prompt rendered in fixed bottom row
- Scrollback area is everything above prompt
- When user scrolls up, prompt stays visible with indicator: `▼ N lines from bottom`
- Press `End` or `ctrl+g` to return to bottom

### 5. Command Menus (Ctrl-X patterns)

**Command mode entry**: Press `ctrl+x` to enter command mode (like vim's `:` but for meept)

When `ctrl+x` is pressed:
1. Enter "command mode" - display mode indicator
2. Wait for next key
3. Dispatch to appropriate menu handler

Follow patterns from existing TUI (`internal/tui/config.go`):

| Key | Menu | Source |
|-----|------|--------|
| `ctrl+x s` | Sessions | `Keybindings.CommandPalette.Sessions` |
| `ctrl+x t` | Tasks | `Keybindings.CommandPalette.ViewTasks` |
| `ctrl+x q` | Queue | `Keybindings.CommandPalette.ViewQueue` |
| `ctrl+x m` | Memory | `Keybindings.CommandPalette.ViewMemory` |
| `ctrl+x c` | Chat (clear/new) | `Keybindings.CommandPalette.ViewChat` |
| `ctrl+x ctrl+x` | Command palette | double-ctrl-x pattern |

**Menu rendering**: tcell-rendered overlay modals (blocking input)
- Modal centered on screen
- Items listed with key shortcuts
- Navigation: `↑/↓` or `j/k`, `Enter` to select, `Esc` to cancel
- Copied from `internal/tui/modal.go` patterns (SessionPickerModal, ConfirmModal, etc.)

### 6. Ctrl-X Command Mode Implementation

**Source code to reuse/adapt**:
- `internal/tui/config.go` - `KeybindingsConfig`, `CommandPaletteKeys` structs
- `internal/tui/modal.go` - Modal rendering, SessionPickerModal, CommandPaletteModal
- `internal/tui/vim/keymap.go` - Key binding patterns

**State machine**:
```
NORMAL ──ctrl+x──> COMMAND_WAIT ──key──> DISPATCH ──> menu handler
                         │
                         └─ctrl+x──> COMMAND_PALETTE
```

### 7. Text Substitutions

Support same patterns as full meept:
- History expansion: `!!` (last command), `!N` (history item N)
- Slash command autocomplete on `/` prefix
- Pasted text indicator: `[pasted X lines]` when bracketed paste detected

---

## Implementation Phases

### Phase 1: Foundation

**Goal:** Basic working client with prompt and chat

| Task | Description |
|------|-------------|
| P1.1 | Create `internal/liteclient/` directory with shared components |
| P1.2 | Implement `transport.Client` wrapper for common operations |
| P1.3 | Create `cmd/meept-lite/main.go` with tcell initialization |
| P1.4 | Implement fixed prompt rendering at bottom row |
| P1.5 | Implement basic input handling (text entry, enter to send) |
| P1.6 | Basic chat loop: send message → print response to scrollback |

**Acceptance Criteria:**
- `meept-lite` binary builds and runs
- Prompt renders at bottom: `| meept:default #>`
- Typing and pressing Enter sends message and prints response

---

### Phase 2: Slash Commands & History

**Goal:** Full slash command support and history navigation

| Task | Description |
|------|-------------|
| P2.1 | Copy `internal/tui/slash.go` to `internal/liteclient/slash.go` |
| P2.2 | Implement slash command execution handlers |
| P2.3 | Implement history tracking (sent commands) |
| P2.4 | Implement ↑/↓ history navigation |
| P2.5 | Implement bracketed paste mode |

**Acceptance Criteria:**
- `/help` shows available commands
- `/clear` clears scrollback
- ↑/↓ navigates history
- Pasting multi-line text works correctly

---

### Phase 3: Session Management

**Goal:** Session naming and switching

| Task | Description |
|------|-------------|
| P3.1 | Copy session logic from `internal/tui/` to `internal/liteclient/` |
| P3.2 | Query daemon for current session name |
| P3.3 | Implement `--session` CLI flag |
| P3.4 | Implement `/session list/create/switch` commands |
| P3.5 | Update prompt when session changes |

**Acceptance Criteria:**
- `meept-lite --session mysession` starts with named session
- `/session list` shows all sessions
- Prompt updates when session changes

---

### Phase 4: Command Menus (Ctrl-X Patterns)

**Goal:** Vim-style leader key menus with ctrl-x command mode

| Task | Description |
|------|-------------|
| P4.1 | Copy `internal/tui/config.go` keybinding structs to `liteclient/config.go` |
| P4.2 | Implement command mode state machine (NORMAL → COMMAND_WAIT → DISPATCH) |
| P4.3 | Copy modal rendering from `internal/tui/modal.go` (Modal, SessionPickerModal) |
| P4.4 | Implement `ctrl+x s` session menu (with list/create/switch) |
| P4.5 | Implement `ctrl+x t` tasks menu |
| P4.6 | Implement `ctrl+x q` queue menu |
| P4.7 | Implement `ctrl+x m` memory menu |
| P4.8 | Implement `ctrl+x c` chat menu (clear/new) |
| P4.9 | Implement `ctrl+x ctrl+x` command palette |

**Acceptance Criteria:**
- `ctrl+x` enters command mode with visual indicator
- All Ctrl-X patterns open appropriate menus
- Menus render as tcell overlays (modal, blocking)
- Navigation works (↑/↓/Enter/Esc)
- Key shortcuts work (e.g., `1-9` for session selection)

---

### Phase 5: Polish & Edge Cases

**Goal:** Production-ready client

| Task | Description |
|------|-------------|
| P5.1 | Implement scrollback indicator (`▼ N lines from bottom`) |
| P5.2 | Add colors to prompt (orange/grey) |
| P5.3 | Handle window resize |
| P5.4 | Graceful shutdown (save session, cleanup) |
| P5.5 | Error handling (daemon not running, connection lost) |
| P5.6 | Tests for shared components |

**Acceptance Criteria:**
- Prompt colors match spec
- Resize handled gracefully
- All tests pass

---

## File Structure

```
internal/liteclient/
├── client.go           # Transport wrapper
├── slash.go            # Slash command parsing (copied/adapted)
├── slash_autocomplete.go  # Popup autocomplete (copied/adapted)
├── history.go          # Input history
├── session.go          # Session management
├── config.go           # Keybinding structs (copied from internal/tui/config.go)
├── menus/
│   ├── modal.go        # Modal rendering (copied/adapted from internal/tui/modal.go)
│   ├── session.go      # Session picker menu
│   ├── tasks.go        # Tasks menu
│   ├── queue.go        # Queue menu
│   ├── memory.go       # Memory menu
│   └── palette.go      # Command palette
├── colors.go           # Orange/grey palette
└── prompt.go           # Prompt rendering

cmd/meept-lite/
├── main.go             # Entry point, CLI flags
├── tui.go              # Terminal UI (tcell-based)
├── command_mode.go     # Ctrl-X state machine and dispatch
└── handlers.go         # Command handlers

tests/liteclient/
├── slash_test.go       # Slash command tests
├── history_test.go     # History navigation tests
├── command_mode_test.go # Ctrl-X state machine tests
└── integration_test.go # End-to-end client tests
```

---

## Key Bindings

| Key | Action |
|-----|--------|
| `Enter` | Send message / Execute command |
| `↑` / `↓` | History navigation |
| `^X s` | Session menu |
| `^X t` | Tasks menu |
| `^X q` | Queue menu |
| `^X m` | Memory menu |
| `^X c` | Chat commands (new/clear) |
| `^X ^X` | Command palette |
| `^C` | Clear input / Exit (context-dependent) |
| `Page Up` / `Page Down` | Scroll scrollback |
| `Home` / `End` | Jump to top/bottom |
| `^G` | Cancel menu / Return to bottom |

---

## Transport Integration

Reuse existing `internal/transport.Client` interface:

```go
// From internal/transport/client.go
type Client interface {
    Connect() error
    Close() error
    Chat(message, conversationID string) (string, error)
    Status() (*types.DaemonStatusResponse, error)
    ListSessions() (*types.SessionListResponse, error)
    // ... etc
}
```

The lite client uses this directly—no wrapper needed initially.

---

## Slash Command List

Copy from `internal/tui/slash.go`:

| Command | Description |
|---------|-------------|
| `/help` | Show help |
| `/new` | New session |
| `/clear` | Clear scrollback |
| `/retry` | Retry last failed action |
| `/undo` | Undo last action |
| `/usage` | Show token usage |
| `/stop` | Stop current task |
| `/status` | Show daemon status |
| `/vim` | Toggle vim mode (if applicable) |
| `/session` | Session management |
| `/task` | Task operations |
| `/cancel` | Cancel operation |
| `/amend` | Amend last message |
| `/interrupt` | Interrupt agent |
| `/tasks` | View tasks |

---

## Testing Strategy

| Phase | Tests |
|-------|-------|
| P1-P2 | Unit tests for slash parsing, history |
| P3 | Integration tests for session operations |
| P4 | Manual testing for menus (terminal UI) |
| P5 | End-to-end: start client, send messages, switch sessions |

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-------------|
| tcell learning curve | Medium | Follow existing tcell examples; minimal feature set |
| Scrollback + fixed prompt | Medium | Use alt screen; render prompt separately from scrollback |
| Command menu complexity | Low | Simple inline menus first; enhance later |
| Transport incompatibility | Low | Use same Client interface as existing client |

---

## Success Criteria

`meept-lite` is complete when:

1. [ ] All slash commands from full TUI work
2. [ ] Prompt format and colors match spec
3. [ ] Session management fully functional
4. [ ] Command menus (Ctrl-X) work
5. [ ] Scrollback with fixed prompt works correctly
6. [ ] Binary size < 10MB
7. [ ] All tests pass

---

## Future Enhancements

- Syntax highlighting for code blocks in responses
- Configurable prompt format
- Mouse support for menu selection
- Notification badges for background tasks
- Compact mode for narrow terminals

---

## References

- Full TUI: `internal/tui/`
- Transport: `internal/transport/client.go`
- Session types: `internal/tui/types/types.go`
- Slash commands: `internal/tui/slash.go`
