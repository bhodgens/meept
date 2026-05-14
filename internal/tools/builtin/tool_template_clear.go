package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/templates"
	"github.com/caimlas/meept/internal/tools"
)

// TemplateClearTool allows agents to deactivate session-scoped templates.
type TemplateClearTool struct {
	registry *templates.Registry
}

// NewTemplateClearTool creates a new template clear tool.
func NewTemplateClearTool(registry *templates.Registry) *TemplateClearTool {
	return &TemplateClearTool{registry: registry}
}

func (t *TemplateClearTool) Name() string { return "template_clear" }

func (t *TemplateClearTool) Description() string {
	return "Deactivate session-scoped prompt templates for a conversation. " +
		"Provide a template name to deactivate a specific template, or omit to clear all active session-scoped templates."
}

func (t *TemplateClearTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"name": {
				Type:        "string",
				Description: "Specific template name to deactivate. If omitted, all session-scoped templates for the conversation are cleared.",
			},
			"conversation_id": {
				Type:        "string",
				Description: "The conversation ID to clear templates for.",
			},
		},
		Required: []string{"conversation_id"},
	}
}

// TemplateClearResult is the result of clearing templates.
type TemplateClearResult struct {
	Success bool     `json:"success"`
	Cleared []string `json:"cleared"`
	Count   int      `json:"count"`
	Error   string   `json:"error,omitempty"`
}

func (t *TemplateClearTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return TemplateClearResult{
			Success: false,
			Error:   "template registry not available",
		}, nil
	}

	conversationID, _ := args["conversation_id"].(string)
	if conversationID == "" {
		return TemplateClearResult{
			Success: false,
			Error:   "conversation_id is required",
		}, nil
	}

	name, _ := args["name"].(string)

	if name != "" {
		// Deactivate specific template
		deactivated := t.registry.DeactivateSessionTemplate(conversationID, name)
		if !deactivated {
			return TemplateClearResult{
				Success: false,
				Cleared: []string{},
				Count:   0,
				Error:   "template was not active for this conversation: " + name,
			}, nil
		}

		return TemplateClearResult{
			Success: true,
			Cleared: []string{name},
			Count:   1,
		}, nil
	}

	// Clear all session-scoped templates
	cleared := t.registry.ClearSessionTemplates(conversationID)
	if len(cleared) == 0 {
		return TemplateClearResult{
			Success: true,
			Cleared: []string{},
			Count:   0,
		}, nil
	}

	return TemplateClearResult{
		Success: true,
		Cleared: cleared,
		Count:   len(cleared),
	}, nil
}

// Ensure interface compliance
var _ tools.Tool = (*TemplateClearTool)(nil)
