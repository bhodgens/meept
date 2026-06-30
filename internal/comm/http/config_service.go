// Package http provides the HTTP REST API server for the Meept menubar application.
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	configCli "github.com/caimlas/meept/internal/config"
	"github.com/tailscale/hujson"
)

// defaultClientConfigJSON5 is the default content used to seed client.json5
// when the file does not yet exist. Both LoadClientConfig and PatchClientConfig
// reference this constant so they cannot drift apart.
const defaultClientConfigJSON5 = `{
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

// validAgentID matches safe agent identifiers used as directory names.
// Disallows path separators, dots, and other shell/path metacharacters
// to prevent path traversal in GetAgent/SaveAgent/DeleteAgent.
var validAgentID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validateAgentID returns an error if id is not a safe agent identifier
// (rejects "..", "/", "\", and any non-[A-Za-z0-9_-] character).
func validateAgentID(id string) error {
	if id == "" || !validAgentID.MatchString(id) {
		return errors.New("invalid agent id: must be non-empty and match [A-Za-z0-9_-]+")
	}
	return nil
}

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

// getMeeptConfigPath returns the path to meept.json5 (the main config).
func (s *ConfigService) getMeeptConfigPath() string {
	return filepath.Join(s.meeptDir, "meept.json5")
}

// LoadMeeptConfig loads the meept.json5 content (raw JSON5 text).
// Returns ("", nil) when the file does not exist, mirroring LoadMenubarConfig.
func (s *ConfigService) LoadMeeptConfig() (string, error) {
	path := s.getMeeptConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read meept config: %w", err)
	}
	return string(data), nil
}

// NormalizeJSON5 converts JSON5 text (with comments, trailing commas, unquoted keys)
// to strict JSON using hujson.Standardize.
func (s *ConfigService) NormalizeJSON5(content string) (string, error) {
	standardized, err := hujson.Standardize([]byte(content))
	if err != nil {
		return "", fmt.Errorf("failed to standardize JSON5: %w", err)
	}
	return string(standardized), nil
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
		//nolint:gosec // user config directory/file permissions
		if err := os.WriteFile(path, []byte(defaultClientConfigJSON5), 0o600); err != nil {
			return "", fmt.Errorf("failed to create default client config: %w", err)
		}
		return defaultClientConfigJSON5, nil
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

			info := AgentInfo{
				ID:      entry.Name(),
				Name:    entry.Name(),
				Enabled: true, // absent/nil in frontmatter means true
			}

			// Simple frontmatter parsing
			content := string(data)
			if strings.HasPrefix(content, "---") {
				parts := strings.SplitN(content, "---", 3)
				if len(parts) >= 2 {
					frontmatter := parts[1]
					lines := strings.SplitSeq(frontmatter, "\n")
					for line := range lines {
						if after, ok := strings.CutPrefix(line, "name:"); ok {
							info.Name = strings.TrimSpace(after)
						}
						if after, ok := strings.CutPrefix(line, "description:"); ok {
							info.Description = strings.TrimSpace(after)
						}
						if after, ok := strings.CutPrefix(line, "role:"); ok {
							info.Role = strings.TrimSpace(after)
						}
						if after, ok := strings.CutPrefix(line, "can_delegate:"); ok {
							val := strings.TrimSpace(after)
							info.CanDelegate = val == "true"
						}
						if after, ok := strings.CutPrefix(line, "reviews_domain:"); ok {
							info.ReviewsDomain = strings.TrimSpace(after)
						}
						if after, ok := strings.CutPrefix(line, "enabled:"); ok {
							val := strings.TrimSpace(after)
							// only "false" disables; nil/absent already defaults to true
							info.Enabled = val != "false"
						}
					}
				}
			}

			agents = append(agents, info)
		}
	}

	return agents, nil
}

// GetAgent gets a specific agent's configuration.
func (s *ConfigService) GetAgent(id string) (*Agent, error) {
	if err := validateAgentID(id); err != nil {
		return nil, err
	}
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
	if err := validateAgentID(id); err != nil {
		return err
	}
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
	if err := validateAgentID(id); err != nil {
		return err
	}
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

// LoadOrchestratorConfig reads the orchestrator block from meept.json5.
// Returns the zero-value OrchestratorConfig when the file or block is absent,
// mirroring the backward-compat behavior of config.LoadJSON5Config.
func (s *ConfigService) LoadOrchestratorConfig() (configCli.OrchestratorConfig, error) {
	path := s.getMeeptConfigPath()
	var zero configCli.OrchestratorConfig

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, nil
		}
		return zero, fmt.Errorf("failed to read meept config: %w", err)
	}

	stdJSON, err := hujson.Standardize(raw)
	if err != nil {
		return zero, fmt.Errorf("failed to parse meept.json5: %w", err)
	}

	// Parse into a partial struct so unknown / remaining keys are tolerated.
	var partial struct {
		Orchestrator configCli.OrchestratorConfig `json:"orchestrator"`
	}
	if err := json.Unmarshal(stdJSON, &partial); err != nil {
		return zero, fmt.Errorf("failed to unmarshal orchestrator config: %w", err)
	}
	return partial.Orchestrator, nil
}

// SaveOrchestratorConfig writes the orchestrator block back to meept.json5,
// preserving all other top-level keys. The file is written atomically via a
// tmp-then-rename to avoid torn writes. If meept.json5 does not exist, it is
// created with just the orchestrator block.
func (s *ConfigService) SaveOrchestratorConfig(oc configCli.OrchestratorConfig) error {
	path := s.getMeeptConfigPath()

	// Read existing content if present, otherwise start from an empty object.
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read meept config: %w", err)
	}
	if os.IsNotExist(err) {
		existing = []byte("{}")
	}

	stdJSON, err := hujson.Standardize(existing)
	if err != nil {
		return fmt.Errorf("failed to parse existing meept.json5: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(stdJSON, &root); err != nil {
		return fmt.Errorf("failed to unmarshal existing meept.json5: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}

	ocBytes, err := json.Marshal(oc)
	if err != nil {
		return fmt.Errorf("failed to marshal orchestrator config: %w", err)
	}
	var ocMap map[string]any
	if err := json.Unmarshal(ocBytes, &ocMap); err != nil {
		return fmt.Errorf("failed to re-marshal orchestrator config: %w", err)
	}
	root["orchestrator"] = ocMap

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated meept config: %w", err)
	}

	tmpPath := path + ".tmp"
	//nolint:gosec // user config file; restrictive perms intended
	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return fmt.Errorf("failed to write meept config temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("failed to rename meept config into place (cleanup also failed: %v): %w", removeErr, err)
		}
		return fmt.Errorf("failed to rename meept config into place: %w", err)
	}
	return nil
}

// PatchClientConfig applies an RFC 7396 JSON merge-patch to the on-disk
// client.json5. It reads the file fresh, standardizes JSON5 → JSON via
// hujson, unmarshals into map[string]any, deep-merges the patch, and
// writes back atomically (temp file + rename). The merged map is
// returned (as plain JSON data — comments from the source file are
// stripped by Standardize). If client.json5 does not exist, it is seeded
// from the same default content block used by LoadClientConfig before
// the patch is applied.
//
// Merge semantics (RFC 7396):
//   - A null value in patch deletes the corresponding key in the target.
//   - Object values are merged recursively.
//   - Arrays and scalars from patch replace the target value.
func (s *ConfigService) PatchClientConfig(patch map[string]any) (map[string]any, error) {
	path := s.getClientConfigPath()

	// Read existing content if present; otherwise seed from the same default
	// block that LoadClientConfig uses (keeps the two paths byte-identical).
	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read client config: %w", err)
		}
		existing = []byte(defaultClientConfigJSON5)
	}

	// Standardize JSON5 (strip comments, trailing commas, quote keys).
	stdJSON, err := hujson.Standardize(existing)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client.json5: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(stdJSON, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client.json5: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}

	merged := configCli.DeepMerge(root, patch)

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged client config: %w", err)
	}
	out = append(out, '\n')

	// Atomic write: temp file + rename. Mirrors SaveOrchestratorConfig.
	tmpPath := path + ".tmp"
	//nolint:gosec // user config file; restrictive perms intended
	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write client config temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("failed to rename client config into place (cleanup also failed: %v): %w", removeErr, err)
		}
		return nil, fmt.Errorf("failed to rename client config into place: %w", err)
	}

	return merged, nil
}
