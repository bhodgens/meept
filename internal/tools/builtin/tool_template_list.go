package builtin

import (
	"context"
	"sort"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/templates"
	"github.com/caimlas/meept/internal/tools"
)

// TemplateListTool allows agents to discover available or active templates.
type TemplateListTool struct {
	registry *templates.Registry
}

// NewTemplateListTool creates a new template list tool.
func NewTemplateListTool(registry *templates.Registry) *TemplateListTool {
	return &TemplateListTool{registry: registry}
}

func (t *TemplateListTool) Name() string { return "template_list" }

func (t *TemplateListTool) Description() string {
	return "List available prompt templates or currently active session-scoped templates. " +
		"Use active=true with a conversation_id to see which templates are currently influencing a conversation."
}

func (t *TemplateListTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"active": {
				Type:        "boolean",
				Description: "If true, list only currently active session-scoped templates for the conversation. Requires conversation_id.",
			},
			"conversation_id": {
				Type:        "string",
				Description: "Required when active=true. The conversation ID to list active templates for.",
			},
		},
		Required: []string{},
	}
}

// TemplateListInfo describes a template for listing purposes.
type TemplateListInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

// ActiveTemplateListInfo describes an active session-scoped template.
type ActiveTemplateListInfo struct {
	Name        string `json:"name"`
	CharCount   int    `json:"char_count"`
	ActivatedAt string `json:"activated_at"`
}

// TemplateListResult is the result of listing templates.
type TemplateListResult struct {
	Templates []TemplateListInfo       `json:"templates"`
	Active    []ActiveTemplateListInfo `json:"active,omitempty"`
	Count     int                      `json:"count"`
	Mode      string                   `json:"mode"` // "all" or "active"
	Error     string                   `json:"error,omitempty"`
}

func (t *TemplateListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return TemplateListResult{
			Templates: []TemplateListInfo{},
			Count:     0,
			Mode:      "all",
		}, nil
	}

	active, _ := args["active"].(bool)

	if active {
		conversationID, _ := args["conversation_id"].(string)
		if conversationID == "" {
			return TemplateListResult{
				Error: "conversation_id is required when active=true",
				Mode:  "active",
			}, nil
		}

		activeTemplates := t.registry.GetActiveTemplates(conversationID)
		activeInfos := make([]ActiveTemplateListInfo, 0, len(activeTemplates))
		for _, at := range activeTemplates {
			activeInfos = append(activeInfos, ActiveTemplateListInfo{
				Name:        at.Name,
				CharCount:   at.CharCount,
				ActivatedAt: at.ActivatedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}

		return TemplateListResult{
			Active: activeInfos,
			Count:  len(activeInfos),
			Mode:   "active",
		}, nil
	}

	// List all available templates
	allTemplates := t.registry.List()
	infos := make([]TemplateListInfo, 0, len(allTemplates))
	for _, tmpl := range allTemplates {
		infos = append(infos, TemplateListInfo{
			Name:        tmpl.Name,
			Description: tmpl.Description,
			Scope:       string(tmpl.Scope),
		})
	}

	// Sort by name for consistent output
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	return TemplateListResult{
		Templates: infos,
		Count:     len(infos),
		Mode:      "all",
	}, nil
}

// Ensure interface compliance
var _ tools.Tool = (*TemplateListTool)(nil)
