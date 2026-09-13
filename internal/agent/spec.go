package agent

import (
	"slices"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
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

	// Reasoning holds optional per-agent reasoning effort configuration.
	// When non-nil, the agent loop uses this as the initial reasoning tier
	// and (if AllowSelfModulation is true) the bounds for self-modulation.
	Reasoning *llm.AgentReasoningConfig `json:"reasoning,omitempty"`
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
	// Intents lists the classifier lanes (intent names, e.g. "code", "debug")
	// this agent is the routing destination for. Derived from the AGENT.md
	// `intents:` frontmatter. internal/agent builds its lane-to-agent routing
	// index from these, so a new specialist is routable with a frontmatter
	// edit alone. Empty = the agent declares no lanes.
	Intents []string `json:"intents,omitempty"`
	// Purpose is a description of what this agent does (used in system prompt).
	Purpose string `json:"purpose"`
	// Model can be an alias name (e.g., "coder"), a direct model reference (e.g., "zai/glm-4.7"),
	// or empty to use the default. If it matches a known alias, alias resolution with
	// automatic failover and cooldown rotation is used.
	Model string `json:"model,omitempty"`
	// EnhancerModel is the small/cheap model used to expand a brief into
	// generator-ready prose. Empty = alias "small".
	EnhancerModel string `json:"enhancer_model,omitempty"`
	// EscalationModel is the model (alias name or "provider/model" ref) used
	// for the next fix attempt when verification exhausts max_fix_loops.
	// Empty = escalation disabled; hook falls back to escalate-to-user.
	EscalationModel string `json:"escalation_model,omitempty" yaml:"escalation_model,omitempty"`
	// AdditionalTools are tools beyond the baseline that this agent has access to.
	AdditionalTools []string `json:"additional_tools,omitempty"`
	// ToolScopeLimit caps how many tools are offered to this agent in the
	// request (tool-list scoping). 0 (default) = no cap: every baseline +
	// additional tool is offered, which preserves the existing behavior.
	// A positive value offers only the first ToolScopeLimit names from
	// AdditionalTools followed by BaselineTools (see ScopedToolNames).
	// Reason: a forced tool call (models.json5 `tool_choice: "required"`)
	// must choose among few candidates — the measured failure was 81 tools
	// offered at once. See docs/reference/agent-loop-tools.md.
	ToolScopeLimit int `json:"tool_scope_limit,omitempty"`
	// Constraints are operational limits for this agent.
	Constraints AgentConstraints `json:"constraints"`
	// SystemPromptSections are additional prompt sections for this agent.
	SystemPromptSections []string `json:"system_prompt_sections,omitempty"`
	// AvailableSkills lists skill names this agent can invoke.
	AvailableSkills []string `json:"available_skills,omitempty"`
	// SkillTriggers maps keywords to skill names for automatic invocation.
	SkillTriggers map[string]string `json:"skill_triggers,omitempty"`
	// Verification configures post-completion verification for this agent.
	Verification VerificationConfig `json:"verification" yaml:"verification"`
	// Gate configures the roster quality gate (from AGENT.md `gate:`) run
	// after turns that mutated the workspace. Nil = no gate. This is the
	// per-agent AGENT.md path, orthogonal to employees.defaults.gate.enabled
	// (the employee kill switch).
	Gate *RosterGateConfig `json:"gate,omitempty" yaml:"gate,omitempty"`
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
	ToolRequestHandoff, // safe for every specialist: pure bus publish; runaway cascades are bounded by the orchestrator's maxHandoffSteps (tactical.go)
	"project_info",
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

// AllTools returns all tools available to this agent, as declared (baseline +
// additional, in that order). The declared names are NOT normalized to
// registry names — use GrantedToolNames for that.
func (s *AgentSpec) AllTools() []string {
	tools := make([]string, 0, len(BaselineTools)+len(s.AdditionalTools))
	tools = append(tools, BaselineTools...)
	tools = append(tools, s.AdditionalTools...)
	return tools
}

// toolGrantAliases maps agent-grant names — the vocabulary used in AGENT.md
// `additional_tools` and in BaselineTools — to the registered tool names in
// the tool registry (tools.Tool.Name()). The registry is keyed by the
// registered name, and FilteredToolRegistry matches grants against those keys
// exactly, so an unnormalized grant is silently dropped: the coder's AGENT.md
// grants `shell_execute` while the registered tool is `shell`, so the shell
// tool was never offered to the coder at all.
//
// Keep this list tiny and one-directional (grant -> registered name). Add an
// entry only when a grant token and a registry name genuinely diverge.
var toolGrantAliases = map[string]string{
	"shell_execute": "shell",
}

// CanonicalToolName returns the registered tool name for an agent grant.
// Unknown names pass through unchanged.
func CanonicalToolName(name string) string {
	if canonical, ok := toolGrantAliases[name]; ok {
		return canonical
	}
	return name
}

// GrantedToolNames returns the agent's declared tools (baseline + additional)
// normalized to registered registry names. It is the allow-list the registry
// filters against, so a grant of `shell_execute` resolves to the registered
// `shell` tool.
func (s *AgentSpec) GrantedToolNames() []string {
	declared := s.AllTools()
	out := make([]string, 0, len(declared))
	for _, name := range declared {
		out = append(out, CanonicalToolName(name))
	}
	return out
}

// ScopedToolNames returns the tool names offered to this agent under
// tool-list scoping, capped at limit.
//
//   - limit <= 0: no cap — the full normalized grant set (GrantedToolNames),
//     i.e. the existing behavior.
//   - limit > 0: at most `limit` names, ordered AdditionalTools first (the
//     task-relevant tools the agent was granted for its job — these survive
//     the cap) then BaselineTools (the always-available platform/task tools),
//     de-duplicated, with names normalized to registry names.
//
// The cap exists because a forced tool call must choose among few candidates:
// with a large candidate set a `tool_choice: "required"` request produces
// spurious calls (measured; see docs/reference/agent-loop-tools.md).
func (s *AgentSpec) ScopedToolNames(limit int) []string {
	if limit <= 0 {
		return s.GrantedToolNames()
	}
	names := make([]string, 0, limit)
	seen := make(map[string]bool, limit)
	add := func(list []string) {
		for _, raw := range list {
			if len(names) >= limit {
				return
			}
			name := CanonicalToolName(raw)
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	add(s.AdditionalTools) // task-relevant tools first: they survive the cap
	add(BaselineTools)     // then the always-available baseline
	return names
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

// DefaultEnhancerModelAlias is the model alias used when EnhancerModel is empty.
const DefaultEnhancerModelAlias = "small"

// EffectiveEnhancerModel returns the enhancer model: the explicit override
// if set, otherwise the "small" alias (resolves to small_model).
func (s *AgentSpec) EffectiveEnhancerModel() string {
	if s == nil || s.EnhancerModel == "" {
		return DefaultEnhancerModelAlias
	}
	return s.EnhancerModel
}
