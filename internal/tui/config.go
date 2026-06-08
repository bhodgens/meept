// Package tui provides the terminal user interface for meept.
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// ClientConfig holds TUI client configuration.
type ClientConfig struct {
	Keybindings KeybindingsConfig `json:"keybindings"`
	Session     SessionConfig     `json:"session"`
	Vim         VimConfig         `json:"vim"`
	Rendering   RenderingConfig   `json:"rendering"`
	Input       InputConfig       `json:"input"`
	Chat        ChatConfig        `json:"chat"`
	STT         STTConfig         `json:"stt"`

	// Connection configures how the CLI connects to the daemon.
	// Transport: "rpc", "http", or "auto" (default: "auto" -> rpc)
	// Address: overrides socket path or HTTP endpoint
	Connection ConnectionConfig `json:"connection"`
}

// VimConfig defines vim mode settings.
type VimConfig struct {
	Enabled      bool              `json:"enabled"`       // Enable vim keybindings (default: false)
	EscapeInsert string            `json:"escape_insert"` // Escape sequence from insert mode (default: "jk")
	Leader       string            `json:"leader"`        // Leader key (default: " ")
	Normal       map[string]string `json:"normal"`        // Custom normal mode bindings
	Insert       map[string]string `json:"insert"`        // Custom insert mode bindings
	Visual       map[string]string `json:"visual"`        // Custom visual mode bindings
}

// RenderingConfig defines rendering settings.
type RenderingConfig struct {
	Markdown           bool          `json:"markdown"`            // Enable markdown rendering (default: true)
	SyntaxHighlighting bool          `json:"syntax_highlighting"` // Enable syntax highlighting (default: true)
	Theme              string        `json:"theme"`               // Syntax theme (default: "monokai")
	WordWrap           bool          `json:"word_wrap"`           // Enable word wrap (default: true)
	ShowHeader         bool          `json:"show_header"`         // Show header bar with session info (default: true)
	SidebarAnimation   bool          `json:"sidebar_animation"`   // Enable animated dispatch visualization in sidebar (default: true)
	Sidebar            SidebarConfig `json:"sidebar"`             // Sidebar panel configuration
}

// InputConfig defines input textarea behavior settings.
// Note: EnterBehavior and AutoExpand were deprecated in favor of hardcoded behavior;
// Enter always sends, Shift+Enter inserts newline, and input has fixed height.
type InputConfig struct {
	// Deprecated: these fields were used in earlier versions but are now ignored.
	// They are retained only for config file backwards-compatibility.
	EnterBehavior string `json:"enter_behavior"`
	AutoExpand    bool   `json:"auto_expand"`
}

// ChatConfig defines chat viewport behavior settings.
type ChatConfig struct {
	AutoCopyOnRelease bool   `json:"auto_copy_on_release"` // Auto-copy selected text on mouse release (default: false)
	ScrollSpeed       int    `json:"scroll_speed"`         // Lines to scroll per mouse wheel event (default: 3)
	Verbosity         string `json:"verbosity"`            // Progress verbosity: "quiet", "normal", "verbose" (default: "normal")
}

// SidebarConfig defines sidebar panel settings.
type SidebarConfig struct {
	ShowMetrics      bool `json:"show_metrics"`       // Show metrics sparklines panel (default: true)
	ShowActivityFeed bool `json:"show_activity_feed"` // Show activity feed panel (default: true)
	DefaultPanel     int  `json:"default_panel"`      // Default expanded panel index (0=status, default: 0)
	MetricsHistory   int  `json:"metrics_history"`    // Number of data points for sparklines (default: 30)
	ActivityFeedSize int  `json:"activity_feed_size"` // Max events in activity feed (default: 50)
}

// KeybindingsConfig defines customizable key bindings.
type KeybindingsConfig struct {
	CommandMode    string             `json:"command_mode"`    // Key to enter command mode (default: "ctrl+x")
	Quit           string             `json:"quit"`            // Key to quit (default: "ctrl+c")
	EscapeBehavior string             `json:"escape_behavior"` // "once", "twice", or "off" for clearing input (default: "once")
	CommandPalette CommandPaletteKeys `json:"command_palette"` // Keys within command palette
}

// CommandPaletteKeys defines keys for command palette actions.
type CommandPaletteKeys struct {
	ViewChat      string `json:"view_chat"`      // Switch to chat view (default: "c")
	ViewSessions  string `json:"view_sessions"`  // Switch to sessions view (default: "s")
	ViewTasks     string `json:"view_tasks"`     // Switch to tasks view (default: "t")
	ViewQueue     string `json:"view_queue"`     // Switch to queue view (default: "q")
	ViewMemory    string `json:"view_memory"`    // Switch to memory view (default: "m")
	ViewPlans     string `json:"view_plans"`     // Switch to plans view (default: "p")
	Sidebar       string `json:"sidebar"`        // Toggle sidebar (default: "y")
	Sessions      string `json:"sessions"`       // Open session picker (default: "shift+s")
	NewSession    string `json:"new_session"`    // Create new session (default: "n")
	RenameSession string `json:"rename_session"` // Rename current session (default: "r")
	Projects      string `json:"projects"`       // Open projects dialog (default: "o")
}

// SessionConfig defines session behavior settings.
type SessionConfig struct {
	AutoResume  bool   `json:"auto_resume"`  // Auto-resume last session on startup (default: true)
	DefaultName string `json:"default_name"` // Default name for new sessions (default: "default")
}

// STTConfig defines speech-to-text settings for the TUI.
type STTConfig struct {
	Enabled  bool   `json:"enabled"`   // Enable speech-to-text (default: false)
	Engine   string `json:"engine"`    // Transcription engine: "whisper", "parakeet", or "native" (default: "whisper")
	Language string `json:"language"`  // Language code for transcription (default: "en")
	AutoSend bool   `json:"auto_send"` // Send transcription result immediately (default: false)
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Keybindings: KeybindingsConfig{
			CommandMode:    "ctrl+x",
			Quit:           "ctrl+c",
			EscapeBehavior: "once",
			CommandPalette: CommandPaletteKeys{
				ViewChat:      "c",
				ViewSessions:  "s",
				ViewTasks:     "t",
				ViewQueue:     "q",
				ViewMemory:    "m",
				ViewPlans:     "p",
				Sidebar:       "y",
				Sessions:      "S",
				NewSession:    "n",
				RenameSession: "r",
				Projects:      "o",
			},
		},
		Session: SessionConfig{
			AutoResume:  true,
			DefaultName: "default",
		},
		Vim: VimConfig{
			Enabled:      false,
			EscapeInsert: "jk",
			Leader:       " ",
		},
		Rendering: RenderingConfig{
			Markdown:           true,
			SyntaxHighlighting: true,
			Theme:              "monokai",
			WordWrap:           true,
			ShowHeader:         true,
			SidebarAnimation:   true,
			Sidebar: SidebarConfig{
				ShowMetrics:      true,
				ShowActivityFeed: true,
				DefaultPanel:     0,
				MetricsHistory:   30,
				ActivityFeedSize: 50,
			},
		},
		Input: InputConfig{
			EnterBehavior: "", // Empty = original behavior (Enter sends message)
			AutoExpand:    false,
		},
		Chat: ChatConfig{
			Verbosity: "normal",
		},
		STT: STTConfig{
			Engine:   "whisper",
			Language: "en",
		},
	}
}

// LoadClientConfig loads client configuration from disk.
// It searches in order:
// 1. .meept/client.json5 (project-local)
// 2. ~/.meept/client.json5 (user-global)
// Falls back to defaults if no config file is found.
func LoadClientConfig() (*ClientConfig, error) {
	// Try project-local first
	localPath := ".meept/client.json5"
	if cfg, err := loadConfigFile(localPath); err == nil {
		return cfg, nil
	}

	// Try user home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		homePath := filepath.Join(homeDir, ".meept", "client.json5")
		if cfg, err := loadConfigFile(homePath); err == nil {
			return cfg, nil
		}
	}

	// Return defaults
	return DefaultClientConfig(), nil
}

// loadConfigFile loads and parses a JSON5 config file.
func loadConfigFile(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Convert JSON5 to standard JSON using hujson
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil, err
	}

	// Start with defaults
	cfg := DefaultClientConfig()

	// Unmarshal on top of defaults to allow partial configs
	if err := json.Unmarshal(standardJSON, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
