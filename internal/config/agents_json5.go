package config

import (
	"os"
	"path/filepath"
)

// AgentDefinitionJSON5 represents an agent in the new JSON5 format.
type AgentDefinitionJSON5 struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Role             string                 `json:"role"`
	Description      string                 `json:"description"`
	Model            string                 `json:"model"`
	Enabled          bool                   `json:"enabled"`
	CanDelegate      bool                   `json:"can_delegate"`
	AdditionalTools  []string               `json:"additional_tools"`
	Capabilities     []string               `json:"capabilities"`
	PromptComponents []string               `json:"prompt_components"`
	Constraints      AgentConstraintsConfig `json:"constraints"`
}

// AgentsFileJSON5 is the root of the agents.json5 file.
type AgentsFileJSON5 struct {
	Agents []AgentDefinitionJSON5 `json:"agents"`
}

// LoadAgentDefinitionsDefaultWithJSON5 tries JSON5 first, then TOML.
func LoadAgentDefinitionsDefaultWithJSON5(cfg *AgentsConfig) (map[string]*AgentDefinition, error) {
	// Try JSON5 format first
	homeDir, _ := os.UserHomeDir()
	json5Path := filepath.Join(homeDir, ".meept", "agents.json5")
	if _, err := os.Stat(json5Path); err == nil {
		return LoadAgentDefinitionsJSON5(json5Path)
	}

	projectJSON5 := "config/agents.json5"
	if _, err := os.Stat(projectJSON5); err == nil {
		return LoadAgentDefinitionsJSON5(projectJSON5)
	}

	// Fall back to TOML directory format
	if cfg == nil {
		cfg = &AgentsConfig{
			ConfigDirs: []string{"~/.meept/agents", "config/agents"},
		}
	}
	return LoadAgentDefinitions(cfg.ConfigDirs)
}
