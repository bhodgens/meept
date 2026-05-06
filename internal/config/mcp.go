package config

import (
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
			// Return empty config if file doesn't exist
			return &MCPServersConfig{
				Servers: []mcp.ServerConfig{},
			}, nil
		}
		return nil, err
	}

	return &cfg, nil
}

// LoadMCPConfigDefault loads MCP config from the default location.
// Prefers JSON5, falls back to legacy JSON path.
func LoadMCPConfigDefault() (*MCPServersConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Return empty config if we can't determine home dir
		return &MCPServersConfig{
			Servers: []mcp.ServerConfig{},
		}, nil
	}

	// Try JSON5 first
	json5Path := filepath.Join(homeDir, ".meept", "mcp_servers.json5")
	if _, err := os.Stat(json5Path); err == nil {
		return LoadMCPConfig(json5Path)
	}

	// Fall back to legacy JSON
	return LoadMCPConfig(filepath.Join(homeDir, ".meept", "mcp_servers.json"))
}
