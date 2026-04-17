// Package vim provides vim-style modal editing for the TUI.
package vim

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Mode represents a vim editing mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual
	ModeCommand
)

// String returns the mode name.
func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeCommand:
		return "COMMAND"
	default:
		return "UNKNOWN"
	}
}

// State holds the vim editing state.
type State struct {
	Mode        Mode
	Enabled     bool
	Register    string    // Yank register content
	Count       int       // Numeric prefix (e.g., 5j)
	Pending     string    // Partial command (e.g., "d" waiting for motion)
	LastSearch  string    // For n/N search navigation
	LastKey     string    // Last key pressed (for escape sequences)
	LastKeyTime time.Time // Time of last key press

	// Configuration
	EscapeSequence string // e.g., "jk" or "jj" to exit insert mode
	EscapeTimeout  time.Duration
	LeaderKey      string // Default " " (space)
}

// NewState creates a new vim state with defaults.
func NewState() *State {
	return &State{
		Mode:           ModeNormal,
		Enabled:        false, // Disabled by default
		EscapeSequence: "jk",
		EscapeTimeout:  300 * time.Millisecond,
		LeaderKey:      " ",
	}
}

// Action represents a vim action to be performed.
type Action struct {
	Type    ActionType
	Target  string // For commands like :q, store the command
	Count   int    // Repeat count
	Payload string // Additional data
}

// ActionType represents the type of vim action.
type ActionType int

const (
	ActionNone ActionType = iota
	ActionModeChange
	ActionMoveUp
	ActionMoveDown
	ActionMoveLeft
	ActionMoveRight
	ActionMoveToTop
	ActionMoveToBottom
	ActionHalfPageUp
	ActionHalfPageDown
	ActionSearch
	ActionSearchNext
	ActionSearchPrev
	ActionYank
	ActionPaste
	ActionDelete
	ActionUndo
	ActionRedo
	ActionCommand
	ActionQuit
	ActionSave
	ActionFocusInput
	ActionFocusViewport
	ActionToggleSidebar
	ActionOpenPalette
	ActionViewTasks
	ActionViewQueue
	ActionViewMemory
	ActionViewChat
	ActionClearChat
	ActionRefresh
	ActionMoveStartOfLine
)

// ModeChangeMsg signals a mode change.
type ModeChangeMsg struct {
	From Mode
	To   Mode
}

// VimActionMsg carries a vim action.
type VimActionMsg struct {
	Action Action
}

// HandleKey processes a key press and returns any resulting action.
func (s *State) HandleKey(msg tea.KeyMsg) (Action, bool) {
	if !s.Enabled {
		return Action{Type: ActionNone}, false
	}

	key := msg.String()

	switch s.Mode {
	case ModeNormal:
		return s.handleNormalMode(key, msg)
	case ModeInsert:
		return s.handleInsertMode(key, msg)
	case ModeVisual:
		return s.handleVisualMode(key, msg)
	case ModeCommand:
		return s.handleCommandMode(key, msg)
	}

	return Action{Type: ActionNone}, false
}

func (s *State) handleNormalMode(key string, msg tea.KeyMsg) (Action, bool) {
	// Check for leader key sequences
	if s.Pending == s.LeaderKey {
		action := s.handleLeaderSequence(key)
		s.Pending = ""
		if action.Type != ActionNone {
			return action, true
		}
	}

	// Handle numeric prefix
	if key >= "0" && key <= "9" && s.Pending == "" {
		if s.Count == 0 && key == "0" {
			// "0" with no accumulated count jumps to the start of the line.
			return Action{Type: ActionMoveStartOfLine, Count: 1}, true
		}
		s.Count = s.Count*10 + int(key[0]-'0')
		return Action{Type: ActionNone}, true
	}

	// Get repeat count (default 1)
	count := s.Count
	if count == 0 {
		count = 1
	}

	// Handle multi-character commands
	switch s.Pending {
	case "g":
		s.Pending = ""
		switch key {
		case "g":
			return Action{Type: ActionMoveToTop, Count: count}, true
		}
		return Action{Type: ActionNone}, false

	case "d":
		s.Pending = ""
		switch key {
		case "d":
			return Action{Type: ActionDelete, Count: count}, true
		}
		return Action{Type: ActionNone}, false
	}

	// Single character commands
	switch key {
	case "j", "down":
		s.Count = 0
		return Action{Type: ActionMoveDown, Count: count}, true

	case "k", "up":
		s.Count = 0
		return Action{Type: ActionMoveUp, Count: count}, true

	case "h", "left":
		s.Count = 0
		return Action{Type: ActionMoveLeft, Count: count}, true

	case "l", "right":
		s.Count = 0
		return Action{Type: ActionMoveRight, Count: count}, true

	case "g":
		s.Pending = "g"
		return Action{Type: ActionNone}, true

	case "G":
		s.Count = 0
		return Action{Type: ActionMoveToBottom, Count: count}, true

	case "ctrl+d":
		s.Count = 0
		return Action{Type: ActionHalfPageDown, Count: count}, true

	case "ctrl+u":
		s.Count = 0
		return Action{Type: ActionHalfPageUp, Count: count}, true

	case "/":
		s.Count = 0
		return Action{Type: ActionSearch}, true

	case "n":
		s.Count = 0
		return Action{Type: ActionSearchNext, Count: count}, true

	case "N":
		s.Count = 0
		return Action{Type: ActionSearchPrev, Count: count}, true

	case "y":
		s.Count = 0
		return Action{Type: ActionYank}, true

	case "p":
		s.Count = 0
		return Action{Type: ActionPaste, Count: count}, true

	case "d":
		s.Pending = "d"
		return Action{Type: ActionNone}, true

	case "u":
		s.Count = 0
		return Action{Type: ActionUndo, Count: count}, true

	case "ctrl+r":
		s.Count = 0
		return Action{Type: ActionRedo, Count: count}, true

	case "i":
		s.Count = 0
		s.Mode = ModeInsert
		return Action{Type: ActionModeChange, Target: "insert"}, true

	case "a":
		s.Count = 0
		s.Mode = ModeInsert
		return Action{Type: ActionFocusInput}, true

	case "o":
		s.Count = 0
		s.Mode = ModeInsert
		return Action{Type: ActionFocusInput}, true

	case "v":
		s.Count = 0
		s.Mode = ModeVisual
		return Action{Type: ActionModeChange, Target: "visual"}, true

	case ":":
		s.Count = 0
		s.Mode = ModeCommand
		s.Pending = ""
		return Action{Type: ActionModeChange, Target: "command"}, true

	case s.LeaderKey:
		s.Pending = s.LeaderKey
		return Action{Type: ActionNone}, true

	case "esc":
		s.Count = 0
		s.Pending = ""
		return Action{Type: ActionNone}, true

	case "r":
		return Action{Type: ActionRefresh}, true
	}

	return Action{Type: ActionNone}, false
}

func (s *State) handleLeaderSequence(key string) Action {
	switch key {
	case "t":
		return Action{Type: ActionViewTasks}
	case "q":
		return Action{Type: ActionViewQueue}
	case "m":
		return Action{Type: ActionViewMemory}
	case "c":
		return Action{Type: ActionViewChat}
	case "s":
		return Action{Type: ActionToggleSidebar}
	case "p":
		return Action{Type: ActionOpenPalette}
	case "w":
		return Action{Type: ActionSave}
	}
	return Action{Type: ActionNone}
}

func (s *State) handleInsertMode(key string, msg tea.KeyMsg) (Action, bool) {
	// Check escape sequence (e.g., "jk")
	if len(s.EscapeSequence) == 2 {
		now := time.Now()
		if s.LastKey != "" && now.Sub(s.LastKeyTime) < s.EscapeTimeout {
			combined := s.LastKey + key
			if combined == s.EscapeSequence {
				s.Mode = ModeNormal
				s.LastKey = ""
				return Action{Type: ActionModeChange, Target: "normal", Payload: "escape_seq"}, true
			}
		}
		s.LastKey = key
		s.LastKeyTime = now
	}

	// Standard escape
	if key == "esc" {
		s.Mode = ModeNormal
		s.LastKey = ""
		return Action{Type: ActionModeChange, Target: "normal"}, true
	}

	// Pass through to normal input handling
	return Action{Type: ActionNone}, false
}

func (s *State) handleVisualMode(key string, msg tea.KeyMsg) (Action, bool) {
	switch key {
	case "esc":
		s.Mode = ModeNormal
		return Action{Type: ActionModeChange, Target: "normal"}, true

	case "j", "down":
		return Action{Type: ActionMoveDown, Count: 1}, true

	case "k", "up":
		return Action{Type: ActionMoveUp, Count: 1}, true

	case "y":
		s.Mode = ModeNormal
		return Action{Type: ActionYank}, true

	case "d":
		s.Mode = ModeNormal
		return Action{Type: ActionDelete}, true
	}

	return Action{Type: ActionNone}, false
}

func (s *State) handleCommandMode(key string, msg tea.KeyMsg) (Action, bool) {
	switch key {
	case "esc":
		s.Mode = ModeNormal
		s.Pending = ""
		return Action{Type: ActionModeChange, Target: "normal"}, true

	case "enter":
		cmd := s.Pending
		s.Mode = ModeNormal
		s.Pending = ""
		return s.executeCommand(cmd), true

	case "backspace":
		if len(s.Pending) > 0 {
			s.Pending = s.Pending[:len(s.Pending)-1]
		}
		return Action{Type: ActionNone}, true

	default:
		// Accumulate command
		if msg.Type == tea.KeyRunes {
			s.Pending += key
		}
		return Action{Type: ActionNone}, true
	}
}

func (s *State) executeCommand(cmd string) Action {
	cmd = strings.TrimSpace(cmd)

	switch cmd {
	case "q", "quit":
		return Action{Type: ActionQuit}
	case "w", "write":
		return Action{Type: ActionSave}
	case "wq":
		return Action{Type: ActionCommand, Target: "save_quit"}
	case "clear":
		return Action{Type: ActionClearChat}
	case "set wrap":
		return Action{Type: ActionCommand, Target: "toggle_wrap"}
	case "help":
		return Action{Type: ActionCommand, Target: "help"}
	}

	// Handle :session <name> commands
	if strings.HasPrefix(cmd, "session ") {
		name := strings.TrimPrefix(cmd, "session ")
		return Action{Type: ActionCommand, Target: "session", Payload: name}
	}

	// Handle :task <id> commands
	if strings.HasPrefix(cmd, "task ") {
		id := strings.TrimPrefix(cmd, "task ")
		return Action{Type: ActionCommand, Target: "task", Payload: id}
	}

	return Action{Type: ActionNone}
}

// SetMode changes the vim mode.
func (s *State) SetMode(mode Mode) {
	s.Mode = mode
	s.Count = 0
	s.Pending = ""
}

// Toggle enables or disables vim mode.
func (s *State) Toggle() {
	s.Enabled = !s.Enabled
	if s.Enabled {
		s.Mode = ModeNormal
	}
}

// Enable turns on vim mode.
func (s *State) Enable() {
	s.Enabled = true
	s.Mode = ModeNormal
}

// Disable turns off vim mode.
func (s *State) Disable() {
	s.Enabled = false
}

// CommandBuffer returns the current command being typed (for display).
func (s *State) CommandBuffer() string {
	if s.Mode == ModeCommand {
		return ":" + s.Pending
	}
	return ""
}

// StatusLine returns information for the status line.
func (s *State) StatusLine() string {
	if !s.Enabled {
		return ""
	}

	switch s.Mode {
	case ModeInsert:
		return "-- INSERT --"
	case ModeVisual:
		return "-- VISUAL --"
	case ModeCommand:
		return ":" + s.Pending
	case ModeNormal:
		if s.Pending != "" {
			return s.Pending
		}
		if s.Count > 0 {
			return string(rune('0' + s.Count))
		}
	}
	return ""
}
