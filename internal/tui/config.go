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
}

// KeybindingsConfig defines customizable key bindings.
type KeybindingsConfig struct {
	CommandMode    string             `json:"command_mode"`    // Key to enter command mode (default: "ctrl+x")
	Quit           string             `json:"quit"`            // Key to quit (default: "ctrl+c")
	CommandPalette CommandPaletteKeys `json:"command_palette"` // Keys within command palette
}

// CommandPaletteKeys defines keys for command palette actions.
type CommandPaletteKeys struct {
	ViewChat   string `json:"view_chat"`   // Switch to chat view (default: "1")
	ViewTasks  string `json:"view_tasks"`  // Switch to tasks view (default: "2")
	ViewQueue  string `json:"view_queue"`  // Switch to queue view (default: "3")
	ViewMemory string `json:"view_memory"` // Switch to memory view (default: "4")
	Sidebar    string `json:"sidebar"`     // Toggle sidebar (default: "y")
	Sessions   string `json:"sessions"`    // Open session picker (default: "s")
}

// SessionConfig defines session behavior settings.
type SessionConfig struct {
	AutoResume  bool   `json:"auto_resume"`  // Auto-resume last session on startup (default: true)
	DefaultName string `json:"default_name"` // Default name for new sessions (default: "default")
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Keybindings: KeybindingsConfig{
			CommandMode: "ctrl+x",
			Quit:        "ctrl+c",
			CommandPalette: CommandPaletteKeys{
				ViewChat:   "1",
				ViewTasks:  "2",
				ViewQueue:  "3",
				ViewMemory: "4",
				Sidebar:    "y",
				Sessions:   "s",
			},
		},
		Session: SessionConfig{
			AutoResume:  true,
			DefaultName: "default",
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
