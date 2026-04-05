package agent

import (
	"time"
)

// AgentRole defines the role an agent plays in the system.
type AgentRole string

const (
	// RoleDispatcher is the intake agent that classifies and routes requests.
	RoleDispatcher AgentRole = "dispatcher"
	// RoleExecutor is a specialist agent that executes specific types of tasks.
	RoleExecutor AgentRole = "executor"
	// RoleReviewer is an agent that reviews and validates work.
	RoleReviewer AgentRole = "reviewer"
)

// AgentConstraints defines operational limits for an agent.
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
type AgentSpec struct {
	// ID is the unique identifier for this agent specification.
	ID string `json:"id"`
	// Name is a human-readable name for the agent.
	Name string `json:"name"`
	// Role defines the agent's role in the system.
	Role AgentRole `json:"role"`
	// Purpose is a description of what this agent does (used in system prompt).
	Purpose string `json:"purpose"`
	// Model can be an alias name (e.g., "coder"), a direct model reference (e.g., "zai/glm-4.7"),
	// or empty to use the default. If it matches a known alias, alias resolution is used.
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
	"memory_store",
	"memory_search",
	"memory_get_context",
	"task_create",
	"task_get",
	"task_list",
	"task_update",
	"platform_status",
	"platform_agents",
	"platform_tools",
}

// DispatcherSpec returns the spec for the dispatcher agent.
func DispatcherSpec() *AgentSpec {
	return &AgentSpec{
		ID:   "dispatcher",
		Name: "Dispatcher Agent",
		Role: RoleDispatcher,
		Purpose: `You are the intake agent for meept. Your role is to:
1. Understand the user's intent from their message
2. Search memory for relevant context using memory_search
3. Discover available specialist agents using platform_agents
4. Create a task with task_create and route to the best specialist agent

## Coworker Awareness
Use platform_agents to discover available specialist agents. Each agent has:
- ID: The unique identifier used for routing (e.g., "coder", "planner", "analyst")
- Name: Human-readable name
- Role: Either "executor" (does work) or "dispatcher" (routes work)
- Purpose: What this agent specializes in

Common specialists:
- coder: File operations, shell commands, coding tasks
- planner: Breaking complex tasks into steps, project planning
- analyst: Data analysis, research, investigation
- debugger: Troubleshooting, error analysis, fixing bugs
- committer: Git operations, commits, PR management
- scheduler: Job scheduling, recurring tasks

## Task Routing
When creating a task, specify the target agent in the task metadata.
Include relevant memory_refs to provide context continuity to the specialist.

Always include relevant memory_refs when creating tasks to provide context continuity.`,
		Model: "", // Use default model
		AdditionalTools: []string{
			"delegate_task",
		},
		Constraints: AgentConstraints{
			MaxIterations:    5,
			Timeout:          60 * time.Second,
			MaxTokensPerTurn: 2048,
			MaxMemoryRefs:    10,
		},
	}
}

// ChatAgentSpec returns the spec for the general chat agent.
func ChatAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "chat",
		Name:    "Chat Agent",
		Role:    RoleExecutor,
		Purpose: "You are a helpful conversational assistant with full tool access.",
		Model:   "",
		AdditionalTools: []string{
			"web_fetch",
			"web_search",
		},
		Constraints: DefaultConstraints(),
	}
}

// CoderAgentSpec returns the spec for the coding specialist agent.
func CoderAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "coder",
		Name:    "Coder Agent",
		Role:    RoleExecutor,
		Purpose: "You are a coding specialist. You can read, write, and modify files, execute shell commands, and work with MCP servers.",
		Model:   "",
		AdditionalTools: []string{
			"file_read",
			"file_write",
			"file_delete",
			"list_directory",
			"shell_execute",
			// NOTE: exec_tool does not exist yet
		},
		Constraints: DefaultConstraints(),
	}
}

// DebuggerAgentSpec returns the spec for the debugging specialist agent.
func DebuggerAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "debugger",
		Name:    "Debugger Agent",
		Role:    RoleExecutor,
		Purpose: "You are a debugging specialist. You diagnose issues, trace problems, and help fix bugs in code.",
		Model:   "",
		AdditionalTools: []string{
			"file_read",
			"file_write",
			"shell_execute",
			// NOTE: exec_tool and run_tests do not exist yet
		},
		Constraints: DefaultConstraints(),
	}
}

// PlannerAgentSpec returns the spec for the planning specialist agent.
func PlannerAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "planner",
		Name:    "Planner Agent",
		Role:    RoleExecutor,
		Purpose: "You are a planning specialist. You decompose complex tasks into smaller subtasks and create execution plans.",
		Model:   "", // Use reasoning model if available
		AdditionalTools: []string{
			// NOTE: decompose_task and create_subtasks tools do not exist yet
		},
		Constraints: AgentConstraints{
			MaxIterations:    5,
			Timeout:          3 * time.Minute,
			MaxTokensPerTurn: 4096,
			MaxMemoryRefs:    15,
		},
	}
}

// AnalystAgentSpec returns the spec for the analysis specialist agent.
func AnalystAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "analyst",
		Name:    "Analyst Agent",
		Role:    RoleExecutor,
		Purpose: "You are an analysis specialist. You research topics, summarize information, and provide insights.",
		Model:   "",
		AdditionalTools: []string{
			"web_fetch",
			"web_search",
			// NOTE: summarize tool does not exist yet
		},
		Constraints: DefaultConstraints(),
	}
}

// CommitterAgentSpec returns the spec for the git operations agent.
func CommitterAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "committer",
		Name:    "Committer Agent",
		Role:    RoleExecutor,
		Purpose: "You are a git operations specialist. You handle commits, branches, and repository management.",
		Model:   "", // Use cheap model
		AdditionalTools: []string{
			"shell_execute", // Use shell_execute for git commands
			// NOTE: git_* tools do not exist yet
		},
		Constraints: AgentConstraints{
			MaxIterations:    5,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
			MaxMemoryRefs:    5,
		},
	}
}

// SchedulerAgentSpec returns the spec for the scheduling agent.
func SchedulerAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "scheduler",
		Name:    "Scheduler Agent",
		Role:    RoleExecutor,
		Purpose: "You are a scheduling specialist. You create, manage, and cancel scheduled tasks and reminders.",
		Model:   "", // Use cheap model
		AdditionalTools: []string{
			"schedule_create",
			"schedule_list",
			"schedule_delete",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          1 * time.Minute,
			MaxTokensPerTurn: 1024,
			MaxMemoryRefs:    5,
		},
	}
}

// CodeReviewerSpec returns the spec for the code reviewer agent.
func CodeReviewerSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "code-reviewer",
		Name:    "Code Reviewer Agent",
		Role:    RoleReviewer,
		Purpose: `You are a code review specialist. Your role is to review code changes for:
1. Correctness: Does the code accomplish what was intended?
2. Style: Does the code follow best practices and idiomatic patterns?
3. Security: Are there any security vulnerabilities or potential issues?
4. Completeness: Is anything missing? Are error cases handled?

When reviewing, provide specific, actionable feedback. If issues are minor, you may approve with notes.
For serious issues, reject with clear explanation of what needs to be fixed.

Always respond with JSON: {"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0}`,
		Model: "",
		AdditionalTools: []string{
			"file_read",
			"memory_search",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
			MaxMemoryRefs:    10,
		},
	}
}

// TestReviewerSpec returns the spec for the test reviewer agent.
func TestReviewerSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "test-reviewer",
		Name:    "Test Reviewer Agent",
		Role:    RoleReviewer,
		Purpose: `You are a test verification specialist. Your role is to verify that work is complete and correct by:
1. Checking that the stated work was actually done
2. Verifying outputs match expectations
3. Running tests if appropriate
4. Validating results

You are pragmatic: if the work looks good and accomplishes the stated goal, approve it quickly.
Don't be overly nitpicky - focus on actual problems that would prevent the work from being useful.

Always respond with JSON: {"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0}`,
		Model: "",
		AdditionalTools: []string{
			"shell_execute",
			"file_read",
		},
		Constraints: AgentConstraints{
			MaxIterations:    5,
			Timeout:          3 * time.Minute,
			MaxTokensPerTurn: 2048,
		},
	}
}

// DebugReviewerSpec returns the spec for the debug reviewer agent.
func DebugReviewerSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "debug-reviewer",
		Name:    "Debug Reviewer Agent",
		Role:    RoleReviewer,
		Purpose: `You are a debugging review specialist. Your role is to review debugging work for:
1. Root cause analysis: Was the actual problem identified?
2. Solution effectiveness: Will the fix actually resolve the issue?
3. Side effects: Could the fix introduce new problems?
4. Testing: Was the fix verified to work?

Debugging work should be practical and focused. Approve if the approach is sound even if not perfect.

Always respond with JSON: {"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0}`,
		Model: "",
		AdditionalTools: []string{
			"file_read",
			"memory_search",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
		},
	}
}

// AnalystReviewerSpec returns the spec for the analyst reviewer agent.
func AnalystReviewerSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "analyst-reviewer",
		Name:    "Analyst Reviewer Agent",
		Role:    RoleReviewer,
		Purpose: `You are an analysis review specialist. Your role is to review analytical work for:
1. Accuracy: Is the information correct and well-sourced?
2. Completeness: Are all relevant aspects considered?
3. Clarity: Is the analysis well-structured and understandable?
4. Actionability: Does the analysis lead to clear conclusions or next steps?

Analysis work should be thorough but not excessively verbose. Approve if the key insights are captured.

Always respond with JSON: {"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0}`,
		Model: "",
		AdditionalTools: []string{
			"web_search",
			"web_fetch",
			"memory_search",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
		},
	}
}

// PlannerReviewerSpec returns the spec for the planner reviewer agent.
func PlannerReviewerSpec() *AgentSpec {
	return &AgentSpec{
		ID:      "planner-reviewer",
		Name:    "Planner Reviewer Agent",
		Role:    RoleReviewer,
		Purpose: `You are a planning review specialist. Your role is to review execution plans for:
1. Feasibility: Can the plan actually be executed as described?
2. Completeness: Are all necessary steps included?
3. Ordering: Are steps in a logical sequence with appropriate dependencies?
4. Risk: Are there obvious risks or missing considerations?

Plans should be actionable and clear. Minor gaps are acceptable if the overall direction is sound.

Always respond with JSON: {"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0}`,
		Model: "",
		AdditionalTools: []string{
			"memory_search",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
		},
	}
}

// DefaultSpecs returns all default agent specifications.
func DefaultSpecs() []*AgentSpec {
	return []*AgentSpec{
		DispatcherSpec(),
		ChatAgentSpec(),
		CoderAgentSpec(),
		DebuggerAgentSpec(),
		PlannerAgentSpec(),
		AnalystAgentSpec(),
		CommitterAgentSpec(),
		SchedulerAgentSpec(),
		CodeReviewerSpec(),
		TestReviewerSpec(),
		DebugReviewerSpec(),
		AnalystReviewerSpec(),
		PlannerReviewerSpec(),
	}
}

// HasTool checks if the agent spec includes a tool (baseline or additional).
func (s *AgentSpec) HasTool(tool string) bool {
	// Check baseline tools
	for _, t := range BaselineTools {
		if t == tool {
			return true
		}
	}
	// Check additional tools
	for _, t := range s.AdditionalTools {
		if t == tool {
			return true
		}
	}
	return false
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
	for _, sk := range s.AvailableSkills {
		if sk == skillName {
			return true
		}
	}
	return false
}

// GetSkillForTrigger returns the skill name for a trigger keyword, or empty string if not found.
func (s *AgentSpec) GetSkillForTrigger(keyword string) string {
	if s.SkillTriggers == nil {
		return ""
	}
	return s.SkillTriggers[keyword]
}
