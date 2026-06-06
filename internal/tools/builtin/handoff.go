package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// handoffBus is the minimal bus interface needed by RequestHandoffTool.
type handoffBus interface {
	Publish(topic string, msg *models.BusMessage) int
}

// HandoffPayload is the bus event payload published by request_handoff.
type HandoffPayload struct {
	TaskID        string `json:"task_id"`
	FromStepID    string `json:"from_step_id"`
	FromAgentID   string `json:"from_agent_id"`
	ToAgentID     string `json:"to_agent_id"`
	Description   string `json:"description"`
	ToolHint      string `json:"tool_hint,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
	InjectAfter   bool   `json:"inject_after"`
	Timestamp     string `json:"timestamp"`
}

// HandoffResult is returned to the calling agent.
type HandoffResult struct {
	Success     bool   `json:"success"`
	TaskID      string `json:"task_id"`
	ToAgentID   string `json:"to_agent_id"`
	Description string `json:"description"`
	Message     string `json:"message"`
	Error       string `json:"error,omitempty"`
}

// RequestHandoffTool allows an agent to request a handoff to another agent mid-task.
type RequestHandoffTool struct {
	bus         handoffBus
	agentExists func(agentID string) bool
}

// NewRequestHandoffTool creates a new request_handoff tool.
func NewRequestHandoffTool(bus handoffBus, agentExist func(agentID string) bool) *RequestHandoffTool {
	return &RequestHandoffTool{
		bus:         bus,
		agentExists: agentExist,
	}
}

func (t *RequestHandoffTool) Name() string { return "request_handoff" }

func (t *RequestHandoffTool) Category() string { return "platform" }

func (t *RequestHandoffTool) Description() string {
	return "Request a mid-task handoff to another agent. Publishes a handoff event that the orchestrator processes to inject the target agent into the task. Use platform_agents first to discover available agents."
}

func (t *RequestHandoffTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"task_id": {
				Type:        schemaTypeString,
				Description: "The ID of the task being handed off.",
			},
			"from_step_id": {
				Type:        schemaTypeString,
				Description: "The ID of the current step where the handoff originates.",
			},
			"from_agent_id": {
				Type:        schemaTypeString,
				Description: "Your agent ID (the agent requesting the handoff).",
			},
			"to_agent_id": {
				Type:        schemaTypeString,
				Description: "The ID of the agent to hand off to (e.g., 'coder', 'debugger', 'planner').",
			},
			"description": {
				Type:        schemaTypeString,
				Description: "Description of what needs to be done by the target agent.",
			},
			"tool_hint": {
				Type:        schemaTypeString,
				Description: "Optional hint about which tools the target agent should use.",
			},
			"reason": {
				Type:        schemaTypeString,
				Description: "Optional explanation of why the handoff is needed.",
			},
			"partial_result": {
				Type:        schemaTypeString,
				Description: "Optional partial result or context from the current agent to pass along.",
			},
		},
		Required: []string{"task_id", "from_step_id", "to_agent_id", "description"},
	}
}

// handoffTopic is the bus topic for handoff events.
const handoffTopic = "orchestrator.handoff"

func (t *RequestHandoffTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Extract required fields
	taskID, _ := args["task_id"].(string)
	fromStepID, _ := args["from_step_id"].(string)
	toAgentID, _ := args["to_agent_id"].(string)
	description, _ := args["description"].(string)

	// Validate required fields
	if taskID == "" {
		return HandoffResult{
			TaskID:      taskID,
			ToAgentID:   toAgentID,
			Description: description,
			Success:     false,
			Error:       "task_id is required",
		}, nil
	}
	if fromStepID == "" {
		return HandoffResult{
			TaskID:      taskID,
			ToAgentID:   toAgentID,
			Description: description,
			Success:     false,
			Error:       "from_step_id is required",
		}, nil
	}
	if toAgentID == "" {
		return HandoffResult{
			TaskID:      taskID,
			ToAgentID:   toAgentID,
			Description: description,
			Success:     false,
			Error:       "to_agent_id is required",
		}, nil
	}
	if description == "" {
		return HandoffResult{
			TaskID:      taskID,
			ToAgentID:   toAgentID,
			Description: description,
			Success:     false,
			Error:       "description is required",
		}, nil
	}

	// Validate target agent exists
	if t.agentExists != nil && !t.agentExists(toAgentID) {
		return HandoffResult{
			TaskID:      taskID,
			ToAgentID:   toAgentID,
			Description: description,
			Success:     false,
			Error:       fmt.Sprintf("agent %q not found. Use the platform_agents tool to discover available agents.", toAgentID),
		}, nil
	}

	// Extract optional fields
	fromAgentID, _ := args["from_agent_id"].(string)
	toolHint, _ := args["tool_hint"].(string)
	reason, _ := args["reason"].(string)
	partialResult, _ := args["partial_result"].(string)

	// Build payload
	now := time.Now().Format(time.RFC3339)
	payload := HandoffPayload{
		TaskID:        taskID,
		FromStepID:    fromStepID,
		FromAgentID:   fromAgentID,
		ToAgentID:     toAgentID,
		Description:   description,
		ToolHint:      toolHint,
		Reason:        reason,
		PartialResult: partialResult,
		InjectAfter:   true,
		Timestamp:     now,
	}

	// Publish bus event as a proper BusMessage
	busMsg, err := models.NewBusMessage(models.MessageTypeEvent, "request_handoff", payload)
	if err != nil {
		return HandoffResult{
			Success: false,
			TaskID:  taskID,
			Error:   fmt.Sprintf("failed to create bus message: %v", err),
		}, nil
	}

	if t.bus != nil {
		t.bus.Publish(handoffTopic, busMsg)
	}

	return HandoffResult{
		Success:     true,
		TaskID:      taskID,
		ToAgentID:   toAgentID,
		Description: description,
		Message:     fmt.Sprintf("handoff requested to agent %q for task %q", toAgentID, taskID),
	}, nil
}

// Ensure RequestHandoffTool implements the Tool and TerminatingTool interfaces.
var _ tools.Tool = (*RequestHandoffTool)(nil)
var _ tools.TerminatingTool = (*RequestHandoffTool)(nil)

// TerminateHint implements tools.TerminatingTool.
func (t *RequestHandoffTool) TerminateHint(args map[string]any) bool { return true }
