package config

import (
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
