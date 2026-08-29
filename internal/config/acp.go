package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ACPAgentEntry is one agent in the ACP catalog.
type ACPAgentEntry struct {
	ID          string            `json:"id"`
	Description string            `json:"description,omitempty"`
	Command     []string          `json:"command"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	DefaultMode string            `json:"default_mode,omitempty"`
	Enabled     bool              `json:"enabled"`
}

// ACPAgentsConfig is the acp_agents.json5 catalog structure.
type ACPAgentsConfig struct {
	Agents []ACPAgentEntry `json:"agents"`
}

// LoadACPAgents loads ACP agent catalog configuration from a JSON5 file.
// If the file doesn't exist, returns an empty config (not an error).
func LoadACPAgents(path string) (*ACPAgentsConfig, error) {
	path = expandPath(path)

	var cfg ACPAgentsConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ACPAgentsConfig{Agents: []ACPAgentEntry{}}, nil
		}
		return nil, fmt.Errorf("failed to load ACP agents: %w", err)
	}
	return &cfg, nil
}

// SaveACPAgents writes the ACP agent catalog atomically.
// Writes to path+".tmp" then renames into place (POSIX atomic).
func SaveACPAgents(path string, cfg *ACPAgentsConfig) error {
	path = expandPath(path)
	tmpPath := path + ".tmp"

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal ACP agents: %w", err)
	}

	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write ACP agents temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("failed to rename ACP agents into place (cleanup also failed: %v): %w", removeErr, err)
		}
		return fmt.Errorf("failed to rename ACP agents into place: %w", err)
	}

	return nil
}
