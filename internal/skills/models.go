// Package skills provides skill discovery, parsing, and execution for meept.
//
// Skills are SKILL.md files with YAML frontmatter describing capabilities,
// requirements, and instructions. The package supports a 3-tier discovery
// hierarchy where higher-priority tiers shadow lower ones.
package skills

// Priority levels for skill discovery (lower is higher priority).
const (
	PriorityProject = 0 // .meept/skills/ (project-local)
	PriorityUser    = 1 // ~/.meept/skills/ (user-global)
	PrioritySystem  = 2 // ~/.config/meept/skills/ (system-wide)
)

// Skill represents a parsed skill definition from a SKILL.md file.
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

	// Priority indicates the discovery tier (0=project, 1=user, 2=system).
	Priority int `json:"priority"`

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
}

// HasCapability checks if the skill requires a specific capability.
func (s *Skill) HasCapability(cap string) bool {
	for _, c := range s.Requires {
		if c == cap {
			return true
		}
	}
	return false
}

// HasTag checks if the skill has a specific tag.
func (s *Skill) HasTag(tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
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

// SkillMetadata holds the parsed YAML frontmatter from a SKILL.md file.
type SkillMetadata struct {
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description"`
	Requires      []string `yaml:"requires"`
	Tags          []string `yaml:"tags"`
	Examples      []string `yaml:"examples"`
	AllowedTools  []string `yaml:"allowed-tools"`
	RiskLevel     string   `yaml:"risk-level"`
	MaxIterations int      `yaml:"max-iterations"`
	Temperature   *float64 `yaml:"temperature"`
	MaxTokens     *int     `yaml:"max-tokens"`
}

// DefaultMetadata returns a SkillMetadata with sensible defaults.
func DefaultMetadata() SkillMetadata {
	return SkillMetadata{
		RiskLevel:     "medium",
		MaxIterations: 10,
	}
}

// SkillExecutionResult holds the result of executing a skill.
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
}
