// Package tui provides the terminal user interface for meept.
package tui

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// ClientConfig holds TUI client configuration.
type ClientConfig struct {
	Keybindings   KeybindingsConfig        `json:"keybindings"`
	Session       SessionConfig            `json:"session"`
	Vim           VimConfig                `json:"vim"`
	Rendering     RenderingConfig          `json:"rendering"`
	Input         InputConfig              `json:"input"`
	Chat          ChatConfig               `json:"chat"`
	STT           STTConfig                `json:"stt"`
	TTS           TTSConfig                `json:"tts"`
	Notifications NotificationsClientConfig `json:"notifications"`
}

// NotificationsClientConfig controls TUI-side notification behavior. This is
// independent of the daemon-wide NotificationsConfig (which gates server-side
// event emission). The TUI field governs only local toast suppression.
type NotificationsClientConfig struct {
	// DoNotDisturb suppresses all TUI toast notifications when true.
	DoNotDisturb bool `json:"do_not_disturb"` // default: false
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
	ViewAgents    string `json:"view_agents"`    // Switch to agents/employees view (default: "e")
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

// TTSConfig defines text-to-speech settings for the TUI.
// Note: Only Enabled, Engine, and Voice are configurable in client.json5.
// Playback and Behavior settings come from meept.json5 main config.
type TTSConfig struct {
	Enabled  bool        `json:"enabled"` // Enable text-to-speech (default: false)
	Engine   string      `json:"engine"`  // TTS engine: "piper" or "platform" (default: "piper")
	Voice    string      `json:"voice"`   // Voice identifier (default: "danny-medium")
	Playback TTSPlayback `json:"playback,omitempty"`
	Behavior TTSBehavior `json:"behavior,omitempty"`
}

// TTSPlayback holds TTS playback settings (volume, rate).
type TTSPlayback struct {
	Volume float64 `json:"volume,omitempty"` // 0.0 to 1.0
	Rate   float64 `json:"rate,omitempty"`   // 0.5 to 2.0
}

// TTSBehavior holds TTS behavior settings (interrupt, queue).
type TTSBehavior struct {
	InterruptOnNewMsg bool `json:"interrupt_on_new_msg,omitempty"`
	QueueMessages     bool `json:"queue_messages,omitempty"`
	MaxQueueSize      int  `json:"max_queue_size,omitempty"`
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
				ViewAgents:    "e",
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
		TTS: TTSConfig{
			Enabled: false,
			Engine:  "piper",
			Voice:   "danny-medium",
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
	slog.Warn("client config: no config file found, using all defaults")
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
		slog.Warn("client config: failed to parse JSON5, using defaults", "path", path, "error", err)
		return nil, err
	}

	// Start with defaults
	cfg := DefaultClientConfig()

	// Unmarshal on top of defaults to allow partial configs
	if err := json.Unmarshal(standardJSON, cfg); err != nil {
		slog.Warn("client config: failed to unmarshal, using defaults", "path", path, "error", err)
		return nil, err
	}

	// Warn about fields that may have defaulted due to missing keys
	checkClientConfigDefaults(path, cfg)

	return cfg, nil
}

// checkClientConfigDefaults logs warnings for fields that may have
// silently defaulted when the config file was missing or incomplete.
func checkClientConfigDefaults(path string, cfg *ClientConfig) {
	if cfg.Session.DefaultName == "" {
		slog.Warn("client config: using default for missing field", "field", "session.default_name", "default", "default", "path", path)
		cfg.Session.DefaultName = "default"
	}
	if cfg.Rendering.Theme == "" {
		slog.Warn("client config: using default for missing field", "field", "rendering.theme", "default", "monokai", "path", path)
		cfg.Rendering.Theme = "monokai"
	}
	if cfg.STT.Engine == "" {
		slog.Warn("client config: using default for missing field", "field", "stt.engine", "default", "whisper", "path", path)
		cfg.STT.Engine = "whisper"
	}
	if cfg.STT.Language == "" {
		slog.Warn("client config: using default for missing field", "field", "stt.language", "default", "en", "path", path)
		cfg.STT.Language = "en"
	}
	if cfg.TTS.Engine == "" {
		slog.Warn("client config: using default for missing field", "field", "tts.engine", "default", "piper", "path", path)
		cfg.TTS.Engine = "piper"
	}
	if cfg.TTS.Voice == "" {
		slog.Warn("client config: using default for missing field", "field", "tts.voice", "default", "danny-medium", "path", path)
		cfg.TTS.Voice = "danny-medium"
	}
	if cfg.Chat.Verbosity == "" {
		slog.Warn("client config: using default for missing field", "field", "chat.verbosity", "default", "normal", "path", path)
		cfg.Chat.Verbosity = "normal"
	}
	if cfg.Chat.ScrollSpeed == 0 {
		slog.Warn("client config: using default for missing field", "field", "chat.scroll_speed", "default", 3, "path", path)
		cfg.Chat.ScrollSpeed = 3
	}
	if cfg.Keybindings.CommandMode == "" {
		slog.Warn("client config: using default for missing field", "field", "keybindings.command_mode", "default", "ctrl+x", "path", path)
		cfg.Keybindings.CommandMode = "ctrl+x"
	}
	if cfg.Keybindings.Quit == "" {
		slog.Warn("client config: using default for missing field", "field", "keybindings.quit", "default", "ctrl+c", "path", path)
		cfg.Keybindings.Quit = "ctrl+c"
	}
	if cfg.Keybindings.EscapeBehavior == "" {
		slog.Warn("client config: using default for missing field", "field", "keybindings.escape_behavior", "default", "once", "path", path)
		cfg.Keybindings.EscapeBehavior = "once"
	}
}
