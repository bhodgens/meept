// Package agents provides agent definition discovery, parsing, and merging.
//
// Agents can be defined in AGENT.md files with YAML frontmatter, following
// the same pattern as skills. The package supports a 3-tier discovery
// hierarchy where higher-priority tiers shadow lower ones.
package agents

import "slices"

import "time"

import "github.com/caimlas/meept/internal/config"

// Priority levels for agent discovery (lower is higher priority).
const (
	PriorityProject = 0 // .meept/agents/ (project-local)
	PriorityUser    = 1 // ~/.meept/agents/ (user-global)
	PrioritySystem  = 2 // ~/.config/meept/agents/ (system-wide)
	PriorityBundled = 3 // config/agents/ (bundled defaults)
)

// AgentMetadata holds the parsed YAML frontmatter from an AGENT.md file.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentMetadata struct {
	// ID is the unique identifier for this agent (e.g., "coder").
	ID string `yaml:"id"`

	// Name is a human-readable name for the agent.
	Name string `yaml:"name"`

	// Role defines the agent's role: "dispatcher", "executor", "reviewer".
	Role string `yaml:"role"`

	// Model can be an alias name or direct model reference.
	Model string `yaml:"model,omitempty"`

	// AdditionalTools are tools beyond baseline that this agent can use.
	AdditionalTools []string `yaml:"additional_tools,omitempty"`

	// Capabilities are capability tags for model selection.
	Capabilities []string `yaml:"capabilities,omitempty"`

	// AvailableSkills lists skill names this agent can invoke.
	AvailableSkills []string `yaml:"available_skills,omitempty"`

	// SkillTriggers maps keywords to skill names for automatic invocation.
	SkillTriggers map[string]string `yaml:"skill_triggers,omitempty"`

	// MaxIterations is the maximum reasoning cycles.
	MaxIterations int `yaml:"max_iterations,omitempty"`

	// TimeoutSeconds is the maximum duration for a single request.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`

	// MaxTokensPerTurn limits tokens per turn.
	MaxTokensPerTurn int `yaml:"max_tokens_per_turn,omitempty"`

	// MaxConversationTokens is the total token budget per conversation turn.
	MaxConversationTokens int `yaml:"max_conversation_tokens,omitempty"`

	// MaxMemoryRefs limits memory references to inject.
	MaxMemoryRefs int `yaml:"max_memory_refs,omitempty"`

	// Temperature controls LLM randomness (nil = use default).
	Temperature *float64 `yaml:"temperature,omitempty"`

	// TopP controls nucleus sampling (nil = use default).
	TopP *float64 `yaml:"top_p,omitempty"`
}

// AgentDefinition represents a fully parsed agent definition from AGENT.md.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentDefinition struct {
	// Metadata from YAML frontmatter.
	AgentMetadata `yaml:",inline"`

	// Body contains the markdown instructions for the agent.
	Body string `json:"body"`

	// Path is the filesystem path the agent was loaded from.
	Path string `json:"path"`

	// Priority indicates the discovery tier (0=project, 1=user, 2=system, 3=bundled).
	Priority int `json:"priority"`
}

// Timeout returns the timeout as a time.Duration.
func (d *AgentDefinition) Timeout() time.Duration {
	if d.TimeoutSeconds <= 0 {
		return 5 * time.Minute // default
	}
	return time.Duration(d.TimeoutSeconds) * time.Second
}

// HasTool checks if the agent has a specific additional tool.
func (d *AgentDefinition) HasTool(tool string) bool {
	return slices.Contains(d.AdditionalTools, tool)
}

// HasCapability checks if the agent has a specific capability.
func (d *AgentDefinition) HasCapability(capability string) bool {
	return slices.Contains(d.Capabilities, capability)
}

// HasSkill checks if the agent has access to a specific skill.
func (d *AgentDefinition) HasSkill(skill string) bool {
	return slices.Contains(d.AvailableSkills, skill)
}

// GetSkillForTrigger returns the skill name for a trigger keyword.
func (d *AgentDefinition) GetSkillForTrigger(keyword string) string {
	if d.SkillTriggers == nil {
		return ""
	}
	return d.SkillTriggers[keyword]
}

// DefaultMetadata returns sensible default values for agent metadata.
func DefaultMetadata() AgentMetadata {
	return AgentMetadata{
		Role:             config.AgentRoleExecutor,
		MaxIterations:    25,
		TimeoutSeconds:   300,
		MaxTokensPerTurn: 4096,
		MaxMemoryRefs:    20,
	}
}

// DiscoveryTier represents a directory tier for agent discovery.
type DiscoveryTier struct {
	Path     string
	Priority int
}
