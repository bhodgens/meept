package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ---------------------------------------------------------------------------
// Shared types for team tool callbacks
// ---------------------------------------------------------------------------

// TeamCreateConfig holds the parameters for creating a new team.
type TeamCreateConfig struct {
	LeadAgent       string
	Roster          []string
	MaxConcurrent   int
	TaskDescription string
	Mode            string
}

// TaskAssignment holds the parameters for assigning a subtask to a team member.
type TaskAssignment struct {
	AgentID  string
	Subtask  string
	Priority string
}

// TeamStatusResult holds the full status of a team returned by GetStatus.
type TeamStatusResult struct {
	SessionID     string                       `json:"session_id"`
	LeadAgent     string                       `json:"lead_agent"`
	Phase         string                       `json:"phase"`
	MemberResults map[string]*MemberStatusInfo `json:"member_results"`
}

// MemberStatusInfo holds the status of a single team member.
type MemberStatusInfo struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// TeamMessage holds the parameters for sending a team message.
type TeamMessage struct {
	Content     string
	TargetAgent string // empty for broadcast
	MessageType string
}

// MemberResult holds the parameters for submitting a partial result.
type MemberResult struct {
	AgentID   string
	Output    string
	Status    string
	Artifacts []string
}

// TeamCallbacks aggregates the callback functions for all team tools.
// The daemon wires these to the TeamOrchestrator at startup.
type TeamCallbacks struct {
	CreateTeam       func(ctx context.Context, config TeamCreateConfig) (string, error)
	CreatePresetTeam func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error)
	AssignTask       func(ctx context.Context, teamID string, assignment TaskAssignment) error
	GetStatus        func(ctx context.Context, teamID string) (*TeamStatusResult, error)
	SendMessage      func(ctx context.Context, teamID string, msg TeamMessage) error
	SubmitResult     func(ctx context.Context, teamID string, result MemberResult) error
}

// ---------------------------------------------------------------------------
// Tool 1: platform_team_create
// ---------------------------------------------------------------------------

// TeamCreateTool allows the lead agent to initiate a new team session.
type TeamCreateTool struct {
	callback func(ctx context.Context, config TeamCreateConfig) (string, error)
}

// NewTeamCreateTool creates a new team create tool.
func NewTeamCreateTool() *TeamCreateTool {
	return &TeamCreateTool{}
}

// SetCallback wires the create callback.
func (t *TeamCreateTool) SetCallback(cb func(ctx context.Context, config TeamCreateConfig) (string, error)) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *TeamCreateTool) Name() string     { return "platform_team_create" }
func (t *TeamCreateTool) Category() string { return "team" }

func (t *TeamCreateTool) Description() string {
	return "Create a new team with a lead agent and specialist roster. " +
		"The lead orchestrates work and synthesizes partial results from team members."
}

func (t *TeamCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"lead_agent": {
				Type:        schemaTypeString,
				Description: "Agent ID of the lead who orchestrates and synthesizes results",
			},
			"roster": {
				Type:        schemaTypeArray,
				Description: "List of specialist agent IDs to assign work to (e.g. ['coder', 'analyst', 'debugger'])",
			},
			"max_concurrent": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of members running concurrently (default 3)",
			},
			"task_description": {
				Type:        schemaTypeString,
				Description: "Description of the overall task the team will work on",
			},
			"mode": {
				Type:        schemaTypeString,
				Description: "Team execution mode (default: team_parallel)",
				Enum:        []string{"team_parallel"},
			},
		},
		Required: []string{"lead_agent", "roster", "task_description"},
	}
}

// TeamCreateResult is returned after creating a team.
type TeamCreateResult struct {
	TeamID       string   `json:"team_id"`
	Success      bool     `json:"success"`
	LeadAgent    string   `json:"lead_agent"`
	Roster       []string `json:"roster"`
	MaxConcurrent int      `json:"max_concurrent"`
	Mode         string   `json:"mode"`
	Message      string   `json:"message"`
}

func (t *TeamCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	leadAgent, _ := args["lead_agent"].(string)
	if leadAgent == "" {
		return tools.NewErrorResult("lead_agent is required"), nil
	}

	taskDescription, _ := args["task_description"].(string)
	if taskDescription == "" {
		return tools.NewErrorResult("task_description is required"), nil
	}

	var roster []string
	if rosterRaw, ok := args["roster"].([]any); ok {
		for _, r := range rosterRaw {
			if s, ok := r.(string); ok && s != "" {
				roster = append(roster, s)
			}
		}
	}
	if len(roster) == 0 {
		return tools.NewErrorResult("roster must contain at least one agent"), nil
	}

	maxConcurrent := 3
	if mc, ok := args["max_concurrent"].(float64); ok && mc > 0 {
		maxConcurrent = int(mc)
	}

	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "team_parallel"
	}

	if t.callback == nil {
		return TeamCreateResult{
			Success: false,
			Message: "team orchestrator not available",
		}, nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	teamID, err := t.callback(ctxWithTimeout, TeamCreateConfig{
		LeadAgent:       leadAgent,
		Roster:          roster,
		MaxConcurrent:   maxConcurrent,
		TaskDescription: taskDescription,
		Mode:            mode,
	})
	if err != nil {
		return TeamCreateResult{
			Success: false,
			Message: fmt.Sprintf("failed to create team: %v", err),
		}, nil
	}

	return TeamCreateResult{
		TeamID:        teamID,
		Success:       true,
		LeadAgent:     leadAgent,
		Roster:        roster,
		MaxConcurrent: maxConcurrent,
		Mode:          mode,
		Message:       fmt.Sprintf("team %s created with lead %s and %d members", teamID, leadAgent, len(roster)),
	}, nil
}

// ---------------------------------------------------------------------------
// Tool 2: team_assign
// ---------------------------------------------------------------------------

// TeamAssignTool assigns a subtask to a specific team member.
type TeamAssignTool struct {
	callback func(ctx context.Context, teamID string, assignment TaskAssignment) error
}

// NewTeamAssignTool creates a new team assign tool.
func NewTeamAssignTool() *TeamAssignTool {
	return &TeamAssignTool{}
}

// SetCallback wires the assign callback.
func (t *TeamAssignTool) SetCallback(cb func(ctx context.Context, teamID string, assignment TaskAssignment) error) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *TeamAssignTool) Name() string     { return "team_assign" }
func (t *TeamAssignTool) Category() string { return "team" }

func (t *TeamAssignTool) Description() string {
	return "Assign a specific subtask to a team member. Use this to distribute work within an active team."
}

func (t *TeamAssignTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"team_id": {
				Type:        schemaTypeString,
				Description: "The team session ID returned by platform_team_create",
			},
			"agent_id": {
				Type:        schemaTypeString,
				Description: "The agent ID of the team member to assign the subtask to",
			},
			"subtask": {
				Type:        schemaTypeString,
				Description: "Description of the subtask to assign",
			},
			schemaPropPriority: {
				Type:        schemaTypeString,
				Description: "Priority level (default: medium)",
				Enum:        []string{"low", "medium", "high", "critical"},
			},
		},
		Required: []string{"team_id", "agent_id", "subtask"},
	}
}

// TeamAssignResult is returned after assigning a subtask.
type TeamAssignResult struct {
	Success  bool   `json:"success"`
	TeamID   string `json:"team_id"`
	AgentID  string `json:"agent_id"`
	Subtask  string `json:"subtask"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

func (t *TeamAssignTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	teamID, _ := args["team_id"].(string)
	if teamID == "" {
		return tools.NewErrorResult("team_id is required"), nil
	}

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return tools.NewErrorResult("agent_id is required"), nil
	}

	subtask, _ := args["subtask"].(string)
	if subtask == "" {
		return tools.NewErrorResult("subtask is required"), nil
	}

	priority, _ := args["priority"].(string)
	if priority == "" {
		priority = "medium"
	}

	if t.callback == nil {
		return TeamAssignResult{
			Success: false,
			TeamID:  teamID,
			Message: "team orchestrator not available",
		}, nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := t.callback(ctxWithTimeout, teamID, TaskAssignment{
		AgentID:  agentID,
		Subtask:  subtask,
		Priority: priority,
	}); err != nil {
		return TeamAssignResult{
			Success: false,
			TeamID:  teamID,
			AgentID: agentID,
			Message: fmt.Sprintf("failed to assign subtask: %v", err),
		}, nil
	}

	return TeamAssignResult{
		Success:  true,
		TeamID:   teamID,
		AgentID:  agentID,
		Subtask:  subtask,
		Priority: priority,
		Message:  fmt.Sprintf("assigned subtask to %s on team %s with priority %s", agentID, teamID, priority),
	}, nil
}

// ---------------------------------------------------------------------------
// Tool 3: team_status
// ---------------------------------------------------------------------------

// TeamStatusTool checks the progress of all team members.
type TeamStatusTool struct {
	callback func(ctx context.Context, teamID string) (*TeamStatusResult, error)
}

// NewTeamStatusTool creates a new team status tool.
func NewTeamStatusTool() *TeamStatusTool {
	return &TeamStatusTool{}
}

// SetCallback wires the status callback.
func (t *TeamStatusTool) SetCallback(cb func(ctx context.Context, teamID string) (*TeamStatusResult, error)) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *TeamStatusTool) Name() string     { return "team_status" }
func (t *TeamStatusTool) Category() string { return "team" }

func (t *TeamStatusTool) Description() string {
	return "Check the current status of all team members, including their progress and results."
}

func (t *TeamStatusTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"team_id": {
				Type:        schemaTypeString,
				Description: "The team session ID returned by platform_team_create",
			},
		},
		Required: []string{"team_id"},
	}
}

func (t *TeamStatusTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	teamID, _ := args["team_id"].(string)
	if teamID == "" {
		return tools.NewErrorResult("team_id is required"), nil
	}

	if t.callback == nil {
		return tools.NewErrorResult("team orchestrator not available"), nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := t.callback(ctxWithTimeout, teamID)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to get team status: %v", err)), nil
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Tool 4: team_message
// ---------------------------------------------------------------------------

// TeamMessageTool sends a broadcast or targeted message within a team.
type TeamMessageTool struct {
	callback func(ctx context.Context, teamID string, msg TeamMessage) error
}

// NewTeamMessageTool creates a new team message tool.
func NewTeamMessageTool() *TeamMessageTool {
	return &TeamMessageTool{}
}

// SetCallback wires the message callback.
func (t *TeamMessageTool) SetCallback(cb func(ctx context.Context, teamID string, msg TeamMessage) error) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *TeamMessageTool) Name() string     { return "team_message" }
func (t *TeamMessageTool) Category() string { return "team" }

func (t *TeamMessageTool) Description() string {
	return "Send a message to team members. If target_agent is empty, broadcasts to all members. " +
		"Use for coordination, sharing findings, or requesting help."
}

func (t *TeamMessageTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"team_id": {
				Type:        schemaTypeString,
				Description: "The team session ID returned by platform_team_create",
			},
			schemaPropContent: {
				Type:        schemaTypeString,
				Description: "The message content to send",
			},
			"target_agent": {
				Type:        schemaTypeString,
				Description: "Optional agent ID to target a specific member. If empty, broadcasts to all.",
			},
			"message_type": {
				Type:        schemaTypeString,
				Description: "Type of message (default: info)",
				Enum:        []string{"info", "request", "alert", "result"},
			},
		},
		Required: []string{"team_id", "content"},
	}
}

// TeamMessageResult is returned after sending a team message.
type TeamMessageResult struct {
	Success     bool   `json:"success"`
	TeamID      string `json:"team_id"`
	TargetAgent string `json:"target_agent,omitempty"`
	MessageType string `json:"message_type"`
	Message     string `json:"message"`
}

func (t *TeamMessageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	teamID, _ := args["team_id"].(string)
	if teamID == "" {
		return tools.NewErrorResult("team_id is required"), nil
	}

	content, _ := args["content"].(string)
	if content == "" {
		return tools.NewErrorResult("content is required"), nil
	}

	targetAgent, _ := args["target_agent"].(string)
	messageType, _ := args["message_type"].(string)
	if messageType == "" {
		messageType = "info"
	}

	if t.callback == nil {
		return TeamMessageResult{
			Success: false,
			TeamID:  teamID,
			Message: "team orchestrator not available",
		}, nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := t.callback(ctxWithTimeout, teamID, TeamMessage{
		Content:     content,
		TargetAgent: targetAgent,
		MessageType: messageType,
	}); err != nil {
		return TeamMessageResult{
			Success: false,
			TeamID:  teamID,
			Message: fmt.Sprintf("failed to send message: %v", err),
		}, nil
	}

	targetDesc := "all members"
	if targetAgent != "" {
		targetDesc = targetAgent
	}

	return TeamMessageResult{
		Success:     true,
		TeamID:      teamID,
		TargetAgent: targetAgent,
		MessageType: messageType,
		Message:     fmt.Sprintf("message delivered to %s on team %s", targetDesc, teamID),
	}, nil
}

// ---------------------------------------------------------------------------
// Tool 5: team_result
// ---------------------------------------------------------------------------

// TeamResultTool allows a team member to submit their partial result.
type TeamResultTool struct {
	callback func(ctx context.Context, teamID string, result MemberResult) error
}

// NewTeamResultTool creates a new team result tool.
func NewTeamResultTool() *TeamResultTool {
	return &TeamResultTool{}
}

// SetCallback wires the result callback.
func (t *TeamResultTool) SetCallback(cb func(ctx context.Context, teamID string, result MemberResult) error) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *TeamResultTool) Name() string     { return "team_result" }
func (t *TeamResultTool) Category() string { return "team" }

func (t *TeamResultTool) Description() string {
	return "Submit a partial result from a team member. The lead agent will aggregate " +
		"all results and synthesize a final output."
}

func (t *TeamResultTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"team_id": {
				Type:        schemaTypeString,
				Description: "The team session ID returned by platform_team_create",
			},
			"agent_id": {
				Type:        schemaTypeString,
				Description: "The agent ID of the team member submitting the result",
			},
			"output": {
				Type:        schemaTypeString,
				Description: "The output or findings from this member's work",
			},
			schemaPropStatus: {
				Type:        schemaTypeString,
				Description: "Result status (default: completed)",
				Enum:        []string{"completed", "failed", "partial"},
			},
			"artifacts": {
				Type:        schemaTypeArray,
				Description: "Optional list of artifact paths or identifiers produced by this member",
			},
		},
		Required: []string{"team_id", "agent_id", "output"},
	}
}

// TeamResultSubmitResult is returned after submitting a team result.
type TeamResultSubmitResult struct {
	Success  bool     `json:"success"`
	TeamID   string   `json:"team_id"`
	AgentID  string   `json:"agent_id"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
}

func (t *TeamResultTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	teamID, _ := args["team_id"].(string)
	if teamID == "" {
		return tools.NewErrorResult("team_id is required"), nil
	}

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return tools.NewErrorResult("agent_id is required"), nil
	}

	output, _ := args["output"].(string)
	if output == "" {
		return tools.NewErrorResult("output is required"), nil
	}

	status, _ := args["status"].(string)
	if status == "" {
		status = "completed"
	}

	var artifacts []string
	if artRaw, ok := args["artifacts"].([]any); ok {
		for _, a := range artRaw {
			if s, ok := a.(string); ok && s != "" {
				artifacts = append(artifacts, s)
			}
		}
	}

	if t.callback == nil {
		return TeamResultSubmitResult{
			Success: false,
			TeamID:  teamID,
			Message: "team orchestrator not available",
		}, nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := t.callback(ctxWithTimeout, teamID, MemberResult{
		AgentID:   agentID,
		Output:    output,
		Status:    status,
		Artifacts: artifacts,
	}); err != nil {
		return TeamResultSubmitResult{
			Success: false,
			TeamID:  teamID,
			AgentID: agentID,
			Message: fmt.Sprintf("failed to submit result: %v", err),
		}, nil
	}

	return TeamResultSubmitResult{
		Success: true,
		TeamID:  teamID,
		AgentID: agentID,
		Status:  status,
		Message: fmt.Sprintf("result from %s accepted by team %s", agentID, teamID),
	}, nil
}

// ---------------------------------------------------------------------------
// Tool 6: team_preset_create
// ---------------------------------------------------------------------------

// TeamPresetCreateTool creates a team from a predefined preset configuration.
type TeamPresetCreateTool struct {
	callback func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error)
}

// NewTeamPresetCreateTool creates a new team preset create tool.
func NewTeamPresetCreateTool() *TeamPresetCreateTool {
	return &TeamPresetCreateTool{}
}

// SetCallback wires the preset create callback.
func (t *TeamPresetCreateTool) SetCallback(cb func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error)) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *TeamPresetCreateTool) Name() string     { return "team_preset_create" }
func (t *TeamPresetCreateTool) Category() string { return "team" }

func (t *TeamPresetCreateTool) Description() string {
	return "Create a team from a predefined preset. Available presets: " +
		"hyperplan (5 critic agents review a plan simultaneously) and " +
		"security_research (3 vulnerability hunters + 2 PoC engineers). " +
		"The preset configures the lead agent, roster, and specialist roles automatically."
}

func (t *TeamPresetCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"preset_name": {
				Type:        schemaTypeString,
				Description: "Name of the preset to use (e.g. 'hyperplan', 'security_research')",
				Enum:        []string{"hyperplan", "security_research"},
			},
			"task_description": {
				Type:        schemaTypeString,
				Description: "Description of the task the preset team will work on",
			},
			"max_concurrent": {
				Type:        schemaTypeInteger,
				Description: "Optional override for max concurrent members (0 = use preset default)",
			},
		},
		Required: []string{"preset_name", "task_description"},
	}
}

// TeamPresetCreateResult is returned after creating a team from a preset.
type TeamPresetCreateResult struct {
	Success      bool     `json:"success"`
	TeamID       string   `json:"team_id"`
	PresetName   string   `json:"preset_name"`
	LeadAgent    string   `json:"lead_agent"`
	Roster       []string `json:"roster"`
	MaxConcurrent int      `json:"max_concurrent"`
	Message      string   `json:"message"`
}

func (t *TeamPresetCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	presetName, _ := args["preset_name"].(string)
	if presetName == "" {
		return tools.NewErrorResult("preset_name is required"), nil
	}

	taskDescription, _ := args["task_description"].(string)
	if taskDescription == "" {
		return tools.NewErrorResult("task_description is required"), nil
	}

	maxConcurrent := 0
	if mc, ok := args["max_concurrent"].(float64); ok && mc > 0 {
		maxConcurrent = int(mc)
	}

	if t.callback == nil {
		return TeamPresetCreateResult{
			Success:    false,
			PresetName: presetName,
			Message:    "team orchestrator not available",
		}, nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	teamID, err := t.callback(ctxWithTimeout, presetName, taskDescription, maxConcurrent)
	if err != nil {
		return TeamPresetCreateResult{
			Success:    false,
			PresetName: presetName,
			Message:    fmt.Sprintf("failed to create preset team: %v", err),
		}, nil
	}

	return TeamPresetCreateResult{
		Success:    true,
		TeamID:     teamID,
		PresetName: presetName,
		Message:    fmt.Sprintf("team created from preset %q with ID %s", presetName, teamID),
	}, nil
}

// ---------------------------------------------------------------------------
// RegisterTeamTools wires all team tools into the registry using the
// provided TeamCallbacks. If callbacks is nil, the tools are still registered
// but will return "not available" errors when executed.
// ---------------------------------------------------------------------------

// RegisterTeamTools registers all team tools with the given callbacks.
func RegisterTeamTools(registry *tools.Registry, callbacks *TeamCallbacks) {
	createTool := NewTeamCreateTool()
	presetTool := NewTeamPresetCreateTool()
	assignTool := NewTeamAssignTool()
	statusTool := NewTeamStatusTool()
	messageTool := NewTeamMessageTool()
	resultTool := NewTeamResultTool()

	if callbacks != nil {
		if callbacks.CreateTeam != nil {
			createTool.SetCallback(callbacks.CreateTeam)
		}
		if callbacks.CreatePresetTeam != nil {
			presetTool.SetCallback(callbacks.CreatePresetTeam)
		}
		if callbacks.AssignTask != nil {
			assignTool.SetCallback(callbacks.AssignTask)
		}
		if callbacks.GetStatus != nil {
			statusTool.SetCallback(callbacks.GetStatus)
		}
		if callbacks.SendMessage != nil {
			messageTool.SetCallback(callbacks.SendMessage)
		}
		if callbacks.SubmitResult != nil {
			resultTool.SetCallback(callbacks.SubmitResult)
		}
	}

	registry.Register(createTool)
	registry.Register(presetTool)
	registry.Register(assignTool)
	registry.Register(statusTool)
	registry.Register(messageTool)
	registry.Register(resultTool)
}

// Ensure all team tools implement the Tool interface.
var (
	_ tools.Tool = (*TeamCreateTool)(nil)
	_ tools.Tool = (*TeamPresetCreateTool)(nil)
	_ tools.Tool = (*TeamAssignTool)(nil)
	_ tools.Tool = (*TeamStatusTool)(nil)
	_ tools.Tool = (*TeamMessageTool)(nil)
	_ tools.Tool = (*TeamResultTool)(nil)
)
