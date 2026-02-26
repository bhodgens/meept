package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// AgentDefinition represents an agent definition from a TOML file.
type AgentDefinition struct {
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Role        string   `toml:"role"` // "dispatcher", "executor", "conversational", "reviewer"
	Description string   `toml:"description"`
	Model       string   `toml:"model"`
	Enabled     bool     `toml:"enabled"`
	CanDelegate bool     `toml:"can_delegate"`

	// Tools and capabilities
	AdditionalTools []string `toml:"additional_tools"`
	Capabilities    []string `toml:"capabilities"`

	// Prompt composition
	PromptComponents []string `toml:"prompt_components"`

	// Constraints
	Constraints AgentConstraintsConfig `toml:"constraints"`
}

// AgentConstraintsConfig holds agent operational constraints.
type AgentConstraintsConfig struct {
	MaxIterations    int `toml:"max_iterations"`
	TimeoutSeconds   int `toml:"timeout_seconds"`
	MaxTokensPerTurn int `toml:"max_tokens_per_turn"`
	MaxMemoryRefs    int `toml:"max_memory_refs"`
}

// ToTimeout converts TimeoutSeconds to time.Duration.
func (c AgentConstraintsConfig) ToTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 5 * time.Minute // Default
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// agentFile represents a TOML file containing agent definitions.
type agentFile struct {
	Agents []AgentDefinition `toml:"agent"`
}

// LoadAgentDefinitions loads all agent definitions from the configured directories.
// Later directories in the list override earlier ones (for the same agent ID).
func LoadAgentDefinitions(configDirs []string) (map[string]*AgentDefinition, error) {
	agents := make(map[string]*AgentDefinition)

	for _, dir := range configDirs {
		dir = expandPath(dir)

		// Check if directory exists
		info, err := os.Stat(dir)
		if os.IsNotExist(err) {
			continue // Skip non-existent directories
		}
		if err != nil {
			return nil, fmt.Errorf("error accessing %s: %w", dir, err)
		}
		if !info.IsDir() {
			continue
		}

		// Load all TOML files in the directory
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("error reading %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".toml") {
				continue
			}

			filePath := filepath.Join(dir, entry.Name())
			fileAgents, err := loadAgentFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("error loading %s: %w", filePath, err)
			}

			// Merge into map (later files override earlier)
			for _, agent := range fileAgents {
				if agent.ID == "" {
					continue // Skip agents without ID
				}
				agentCopy := agent // Create copy to avoid pointer issues
				agents[agent.ID] = &agentCopy
			}
		}
	}

	return agents, nil
}

// loadAgentFile loads agent definitions from a single TOML file.
func loadAgentFile(path string) ([]AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Expand environment variables
	content := expandEnvVars(string(data))

	var file agentFile
	if err := toml.Unmarshal([]byte(content), &file); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return file.Agents, nil
}

// LoadAgentDefinitionsDefault loads agents from default locations.
func LoadAgentDefinitionsDefault(cfg *AgentsConfig) (map[string]*AgentDefinition, error) {
	if cfg == nil {
		cfg = &AgentsConfig{
			ConfigDirs: []string{"~/.meept/agents", "config/agents"},
		}
	}

	return LoadAgentDefinitions(cfg.ConfigDirs)
}

// ValidateAgentDefinition validates an agent definition.
func ValidateAgentDefinition(agent *AgentDefinition) error {
	if agent.ID == "" {
		return fmt.Errorf("agent ID is required")
	}

	if agent.Name == "" {
		return fmt.Errorf("agent name is required for %s", agent.ID)
	}

	validRoles := map[string]bool{
		"dispatcher":     true,
		"executor":       true,
		"conversational": true,
		"reviewer":       true,
	}
	if agent.Role != "" && !validRoles[agent.Role] {
		return fmt.Errorf("invalid role %q for agent %s", agent.Role, agent.ID)
	}

	return nil
}

// FilterEnabledAgents returns only enabled agent definitions.
func FilterEnabledAgents(agents map[string]*AgentDefinition) map[string]*AgentDefinition {
	enabled := make(map[string]*AgentDefinition)
	for id, agent := range agents {
		if agent.Enabled {
			enabled[id] = agent
		}
	}
	return enabled
}

// GetAgentsByRole returns agents with a specific role.
func GetAgentsByRole(agents map[string]*AgentDefinition, role string) []*AgentDefinition {
	var result []*AgentDefinition
	for _, agent := range agents {
		if agent.Role == role {
			result = append(result, agent)
		}
	}
	return result
}

// MergeAgentDefaults applies default values to an agent definition.
func MergeAgentDefaults(agent *AgentDefinition) {
	if agent.Constraints.MaxIterations == 0 {
		agent.Constraints.MaxIterations = 10
	}
	if agent.Constraints.TimeoutSeconds == 0 {
		agent.Constraints.TimeoutSeconds = 300 // 5 minutes
	}
	if agent.Constraints.MaxTokensPerTurn == 0 {
		agent.Constraints.MaxTokensPerTurn = 4096
	}
	if agent.Constraints.MaxMemoryRefs == 0 {
		agent.Constraints.MaxMemoryRefs = 20
	}
}
