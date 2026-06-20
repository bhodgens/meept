package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/tools/mcp"
)

// MCPServersConfig represents the mcp_servers.json5 configuration structure.
type MCPServersConfig struct {
	Servers []mcp.ServerConfig `json:"servers"`
}

// LoadMCPConfig loads MCP server configuration from a JSON5 file.
// If the file doesn't exist, returns an empty config (not an error).
func LoadMCPConfig(path string) (*MCPServersConfig, error) {
	path = expandPath(path)

	var cfg MCPServersConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return &MCPServersConfig{Servers: []mcp.ServerConfig{}}, nil
		}
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}
	return &cfg, nil
}

// LoadMCPConfigDefault loads MCP config from the default location (~/.meept/mcp_servers.json5).
func LoadMCPConfigDefault() (*MCPServersConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &MCPServersConfig{Servers: []mcp.ServerConfig{}}, err
	}
	return LoadMCPConfig(filepath.Join(homeDir, ".meept", "mcp_servers.json5"))
}

// SaveMCPConfig writes the MCP server configuration atomically.
// Writes to path+".tmp" then renames into place (POSIX atomic).
// ${VAR} placeholders in env values are preserved as-is.
func SaveMCPConfig(path string, cfg *MCPServersConfig) error {
	path = expandPath(path)
	tmpPath := path + ".tmp"

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	// Restricted perms: file may contain API key placeholders.
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write MCP config temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		// Best-effort cleanup of the temp file on failure; the rename error
		// is the primary failure and should be surfaced, not the cleanup.
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("failed to rename MCP config into place (cleanup also failed: %v): %w", removeErr, err)
		}
		return fmt.Errorf("failed to rename MCP config into place: %w", err)
	}

	return nil
}
