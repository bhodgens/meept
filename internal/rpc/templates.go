package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/templates"
)

// RegisterTemplateHandlers registers template-related RPC handlers directly
// on the RPC server. This follows the same pattern as RegisterSkillsHandlers.
func RegisterTemplateHandlers(server *Server, registry *templates.Registry, executor *skills.Executor) {
	// templates.list - list all discovered templates
	server.RegisterHandler("templates.list", func(ctx context.Context, params json.RawMessage) (any, error) {
		if registry == nil {
			return nil, fmt.Errorf("template registry not configured")
		}

		list := registry.List()
		templatesData := make([]map[string]any, len(list))
		for i, t := range list {
			templatesData[i] = map[string]any{
				RPCKeyName:        t.Name,
				RPCKeyDescription: t.Description,
				"scope":       string(t.Scope),
				RPCKeyPath:        t.Path,
				RPCKeyPriority:    t.Priority,
			}
		}

		return map[string]any{
			"templates": templatesData,
			RPCKeyCount:     len(templatesData),
		}, nil
	})

	// templates.get - get template details by name
	server.RegisterHandler("templates.get", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if registry == nil {
			return nil, fmt.Errorf("template registry not configured")
		}

		tmpl := registry.Get(p.Name)
		if tmpl == nil {
			return nil, fmt.Errorf("template not found: %s", p.Name)
		}

		return map[string]any{
			RPCKeyName:        tmpl.Name,
			RPCKeyDescription: tmpl.Description,
			"scope":       string(tmpl.Scope),
			"body":        tmpl.Body,
			RPCKeyPath:        tmpl.Path,
			RPCKeyPriority:    tmpl.Priority,
		}, nil
	})

	// templates.invoke - substitute args and optionally execute via LLM
	server.RegisterHandler("templates.invoke", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if registry == nil {
			return nil, fmt.Errorf("template registry not configured")
		}

		prompt, err := registry.Substitute(p.Name, p.Args)
		if err != nil {
			return nil, fmt.Errorf("template substitution failed: %w", err)
		}

		// If no executor, return the substituted prompt.
		if executor == nil {
			return map[string]any{
				"prompt":  prompt,
				RPCKeySuccess: true,
			}, nil
		}

		// Execute via skill executor.
		tmpl := registry.Get(p.Name)
		result, err := executor.Execute(ctx, templateToSkill(tmpl), prompt)
		if err != nil {
			return nil, fmt.Errorf("template execution failed: %w", err)
		}

		return map[string]any{
			"prompt":            prompt,
			RPCKeyContent:           result.Content,
			RPCKeyModel:             result.Model,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"total_tokens":      result.TotalTokens,
			RPCKeySuccess:           true,
		}, nil
	})

	// templates.clear - clear session-scoped templates for a conversation
	server.RegisterHandler("templates.clear", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			ConversationID string `json:"conversation_id"`
			Name           string `json:"name,omitempty"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if p.ConversationID == "" {
			return nil, fmt.Errorf("conversation_id is required")
		}

		if registry == nil {
			return nil, fmt.Errorf("template registry not configured")
		}

		var cleared []string
		if p.Name != "" {
			if registry.DeactivateSessionTemplate(p.ConversationID, p.Name) {
				cleared = []string{p.Name}
			}
		} else {
			cleared = registry.ClearSessionTemplates(p.ConversationID)
		}

		return map[string]any{
			"cleared": cleared,
			RPCKeyCount:   len(cleared),
		}, nil
	})
}

// templateToSkill creates a minimal skills.Skill from a template for
// execution through the skill executor.
func templateToSkill(tmpl *templates.Template) *skills.Skill {
	if tmpl == nil {
		return nil
	}
	return &skills.Skill{
		Name:        tmpl.Name,
		Description: tmpl.Description,
		Body:        tmpl.Body,
	}
}
