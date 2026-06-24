// Package skills provides skill discovery, parsing, and execution for meept.
//
// Skills are SKILL.md files with YAML frontmatter describing capabilities,
// requirements, and instructions. The package supports a 3-tier discovery
// hierarchy where higher-priority tiers shadow lower ones.
package skills

import (
	"slices"

	"github.com/caimlas/meept/internal/security/taint"
)

// Priority levels for skill discovery (lower is higher priority).
const (
	PriorityProject = 0 // .meept/skills/ (project-local)
	PriorityUser    = 1 // ~/.meept/skills/ (user-global)
	PriorityClaude  = 2 // ~/.claude/skills/ (Claude Code skills)
	PriorityHermes  = 3 // ~/.hermes/skills/ (Hermes-Agent skills)
	PrioritySystem  = 4 // ~/.config/meept/skills/ (system-wide)
)

// Skill represents a parsed skill definition from a SKILL.md file.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Skill struct {
	// Name is the unique identifier for the skill (e.g., "code-review").
	Name string `json:"name"`

	// Description is a human-readable description of what the skill does.
	Description string `json:"description"`

	// Requires lists capability tags that a model must satisfy to run this skill.
	// Examples: ["code", "reasoning"], ["tool_use"], etc.
	Requires []string `json:"requires,omitempty"`

	// Tags are categorization labels for the skill.
	Tags []string `json:"tags,omitempty"`

	// Examples are sample prompts or use cases for the skill.
	Examples []string `json:"examples,omitempty"`

	// Body contains the instruction markdown from the SKILL.md file.
	Body string `json:"body"`

	// Path is the filesystem path the skill was loaded from.
	Path string `json:"path"`

	// Priority indicates the discovery tier (0=project, 1=user, 2=claude, 3=hermes, 4=system).
	Priority int `json:"priority"`

	// Source identifies where the skill was discovered from.
	// Values: "meept" (default), "claude" (from Claude tier).
	Source string `json:"source,omitempty"`

	// AllowedTools is a subset of tool names this skill may use. Empty means all.
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// RiskLevel is the risk classification: "low", "medium", "high".
	RiskLevel string `json:"risk_level"`

	// MaxIterations is the maximum agent-loop iterations for this skill.
	MaxIterations int `json:"max_iterations"`

	// Temperature is an optional LLM temperature override for this skill.
	Temperature *float64 `json:"temperature,omitempty"`

	// MaxTokens is an optional LLM max_tokens override for this skill.
	MaxTokens *int `json:"max_tokens,omitempty"`

	// MCPServers lists MCP servers that should be started when this skill is activated.
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`

	// UIType is an optional UI descriptor for skill rendering.
	// Values: "panel" (renders as a panel), "dialog" (opens a dialog), "external" (opens URL).
	// Empty means default behavior (description + execute button).
	UIType string `json:"ui_type,omitempty"`

	// Prerequisites holds Hermes-Agent runtime requirements (env vars, commands, packages).
	// Nil for Meept-native skills.
	Prerequisites *HermesPrerequisites `json:"prerequisites,omitempty" yaml:"prerequisites,omitempty"`

	// SourceOrigin tracks which skill system the skill originated from.
	// Values: "meept" (default), "claude", "hermes".
	SourceOrigin string `json:"source_origin,omitempty"`
}

// HasCapability checks if the skill requires a specific capability.
func (s *Skill) HasCapability(capability string) bool {
	return slices.Contains(s.Requires, capability)
}

// HasTag checks if the skill has a specific tag.
func (s *Skill) HasTag(tag string) bool {
	return slices.Contains(s.Tags, tag)
}

// MatchesTags returns true if the skill has all specified tags.
func (s *Skill) MatchesTags(tags []string) bool {
	for _, tag := range tags {
		if !s.HasTag(tag) {
			return false
		}
	}
	return true
}

// UsesExternalLLM returns true if the skill uses an external LLM for inference.
// Skills always use LLMs for inference, so this returns true for all skills.
func (s *Skill) UsesExternalLLM() bool {
	return true // All skills invoke LLMs for inference
}

// UsesMCP returns true if the skill is configured to use MCP servers.
func (s *Skill) UsesMCP() bool {
	return len(s.MCPServers) > 0
}

// MCPServerConfig describes an MCP server embedded in a skill.
type MCPServerConfig struct {
	// Name is a unique identifier for this MCP server within the skill.
	Name string `yaml:"name"`
	// Command is the executable to launch for the MCP server.
	Command string `yaml:"command"`
	// Args are arguments passed to the command.
	Args []string `yaml:"args"`
	// Env are optional environment variables set for the server process.
	Env map[string]string `yaml:"env,omitempty"`
}

// SkillMetadata holds the parsed YAML frontmatter from a SKILL.md file.
//
//nolint:revive // stutter with package name is intentional for API clarity
type SkillMetadata struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	Requires      []string          `yaml:"requires"`
	Tags          []string          `yaml:"tags"`
	Examples      []string          `yaml:"examples"`
	AllowedTools  []string          `yaml:"allowed-tools"`
	RiskLevel     string            `yaml:"risk-level"`
	MaxIterations int               `yaml:"max-iterations"`
	Temperature   *float64          `yaml:"temperature"`
	MaxTokens     *int              `yaml:"max-tokens"`
	MCPServers    []MCPServerConfig `yaml:"mcp-servers"`
	UIType        string            `yaml:"ui-type"`

	// Claude-specific fields (parsed separately, merged into Tags).
	Trigger string `yaml:"trigger"`

	// Hermes-specific fields (populated during 4th parse pass).
	HermesPrereqs *HermesPrerequisites `yaml:"-"`
	SourceOrigin  string               `yaml:"-"`
}

// DefaultMetadata returns a SkillMetadata with sensible defaults.
func DefaultMetadata() SkillMetadata {
	return SkillMetadata{
		RiskLevel:     "medium",
		MaxIterations: 10,
	}
}

// SkillExecutionResult holds the result of executing a skill.
//
//nolint:revive // stutter with package name is intentional for API clarity
type SkillExecutionResult struct {
	// Content is the LLM response content.
	Content string `json:"content"`

	// Model is the model ID that was used.
	Model string `json:"model"`

	// PromptTokens is the number of prompt tokens used.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the number of completion tokens used.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the total tokens used.
	TotalTokens int `json:"total_tokens"`

	// MCPTools lists the tools discovered from MCP servers that were
	// running during this execution. Empty if no MCP servers were started.
	MCPTools []ToolDef `json:"mcp_tools,omitempty"`

	// MCPServersStarted is true when at least one MCP server was
	// successfully started for this execution.
	MCPServersStarted bool `json:"mcp_servers_started"`

	// TaintLabel indicates the trust level of the skill output.
	// Values: "none" (clean), "untrusted" (external LLM/MCP), "external" (web fetch), etc.
	TaintLabel taint.TaintLabel `json:"taint_label,omitempty"`

	// WasSanitized is true when the skill output was modified by security sanitization.
	WasSanitized bool `json:"was_sanitized"`
}
