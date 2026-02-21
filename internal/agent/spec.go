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
}

// DefaultConstraints returns sensible default constraints.
func DefaultConstraints() AgentConstraints {
	return AgentConstraints{
		MaxIterations:    10,
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
	// Model is the LLM model to use (empty means use default).
	Model string `json:"model,omitempty"`
	// AdditionalTools are tools beyond the baseline that this agent has access to.
	AdditionalTools []string `json:"additional_tools,omitempty"`
	// Constraints are operational limits for this agent.
	Constraints AgentConstraints `json:"constraints"`
	// SystemPromptSections are additional prompt sections for this agent.
	SystemPromptSections []string `json:"system_prompt_sections,omitempty"`
}

// BaselineTools are the tools available to all agents.
var BaselineTools = []string{
	"memory.store",
	"memory.search",
	"memory.get_context",
	"task.create",
	"task.query",
	"task.update",
	"platform.status",
}

// DispatcherSpec returns the spec for the dispatcher agent.
func DispatcherSpec() *AgentSpec {
	return &AgentSpec{
		ID:   "dispatcher",
		Name: "Dispatcher Agent",
		Role: RoleDispatcher,
		Purpose: `You are the intake agent for meept. Your role is to:
1. Understand the user's intent from their message
2. Search memory for relevant context
3. Create a task with appropriate memory references
4. Route to the best specialist agent

Always include relevant memory_refs when creating tasks to provide context continuity.`,
		Model: "", // Use default model
		AdditionalTools: []string{
			"classify_intent",
			"delegate",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          30 * time.Second,
			MaxTokensPerTurn: 1024,
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
			"exec_tool",
			"file_read",
			"file_write",
			"file_delete",
			"list_directory",
			"shell_execute",
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
			"exec_tool",
			"file_read",
			"file_write",
			"shell_execute",
			"run_tests",
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
			"decompose_task",
			"create_subtasks",
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
			"web_search",
			"web_fetch",
			"summarize",
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
			"git_status",
			"git_add",
			"git_commit",
			"git_push",
			"git_branch",
			"git_log",
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
			"schedule",
			"list_jobs",
			"cancel_job",
		},
		Constraints: AgentConstraints{
			MaxIterations:    3,
			Timeout:          1 * time.Minute,
			MaxTokensPerTurn: 1024,
			MaxMemoryRefs:    5,
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
