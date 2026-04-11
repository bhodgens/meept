// Package lite provides a lightweight TUI for meept with shell-like editing.
package lite

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tailscale/hujson"
)

// Config holds the meept-lite configuration.
type Config struct {
	// Model preferences
	DefaultModel string `json:"default_model"`

	// Display preferences
	ShowTokens   bool `json:"show_tokens"`
	ShowCost     bool `json:"show_cost"`
	ShowDuration bool `json:"show_duration"`

	// Behavior
	AutoScroll  bool `json:"auto_scroll"`
	HistorySize int  `json:"history_size"`

	// Keybindings (optional overrides)
	Keybindings map[string]string `json:"keybindings,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		DefaultModel: "",
		ShowTokens:   true,
		ShowCost:     true,
		ShowDuration: true,
		AutoScroll:   true,
		HistorySize:  100,
		Keybindings: map[string]string{
			"menu":             "ctrl+x",
			"quit":             "ctrl+c",
			"scroll_up":        "pgup",
			"scroll_down":      "pgdown",
			"scroll_top":       "ctrl+home",
			"scroll_bottom":    "ctrl+end",
			"history_prev":     "up",
			"history_next":     "down",
			"word_left":        "ctrl+left",
			"word_right":       "ctrl+right",
			"delete_word_back": "ctrl+w",
			"delete_word_fwd":  "alt+d",
			"clear_line":       "ctrl+u",
			"clear_to_end":     "ctrl+k",
		},
	}
}

// configFileName is the name of the config file.
const configFileName = "lite.json5"

// ConfigPath returns the config file path.
func ConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".meept", configFileName)
}

// LoadConfig loads config from disk, returning defaults if not found.
func LoadConfig() (*Config, error) {
	path := ConfigPath()
	if path == "" {
		return DefaultConfig(), nil
	}

	cfg, err := loadConfigFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), nil
		}
		// Return defaults but preserve error for caller awareness
		return DefaultConfig(), err
	}

	return cfg, nil
}

// loadConfigFile loads and parses a JSON5 config file.
func loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Convert JSON5 to standard JSON using hujson
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		// Fall back to treating as standard JSON
		standardJSON = data
	}

	// Start with defaults
	cfg := DefaultConfig()

	// Unmarshal on top of defaults to allow partial configs
	if err := json.Unmarshal(standardJSON, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save saves the config to disk.
func (c *Config) Save() error {
	path := ConfigPath()
	if path == "" {
		return errors.New("unable to determine config path")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Write atomically using temp file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// ReloadConfig reloads config from disk.
func ReloadConfig() (*Config, error) {
	return LoadConfig()
}

// ConfigReloadedMsg is sent when config has been reloaded.
type ConfigReloadedMsg struct {
	Config *Config
	Err    error
}

// EditConfig opens the config file in $EDITOR.
// Returns a tea.Cmd that when executed will open the editor.
func EditConfig() tea.Cmd {
	return func() tea.Msg {
		path := ConfigPath()
		if path == "" {
			return ConfigReloadedMsg{Err: errors.New("unable to determine config path")}
		}

		// Ensure config file exists with defaults if not present
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			if err := cfg.Save(); err != nil {
				return ConfigReloadedMsg{Err: err}
			}
		}

		// Determine editor
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			// Try common editors
			for _, e := range []string{"nano", "vim", "vi"} {
				if _, err := exec.LookPath(e); err == nil {
					editor = e
					break
				}
			}
		}
		if editor == "" {
			return ConfigReloadedMsg{Err: errors.New("no editor found; set $EDITOR")}
		}

		// Run editor
		cmd := exec.Command(editor, path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return ConfigReloadedMsg{Err: err}
		}

		// Reload config after editing
		cfg, err := ReloadConfig()
		return ConfigReloadedMsg{Config: cfg, Err: err}
	}
}

// GetKeybinding returns the keybinding for an action, with fallback to default.
func (c *Config) GetKeybinding(action string) string {
	if c.Keybindings != nil {
		if binding, ok := c.Keybindings[action]; ok {
			return binding
		}
	}
	// Fall back to default
	defaults := DefaultConfig()
	if binding, ok := defaults.Keybindings[action]; ok {
		return binding
	}
	return ""
}

// Merge merges another config into this one.
// Non-zero values from other override values in c.
func (c *Config) Merge(other *Config) {
	if other == nil {
		return
	}

	if other.DefaultModel != "" {
		c.DefaultModel = other.DefaultModel
	}
	// Boolean fields are always merged (since we can't distinguish false from unset)
	// This is handled by unmarshaling on top of defaults in loadConfigFile.

	if other.HistorySize > 0 {
		c.HistorySize = other.HistorySize
	}

	// Merge keybindings
	if other.Keybindings != nil {
		if c.Keybindings == nil {
			c.Keybindings = make(map[string]string)
		}
		for k, v := range other.Keybindings {
			c.Keybindings[k] = v
		}
	}
}

// Clone returns a deep copy of the config.
func (c *Config) Clone() *Config {
	clone := &Config{
		DefaultModel: c.DefaultModel,
		ShowTokens:   c.ShowTokens,
		ShowCost:     c.ShowCost,
		ShowDuration: c.ShowDuration,
		AutoScroll:   c.AutoScroll,
		HistorySize:  c.HistorySize,
	}

	if c.Keybindings != nil {
		clone.Keybindings = make(map[string]string, len(c.Keybindings))
		for k, v := range c.Keybindings {
			clone.Keybindings[k] = v
		}
	}

	return clone
}
