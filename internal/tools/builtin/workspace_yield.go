package builtin

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// WorkspaceYieldTool allows agents in a pair programming session to end their turn
// and optionally approve, request changes, or request the editor token.
type WorkspaceYieldTool struct {
	// callback is invoked when the tool is executed. Registered by the CollaborationEngine.
	callback func(ctx context.Context, action, feedback string) error
}

// NewWorkspaceYieldTool creates a new workspace yield tool.
func NewWorkspaceYieldTool() *WorkspaceYieldTool {
	return &WorkspaceYieldTool{}
}

// SetCallback sets the callback for when an agent yields.
func (t *WorkspaceYieldTool) SetCallback(cb func(ctx context.Context, action, feedback string) error) {
	t.callback = cb
}

func (t *WorkspaceYieldTool) Name() string        { return "workspace_yield" }
func (t *WorkspaceYieldTool) Category() string    { return "collaboration" }
func (t *WorkspaceYieldTool) Description() string {
	return "End your turn as the active driver in a pair programming session. " +
		"Optionally approve the current state, request changes, or request the token."
}

func (t *WorkspaceYieldTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"action": {
				Type:        schemaTypeString,
				Description: "approve = pass turn to other agent; " +
					"request_changes = ask other to fix something; " +
					"request_token = take over as driver",
				Enum: []string{"approve", "request_changes", "request_token"},
			},
			"feedback": {
				Type:        schemaTypeString,
				Description: "Context for the other agent (e.g. 'the sort function looks correct but add a nil check')",
			},
		},
		Required: []string{"action"},
	}
}

// WorkspaceYieldResult is returned to the LLM after yield.
type WorkspaceYieldResult struct {
	Success  bool   `json:"success"`
	Action   string `json:"action"`
	Feedback string `json:"feedback,omitempty"`
	Message  string `json:"message"`
}

func (t *WorkspaceYieldTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	if action != "approve" && action != "request_changes" && action != "request_token" {
		return tools.NewErrorResult("action must be one of: approve, request_changes, request_token"), nil
	}

	feedback, _ := args["feedback"].(string)

	if t.callback != nil {
		if err := t.callback(ctx, action, feedback); err != nil {
			return WorkspaceYieldResult{
				Success: false,
				Action:  action,
				Message: fmt.Sprintf("yield callback failed: %v", err),
			}, nil
		}
	}

	msg := "Turn ended. "
	switch action {
	case "approve":
		msg += "You approved the current state and passed the turn."
	case "request_changes":
		msg += "You requested changes. The other agent will address: " + feedback
	case "request_token":
		msg += "You requested the editor token. If approved, you will become the driver."
	}

	return WorkspaceYieldResult{
		Success:  true,
		Action:   action,
		Feedback: feedback,
		Message:  msg,
	}, nil
}

// Ensure WorkspaceYieldTool implements the Tool interface.
var _ tools.Tool = (*WorkspaceYieldTool)(nil)
