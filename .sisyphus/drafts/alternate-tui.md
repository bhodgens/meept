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
  - Max 100 total menus (10 categories × 10 menus)
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

### Hermes Agent (awaiting librarian results)
*To be filled from bg_08142abc*

### Current Meept TUI (awaiting explore results)
*To be filled from bg_9ccd5f28*

## Technical Decisions

### 1. TUI Library Choice
**User Requirements**:
- Bash-like navigation: Ctrl+Left/Right for words, full shell precision
- Mouse cursor navigation at prompt
- Paste handling: "[pasted X lines]" format
- Shell-like editing experience

**Options to Research**:
- BubbleTea with custom input component
- tview with input field customization
- go-readline-ny with enhancements
- Custom implementation on top of termbox/termenv

**Needs Further Research**: Which library supports mouse cursor positioning at prompt AND paste detection with line count formatting?

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

## Technical Decisions

*To be filled after research and discussion*

## Scope Boundaries

### INCLUDE:
- Interactive prompt with shell-like editing
- 2-line prompt with metadata + input
- Built-in scrollback navigation
- Keyboard-driven menu system
- Unified slash command hierarchy
- Shell expansion (~, paths)
- Agent status display
- Session/task navigation
- Configuration editor integration

### EXCLUDE (explicitly out of scope):
- Full terminal emulator (just prompt area)
- Mouse support (keyboard-only initially)
- Color theming system (use terminal defaults)
- Multi-pane layout (simple prompt-only)
- Remote sessions (local only for now)