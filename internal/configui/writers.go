package configui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/config"
)

// ConfigFilePath resolves a config file path relative to ~/.meept/.
// It is a package-level variable so tests can override it to use temp dirs.
var ConfigFilePath = configFilePath

func configFilePath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return name
	}
	return filepath.Join(home, ".meept", name)
}

// WriteConfigFile atomically writes a JSON5 config file.
// It marshals v to indented JSON, writes to a temp file, then renames.
// Permissions are set to 0600 for security.
func WriteConfigFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	// Write to temp file then rename for atomicity
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // cleanup on failure
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}

	return nil
}

// LoadMainConfig loads the main meept.json5 config.
func LoadMainConfig() (*config.Config, error) {
	return config.LoadDefault()
}

// SaveMainConfig saves the full main config to meept.json5.
func SaveMainConfig(cfg *config.Config) error {
	path := ConfigFilePath("meept.json5")
	return WriteConfigFile(path, cfg)
}
