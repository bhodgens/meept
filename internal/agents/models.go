// Package agents provides agent definition discovery, parsing, and merging.
//
// Agents can be defined in AGENT.md files with YAML frontmatter, following
// the same pattern as skills. The package supports a 3-tier discovery
// hierarchy where higher-priority tiers shadow lower ones.
package agents

import "slices"

import "time"

import (
	"gopkg.in/yaml.v3"

	"github.com/caimlas/meept/internal/config"
)

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

	// Description is a one-liner for UI/API display.
	Description string `yaml:"description,omitempty"`

	// Enabled controls whether the agent is loaded. Nil/absent = true (default on).
	// Set explicitly to false in AGENT.md to disable an agent.
	Enabled *bool `yaml:"enabled,omitempty"`

	// CanDelegate controls whether delegate_task is added to the agent's tool set.
	CanDelegate bool `yaml:"can_delegate"`

	// Model can be an alias name or direct model reference.
	Model string `yaml:"model,omitempty"`

	// EnhancerModel is the small/cheap model used to expand a brief into
	// generator-ready prose before an image or video backend is called.
	// Empty = alias "small" (resolves to small_model).
	EnhancerModel string `yaml:"enhancer_model,omitempty"`

	// EscalationModel is the model (alias name or "provider/model" ref)
	// used for the next fix attempt when verification exhausts
	// max_fix_loops. Mirrors the AgentSpec pipeline field (D1, D14);
	// empty = escalation disabled (escalate-to-user fallback).
	EscalationModel string `yaml:"escalation_model,omitempty"`

	// PromptComponents lists component IDs (e.g., "base.constitution") that wrap
	// the AGENT.md body when assembling the system prompt.
	PromptComponents []string `yaml:"prompt_components,omitempty"`

	// ReviewsDomain declares the review domain (code|debug|plan|analysis|test)
	// for reviewer agents. Used by ReviewPolicy.SelectReviewer for dynamic
	// reviewer routing. Empty for non-reviewer agents.
	ReviewsDomain string `yaml:"reviews_domain,omitempty"`

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

	// Reasoning holds per-agent reasoning/thinking settings (effort tier,
	// self-modulation bounds, budget override). Nil = defer to model default.
	Reasoning *AgentReasoningMetadata `yaml:"reasoning,omitempty"`

	// Verification configures post-completion verification for this agent.
	// Nil = use DefaultVerificationConfig() at spec conversion time.
	Verification *VerificationMetadata `yaml:"verification,omitempty" json:"verification,omitempty"`

	// Gate configures the roster quality gate (e.g. "go test ./...") that
	// runs after turns which mutated the workspace. Nil = no gate. The
	// registry converts it to a RosterGateConfig; see
	// internal/agent/roster_gate.go. This is the per-agent AGENT.md path and
	// is orthogonal to employees.defaults.gate.enabled (the employee kill
	// switch).
	Gate *GateMetadata `yaml:"gate,omitempty" json:"gate,omitempty"`
}

// GateMetadata is the AGENT.md frontmatter form of the roster quality gate.
// The registry converts it to a per-agent gate. Contract C2 (frozen).
type GateMetadata struct {
	// Command is the shell command run in the session workdir after a
	// mutating turn. Empty means no gate.
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
	// TimeoutSeconds kills the gate command after this many seconds.
	// Zero/unset = DefaultGateTimeoutSeconds (300) at conversion time.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	// SkipWhenUnchanged skips re-running a previously FAILED gate when the
	// workspace is unchanged since the failure. Absent YAML key parses as
	// true (see UnmarshalYAML).
	SkipWhenUnchanged bool `yaml:"skip_when_unchanged" json:"skip_when_unchanged"`

	// skipExplicit records that skip_when_unchanged was explicitly present
	// in the parsed frontmatter. It exists only so NormalizeGateDefaults can
	// distinguish "unset" from "false" for a plain-bool field; programmatic
	// construction without parsing leaves it false, which normalizes to the
	// true default (same documented caveat as employee.GateConfig).
	skipExplicit bool
}

// NormalizeGateDefaults applies roster-gate defaults at conversion time:
// TimeoutSeconds unset/<=0 becomes DefaultGateTimeoutSeconds (300) and
// SkipWhenUnchanged defaults to true unless explicitly configured false in
// parsed frontmatter. Callers converting GateMetadata to a runtime gate
// MUST call this first.
func (m *GateMetadata) NormalizeGateDefaults() {
	if m == nil {
		return
	}
	if m.TimeoutSeconds <= 0 {
		m.TimeoutSeconds = DefaultGateTimeoutSeconds
	}
	if !m.skipExplicit {
		m.SkipWhenUnchanged = true
	}
}

// UnmarshalYAML implements yaml.Unmarshaler. It decodes the C2 field set and
// records whether skip_when_unchanged was explicitly present so
// NormalizeGateDefaults can distinguish unset (default true) from an
// explicit false.
func (m *GateMetadata) UnmarshalYAML(value *yaml.Node) error {
	var aux struct {
		Command           string `yaml:"command"`
		TimeoutSeconds    int    `yaml:"timeout_seconds"`
		SkipWhenUnchanged *bool  `yaml:"skip_when_unchanged"`
	}
	if err := value.Decode(&aux); err != nil {
		return err
	}
	m.Command = aux.Command
	m.TimeoutSeconds = aux.TimeoutSeconds
	m.SkipWhenUnchanged = aux.SkipWhenUnchanged == nil || *aux.SkipWhenUnchanged
	m.skipExplicit = aux.SkipWhenUnchanged != nil
	return nil
}

// DefaultGateTimeoutSeconds is the roster gate timeout applied when
// GateMetadata.TimeoutSeconds is unset.
const DefaultGateTimeoutSeconds = 300

// VerificationMetadata is the AGENT.md frontmatter form of per-agent
// verification config. The registry converts it to agent.VerificationConfig.
type VerificationMetadata struct {
	// Enabled controls whether verification runs after this agent completes.
	// Nil = use default (true).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Model overrides the model used for verification.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// AutoTrigger controls whether verification triggers automatically.
	// Nil = use default (true).
	AutoTrigger *bool `yaml:"auto_trigger,omitempty" json:"auto_trigger,omitempty"`
	// MaxFixLoops is the maximum number of fix-reverify cycles.
	// 0 = use default (3).
	MaxFixLoops int `yaml:"max_fix_loops,omitempty" json:"max_fix_loops,omitempty"`
}

// AgentReasoningMetadata is the AGENT.md frontmatter form of the per-agent
// reasoning config. The registry converts it to llm.AgentReasoningConfig at
// load time.
type AgentReasoningMetadata struct {
	// Effort is the initial reasoning tier (e.g. "high", "xhigh").
	Effort string `yaml:"effort,omitempty"`

	// AllowSelfModulation permits the agent loop to change effort between
	// turns. Default false.
	AllowSelfModulation bool `yaml:"allow_self_modulation"`

	// MinEffort / MaxEffort bound self-modulation. Empty = no bound on
	// that side.
	MinEffort string `yaml:"min_effort,omitempty"`
	MaxEffort string `yaml:"max_effort,omitempty"`

	// BudgetTokens overrides the tier→budget mapping for this agent.
	BudgetTokens *int `yaml:"budget_tokens,omitempty"`

	// Force bypasses capability gating.
	Force bool `yaml:"force"`
}

// IsEnabled reports whether the agent should be loaded. Nil (absent in
// frontmatter) is treated as true per the AGENT.md schema contract.
func (m *AgentMetadata) IsEnabled() bool {
	if m == nil || m.Enabled == nil {
		return true
	}
	return *m.Enabled
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
		Description:      "",  // empty default; AGENT.md should provide one
		Enabled:          nil, // nil means true per IsEnabled() contract; explicit for clarity
		CanDelegate:      false,
		PromptComponents: nil, // no components → body-only system prompt
		ReviewsDomain:    "",  // empty for non-reviewer agents
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
