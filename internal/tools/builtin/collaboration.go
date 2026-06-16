package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// InitiateCollaborationTool allows agents to request a collaborative session.
type InitiateCollaborationTool struct {
	// callback is called with (mode, task_description, reason, preferred_agents)
	// Returns (session_id, error)
	callback func(ctx context.Context, mode, taskDesc, reason string, preferredAgents []string) (string, error)
}

// NewInitiateCollaborationTool creates a new initiate collaboration tool.
func NewInitiateCollaborationTool() *InitiateCollaborationTool {
	return &InitiateCollaborationTool{}
}

// SetCallback sets the collaboration callback.
func (t *InitiateCollaborationTool) SetCallback(cb func(ctx context.Context, mode, taskDesc, reason string, preferredAgents []string) (string, error)) {
	if cb != nil {
		t.callback = cb
	}
}

func (t *InitiateCollaborationTool) Name() string     { return "initiate_collaboration" }
func (t *InitiateCollaborationTool) Category() string { return "collaboration" }
func (t *InitiateCollaborationTool) Description() string {
	return "Request a collaborative session with another agent when facing an ambiguous or complex problem."
}

func (t *InitiateCollaborationTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"mode": {
				Type:        schemaTypeString,
				Description: "Collaboration mode to use",
				Enum:        []string{"pair_programming", "differential"},
			},
			"task_description": {
				Type:        schemaTypeString,
				Description: "Description of what needs collaboration",
			},
			"reason": {
				Type:        schemaTypeString,
				Description: "Why collaboration is needed (e.g. 'uncertain about the best architecture')",
			},
			"preferred_agents": {
				Type:        schemaTypeArray,
				Description: "Optional agent IDs to involve",
			},
		},
		Required: []string{"mode", "task_description", "reason"},
	}
}

// InitiateCollaborationResult is returned after initiating collaboration.
type InitiateCollaborationResult struct {
	SessionID       string `json:"session_id,omitempty"`
	Success         bool   `json:"success"`
	Mode            string `json:"mode"`
	Message         string `json:"message"`
	EstimatedTokens int64  `json:"estimated_tokens,omitempty"`
}

func (t *InitiateCollaborationTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	mode, _ := args["mode"].(string)
	if mode != "pair_programming" && mode != "differential" {
		return tools.NewErrorResult("mode must be one of: pair_programming, differential"), nil
	}

	taskDesc, _ := args["task_description"].(string)
	if taskDesc == "" {
		return tools.NewErrorResult("task_description is required"), nil
	}

	reason, _ := args["reason"].(string)
	if reason == "" {
		return tools.NewErrorResult("reason is required"), nil
	}

	var preferredAgents []string
	if agentsRaw, ok := args["preferred_agents"].([]any); ok {
		for _, a := range agentsRaw {
			if s, ok := a.(string); ok {
				preferredAgents = append(preferredAgents, s)
			}
		}
	}

	if t.callback == nil {
		return InitiateCollaborationResult{
			Success: false,
			Mode:    mode,
			Message: "collaboration engine not available",
		}, nil
	}

	// Estimate tokens based on task description length (~0.25 tokens per char rough estimate)
	estTokens := int64(len(taskDesc) / 4)
	if estTokens < 100 {
		estTokens = 100
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	sessionID, err := t.callback(ctxWithTimeout, mode, taskDesc, reason, preferredAgents)
	if err != nil {
		return InitiateCollaborationResult{
			Success:         false,
			Mode:            mode,
			Message:         fmt.Sprintf("failed to initiate collaboration: %v", err),
			EstimatedTokens: estTokens,
		}, nil
	}

	return InitiateCollaborationResult{
		SessionID:       sessionID,
		Success:         true,
		Mode:            mode,
		Message:         fmt.Sprintf("Collaboration session %s started in %s mode", sessionID, mode),
		EstimatedTokens: estTokens,
	}, nil
}

// Ensure InitiateCollaborationTool implements the Tool interface.
var _ tools.Tool = (*InitiateCollaborationTool)(nil)
