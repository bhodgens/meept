package config

import (
	"encoding/json"
	"os"

	"github.com/caimlas/meept/internal/tools/mcp"
)

// MCPServersConfig represents the mcp_servers.json configuration structure.
type MCPServersConfig struct {
	Servers []mcp.ServerConfig `json:"servers"`
}

// LoadMCPConfig loads MCP server configuration from a JSON file.
// If the file doesn't exist, returns an empty config (not an error).
func LoadMCPConfig(path string) (*MCPServersConfig, error) {
	path = expandPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config if file doesn't exist
			return &MCPServersConfig{
				Servers: []mcp.ServerConfig{},
			}, nil
		}
		return nil, err
	}

	// Expand environment variables
	content := expandEnvVars(string(data))

	var cfg MCPServersConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadMCPConfigDefault loads MCP config from the default location (~/.meept/mcp_servers.json).
func LoadMCPConfigDefault() (*MCPServersConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Return empty config if we can't determine home dir
		return &MCPServersConfig{
			Servers: []mcp.ServerConfig{},
		}, nil
	}

	return LoadMCPConfig(homeDir + "/.meept/mcp_servers.json")
}
