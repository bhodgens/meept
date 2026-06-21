package agent

import (
	"slices"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// AgentRole defines the role an agent plays in the system.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentRole string

const (
	// RoleDispatcher is the intake agent that classifies and routes requests.
	RoleDispatcher AgentRole = "dispatcher"
	// RoleExecutor is a specialist agent that executes specific types of tasks.
	RoleExecutor AgentRole = "executor"
	// RoleReviewer is an agent that reviews and validates work.
	RoleReviewer AgentRole = "reviewer"
	// RoleBot is a persistent autonomous agent that runs on triggers.
	RoleBot AgentRole = "bot"
)

// AgentConstraints defines operational limits for an agent.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentConstraints struct {
	// MaxIterations is the maximum number of reasoning cycles.
	MaxIterations int `json:"max_iterations"`
	// Timeout is the maximum duration for a single request.
	Timeout time.Duration `json:"timeout"`
	// MaxTokensPerTurn is the maximum tokens to generate per turn.
	MaxTokensPerTurn int `json:"max_tokens_per_turn,omitempty"`
	// MaxMemoryRefs is the maximum memory references to inject.
	MaxMemoryRefs int `json:"max_memory_refs,omitempty"`
	// MaxConversationTokens is the total token budget for a single conversation turn.
	// When exceeded, the agent stops gracefully. 0 means use the default.
	MaxConversationTokens int `json:"max_conversation_tokens,omitempty"`

	// Inference parameter overrides (nil = use model default)
	// Temperature controls randomness. Lower values are more deterministic.
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP controls nucleus sampling. 1.0 means no nucleus sampling.
	TopP *float64 `json:"top_p,omitempty"`
	// FrequencyPenalty penalizes tokens based on frequency in the text so far.
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	// PresencePenalty penalizes tokens based on whether they appear in the text so far.
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`
	// StopSequences are sequences where the model will stop generating.
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// DefaultConstraints returns sensible default constraints.
func DefaultConstraints() AgentConstraints {
	return AgentConstraints{
		MaxIterations:    25,
		Timeout:          5 * time.Minute,
		MaxTokensPerTurn: 4096,
		MaxMemoryRefs:    20,
	}
}

// AgentSpec defines the specification for creating an agent.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentSpec struct {
	// ID is the unique identifier for this agent specification.
	ID string `json:"id"`
	// Name is a human-readable name for the agent.
	Name string `json:"name"`
	// Role defines the agent's role in the system.
	Role AgentRole `json:"role"`
	// Description is a one-liner surfaced in API/UI displays.
	Description string `json:"description,omitempty"`
	// Enabled reports whether the agent is active. Disabled agents are filtered
	// out at load time and never instantiated.
	Enabled bool `json:"enabled"`
	// CanDelegate controls whether the delegate_task tool is available to this
	// agent. When false, delegate_task is stripped from the filtered tool set.
	CanDelegate bool `json:"can_delegate"`
	// ReviewsDomain, set on reviewer-role agents, declares which review domain
	// (code|debug|plan|analysis|test) the reviewer covers. ReviewPolicy uses
	// this for dynamic reviewer selection.
	ReviewsDomain string `json:"reviews_domain,omitempty"`
	// Purpose is a description of what this agent does (used in system prompt).
	Purpose string `json:"purpose"`
	// Model can be an alias name (e.g., "coder"), a direct model reference (e.g., "zai/glm-4.7"),
	// or empty to use the default. If it matches a known alias, alias resolution with
	// automatic failover and cooldown rotation is used.
	Model string `json:"model,omitempty"`
	// AdditionalTools are tools beyond the baseline that this agent has access to.
	AdditionalTools []string `json:"additional_tools,omitempty"`
	// Constraints are operational limits for this agent.
	Constraints AgentConstraints `json:"constraints"`
	// SystemPromptSections are additional prompt sections for this agent.
	SystemPromptSections []string `json:"system_prompt_sections,omitempty"`
	// AvailableSkills lists skill names this agent can invoke.
	AvailableSkills []string `json:"available_skills,omitempty"`
	// SkillTriggers maps keywords to skill names for automatic invocation.
	SkillTriggers map[string]string `json:"skill_triggers,omitempty"`
}

// BaselineTools are the tools available to all agents.
var BaselineTools = []string{
	ToolMemoryStore,
	ToolMemorySearch,
	ToolMemoryGetContext,
	"task_create",
	"task_get",
	"task_list",
	"task_update",
	ToolPlatformStatus,
	ToolPlatformAgents,
	ToolPlatformTools,
	"delegate_task",
}

// ExecutorAgentIDs returns the canonical list of executor agent IDs (excluding
// the dispatcher, which routes but does not execute jobs). The IDs are returned
// in a stable order suitable for deterministic worker bootstrapping.
func ExecutorAgentIDs() []string {
	return []string{
		config.AgentIDChat,
		config.AgentIDCoder,
		config.AgentIDDebugger,
		config.AgentIDPlanner,
		config.AgentIDAnalyst,
		config.AgentIDResearcher,
		config.AgentIDCommitter,
		config.AgentIDScheduler,
	}
}

// HasTool checks if the agent spec includes a tool (baseline or additional).
func (s *AgentSpec) HasTool(tool string) bool {
	// Check baseline tools
	if slices.Contains(BaselineTools, tool) {
		return true
	}
	// Check additional tools
	return slices.Contains(s.AdditionalTools, tool)
}

// AllTools returns all tools available to this agent.
func (s *AgentSpec) AllTools() []string {
	tools := make([]string, 0, len(BaselineTools)+len(s.AdditionalTools))
	tools = append(tools, BaselineTools...)
	tools = append(tools, s.AdditionalTools...)
	return tools
}

// HasSkill checks if the agent spec includes a specific skill.
func (s *AgentSpec) HasSkill(skillName string) bool {
	return slices.Contains(s.AvailableSkills, skillName)
}

// GetSkillForTrigger returns the skill name for a trigger keyword, or empty string if not found.
func (s *AgentSpec) GetSkillForTrigger(keyword string) string {
	if s.SkillTriggers == nil {
		return ""
	}
	return s.SkillTriggers[keyword]
}
