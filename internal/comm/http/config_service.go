// Package http provides the HTTP REST API server for the Meept menubar application.
package http

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// ConfigService handles configuration file operations.
type ConfigService struct {
	meeptDir string
}

// NewConfigService creates a new ConfigService.
func NewConfigService() (*ConfigService, error) {
	// Find the meept directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Try user.Current as fallback
		if u, err := user.Current(); err == nil {
			homeDir = u.HomeDir
		} else {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	meeptDir := filepath.Join(homeDir, ".meept")

	// Ensure directory exists
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(meeptDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create meept directory: %w", err)
	}

	return &ConfigService{
		meeptDir: meeptDir,
	}, nil
}

// getClientConfigPath returns the path to client.json5.
func (s *ConfigService) getClientConfigPath() string {
	return filepath.Join(s.meeptDir, "client.json5")
}

// getModelsConfigPath returns the path to models.json5.
func (s *ConfigService) getModelsConfigPath() string {
	return filepath.Join(s.meeptDir, "models.json5")
}

// getAgentsDir returns the path to the agents directory.
func (s *ConfigService) getAgentsDir() string {
	return filepath.Join(s.meeptDir, "agents")
}

// getMenubarConfigPath returns the path to menubar.json5.
func (s *ConfigService) getMenubarConfigPath() string {
	return filepath.Join(s.meeptDir, "menubar.json5")
}

// LoadMenubarConfig loads the menubar.json5 content.
func (s *ConfigService) LoadMenubarConfig() (string, error) {
	path := s.getMenubarConfigPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read menubar config: %w", err)
	}
	return string(data), nil
}

// SaveMenubarConfig saves the menubar.json5 content.
func (s *ConfigService) SaveMenubarConfig(content string) error {
	path := s.getMenubarConfigPath()
	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write menubar config: %w", err)
	}
	return nil
}

// LoadClientConfig loads the client.json5 content.
func (s *ConfigService) LoadClientConfig() (string, error) {
	path := s.getClientConfigPath()

	// If client.json5 doesn't exist, create a default one
	if _, err := os.Stat(path); os.IsNotExist(err) {
		defaultContent := `{
  // Meept Client Configuration
  // This file configures the CLI and menubar app behavior

  "theme": "system",
  "language": "en",
  "notifications": {
    "enabled": true,
    "sound": true
  },
  "menubar": {
    "show_status": true,
    "refresh_interval": 5
  }
}`
		//nolint:gosec // user config directory/file permissions
		if err := os.WriteFile(path, []byte(defaultContent), 0o600); err != nil {
			return "", fmt.Errorf("failed to create default client config: %w", err)
		}
		return defaultContent, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read client config: %w", err)
	}

	return string(data), nil
}

// SaveClientConfig saves the client.json5 content.
func (s *ConfigService) SaveClientConfig(content string) error {
	path := s.getClientConfigPath()

	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write client config: %w", err)
	}

	return nil
}

// LoadModelsConfig loads the models.json5 content.
func (s *ConfigService) LoadModelsConfig() (string, error) {
	path := s.getModelsConfigPath()

	// If models.json5 doesn't exist, try to load from config/ directory
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Try project-local first
		if _, err := os.Stat("config/models.json5"); err == nil {
			data, err := os.ReadFile("config/models.json5")
			if err == nil {
				return string(data), nil
			}
		}

		// Create a default one
		defaultContent := `{
  // Meept Models Configuration
  // Define LLM providers and models with their capabilities

  "model": "anthropic/claude-sonnet-4-5",
  "small_model": "anthropic/claude-haiku-4-5",
  "disabled_providers": [],
  "default_timeout": 30,

  "providers": {
    "anthropic": {
      "api": "anthropic",
      "options": {
        "baseURL": "https://api.anthropic.com",
        "apiKey": "${ANTHROPIC_API_KEY}",
        "timeout": 60
      },
      "models": {
        "claude-sonnet-4-5": {
          "name": "Claude Sonnet 4.5",
          "capabilities": ["reasoning", "code", "tool_use"],
          "input_cost": 3.0,
          "output_cost": 15.0,
          "context_limit": 200000,
          "max_output": 64000,
          "temperature": 0.7
        },
        "claude-haiku-4-5": {
          "name": "Claude Haiku 4.5",
          "capabilities": ["reasoning", "fast"],
          "input_cost": 0.8,
          "output_cost": 4.0,
          "context_limit": 200000,
          "max_output": 64000,
          "temperature": 0.7
        }
      }
    }
  }
}`
		//nolint:gosec // user config directory/file permissions
		if err := os.WriteFile(path, []byte(defaultContent), 0o600); err != nil {
			return "", fmt.Errorf("failed to create default models config: %w", err)
		}
		return defaultContent, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read models config: %w", err)
	}

	return string(data), nil
}

// SaveModelsConfig saves the models.json5 content.
func (s *ConfigService) SaveModelsConfig(content string) error {
	path := s.getModelsConfigPath()

	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write models config: %w", err)
	}

	return nil
}

// ListAgents lists all configured agents.
func (s *ConfigService) ListAgents() ([]AgentInfo, error) {
	agentsDir := s.getAgentsDir()

	// Check if agents directory exists
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		// Try to use config/agents from project
		if _, err := os.Stat("config/agents"); err == nil {
			agentsDir = "config/agents"
		} else {
			return []AgentInfo{}, nil
		}
	}

	var agents []AgentInfo

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		agentPath := filepath.Join(agentsDir, entry.Name(), "AGENT.md")
		if _, err := os.Stat(agentPath); err == nil {
			// Read the frontmatter to get agent info
			data, err := os.ReadFile(agentPath)
			if err != nil {
				continue
			}

			name := entry.Name()
			description := ""
			enabled := true

			// Simple frontmatter parsing
			content := string(data)
			if strings.HasPrefix(content, "---") {
				parts := strings.SplitN(content, "---", 3)
				if len(parts) >= 2 {
					frontmatter := parts[1]
					lines := strings.SplitSeq(frontmatter, "\n")
					for line := range lines {
						if after, ok := strings.CutPrefix(line, "name:"); ok {
							name = strings.TrimSpace(after)
						}
						if after, ok := strings.CutPrefix(line, "description:"); ok {
							description = strings.TrimSpace(after)
						}
					}
				}
			}

			agents = append(agents, AgentInfo{
				ID:          entry.Name(),
				Name:        name,
				Description: description,
				Enabled:     enabled,
			})
		}
	}

	return agents, nil
}

// GetAgent gets a specific agent's configuration.
func (s *ConfigService) GetAgent(id string) (*Agent, error) {
	agentsDir := s.getAgentsDir()

	// Check if agents directory exists
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		if _, err := os.Stat("config/agents"); err == nil {
			agentsDir = "config/agents"
		} else {
			return nil, fmt.Errorf("agents directory not found")
		}
	}

	agentPath := filepath.Join(agentsDir, id, "AGENT.md")
	//nolint:gosec // path validated by config directory check
	data, err := os.ReadFile(agentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent file: %w", err)
	}

	content := string(data)
	agent := &Agent{
		ID:      id,
		Prompt:  content,
		Enabled: true,
	}

	// Parse frontmatter
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			frontmatter := parts[1]
			agent.Prompt = strings.TrimSpace(parts[2])

			// Simple YAML-like parsing for frontmatter
			lines := strings.Split(frontmatter, "\n")
			agent.Frontmatter = make(map[string]any)
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if idx := strings.Index(line, ":"); idx > 0 {
					key := strings.TrimSpace(line[:idx])
					value := strings.TrimSpace(line[idx+1:])
					// Remove quotes if present
					value = strings.Trim(value, "\"'")
					agent.Frontmatter[key] = value

					// Extract common fields
					switch key {
					case "name":
						agent.Name = value
					case "description":
						agent.Description = value
					}
				}
			}
		}
	}

	return agent, nil
}

// SaveAgent saves an agent's configuration.
func (s *ConfigService) SaveAgent(id string, agent *Agent) error {
	agentsDir := s.getAgentsDir()

	// Create agent directory
	agentDir := filepath.Join(agentsDir, id)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	// Build AGENT.md content
	var content strings.Builder
	content.WriteString("---\n")
	fmt.Fprintf(&content, "name: %s\n", agent.Name)
	fmt.Fprintf(&content, "description: %s\n", agent.Description)

	// Add other frontmatter fields
	for key, value := range agent.Frontmatter {
		if key != "name" && key != "description" {
			fmt.Fprintf(&content, "%s: %v\n", key, value)
		}
	}

	content.WriteString("---\n\n")
	content.WriteString(agent.Prompt)

	agentPath := filepath.Join(agentDir, "AGENT.md")
	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(agentPath, []byte(content.String()), 0o600); err != nil {
		return fmt.Errorf("failed to write agent file: %w", err)
	}

	return nil
}

// DeleteAgent deletes an agent.
func (s *ConfigService) DeleteAgent(id string) error {
	agentsDir := s.getAgentsDir()
	agentDir := filepath.Join(agentsDir, id)

	// Check if it's a project agent
	//nolint:gosec // path validated by config directory check
	if _, err := os.Stat("config/agents/" + id); err == nil {
		return fmt.Errorf("cannot delete built-in agent %s", id)
	}

	//nolint:gosec // path validated by config directory check
	if err := os.RemoveAll(agentDir); err != nil {
		return fmt.Errorf("failed to delete agent directory: %w", err)
	}

	return nil
}
