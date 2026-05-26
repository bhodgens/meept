package vim

// Keymap holds the key bindings for each mode.
type Keymap struct {
	Normal  map[string]ActionType
	Insert  map[string]ActionType
	Visual  map[string]ActionType
	Command map[string]ActionType
	Leader  map[string]ActionType
}

// DefaultKeymap returns the default vim keybindings.
func DefaultKeymap() *Keymap {
	return &Keymap{
		Normal: map[string]ActionType{
			"j":      ActionMoveDown,
			"k":      ActionMoveUp,
			"h":      ActionMoveLeft,
			"l":      ActionMoveRight,
			"down":   ActionMoveDown,
			"up":     ActionMoveUp,
			"left":   ActionMoveLeft,
			"right":  ActionMoveRight,
			"G":      ActionMoveToBottom,
			"ctrl+d": ActionHalfPageDown,
			"ctrl+u": ActionHalfPageUp,
			"/":      ActionSearch,
			"n":      ActionSearchNext,
			"N":      ActionSearchPrev,
			"y":      ActionYank,
			"p":      ActionPaste,
			"u":      ActionUndo,
			"ctrl+r": ActionRedo,
			"i":      ActionFocusInput,
			"a":      ActionFocusInput,
			"o":      ActionFocusInput,
			"r":      ActionRefresh,
		},
		Insert: map[string]ActionType{
			"esc":    ActionModeChange,
			"ctrl+w": ActionDelete, // Delete word
			"ctrl+u": ActionDelete, // Delete to start
			"ctrl+a": ActionMoveLeft,
			"ctrl+e": ActionMoveRight,
		},
		Visual: map[string]ActionType{
			"esc":  ActionModeChange,
			"j":    ActionMoveDown,
			"k":    ActionMoveUp,
			"y":    ActionYank,
			"d":    ActionDelete,
			"down": ActionMoveDown,
			"up":   ActionMoveUp,
		},
		Command: map[string]ActionType{
			"esc":       ActionModeChange,
			"enter":     ActionCommand,
			"backspace": ActionNone,
		},
		Leader: map[string]ActionType{
			"t": ActionViewTasks,
			"q": ActionViewQueue,
			"m": ActionViewMemory,
			"c": ActionViewChat,
			"s": ActionToggleSidebar,
			"p": ActionOpenPalette,
			"w": ActionSave,
		},
	}
}

// VimConfig holds vim configuration options.
type VimConfig struct { //nolint:revive // stutter with package name is intentional for API clarity
	Enabled        bool              `json:"enabled"`
	EscapeInsert   string            `json:"escape_insert"` // e.g., "jk" or "jj"
	Leader         string            `json:"leader"`        // Default: " "
	NormalBindings map[string]string `json:"normal"`        // Custom normal mode bindings
	InsertBindings map[string]string `json:"insert"`        // Custom insert mode bindings
	VisualBindings map[string]string `json:"visual"`        // Custom visual mode bindings
}

// DefaultVimConfig returns the default vim configuration.
func DefaultVimConfig() VimConfig {
	return VimConfig{
		Enabled:      false,
		EscapeInsert: "jk",
		Leader:       " ",
	}
}

// HelpText returns vim keybinding help text.
func HelpText() string {
	return `Vim Keybindings (when enabled)

NORMAL MODE
  j/k, down/up    Move down/up
  gg              Go to top
  G               Go to bottom
  Ctrl+d/u        Half page down/up
  /               Search
  n/N             Next/previous search result
  y               Yank (copy) selected
  p               Paste
  i, a, o         Enter INSERT mode
  v               Enter VISUAL mode
  :               Enter COMMAND mode
  <leader>t       View tasks
  <leader>q       View queue
  <leader>m       View memory
  <leader>s       Toggle sidebar
  <leader>p       Command palette
  r               Refresh

INSERT MODE
  Esc, jk         Return to NORMAL mode
  Ctrl+w          Delete word
  Ctrl+u          Delete to start of line
  Ctrl+a          Move to start
  Ctrl+e          Move to end

VISUAL MODE
  j/k             Extend selection
  y               Yank selection
  d               Delete selection
  Esc             Exit to NORMAL mode

COMMAND MODE (:)
  :q, :quit       Quit
  :w, :write      Save
  :wq             Save and quit
  :clear          Clear chat
  :session <name> Switch session
  :task <id>      View task
  :help           Show help
`
}
