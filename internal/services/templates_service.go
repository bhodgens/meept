package services

import (
	"context"

	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/templates"
)

// TemplatesService handles template operations.
type TemplatesService struct {
	registry *templates.Registry
	executor *skills.Executor
}

// NewTemplatesService creates a templates service.
func NewTemplatesService(reg *templates.Registry, exec *skills.Executor) *TemplatesService {
	return &TemplatesService{
		registry: reg,
		executor: exec,
	}
}

// TemplatesListRequest contains list parameters.
type TemplatesListRequest struct {
	Limit int `json:"limit,omitempty"`
}

// TemplateInfo contains template information for API responses.
type TemplateInfo struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Scope       templates.TemplateScope `json:"scope"`
	Path        string                  `json:"path,omitempty"`
	Priority    int                     `json:"priority"`
	Body        string                  `json:"body,omitempty"`
}

// List returns available templates.
func (s *TemplatesService) List(ctx context.Context, req TemplatesListRequest) ([]TemplateInfo, error) {
	if s.registry == nil {
		return nil, wrapError("templates", "List", ErrUnavailable)
	}

	list := s.registry.List()
	result := make([]TemplateInfo, 0, len(list))
	for _, tmpl := range list {
		result = append(result, TemplateInfo{
			Name:        tmpl.Name,
			Description: tmpl.Description,
			Scope:       tmpl.Scope,
			Path:        tmpl.Path,
			Priority:    tmpl.Priority,
		})
		if req.Limit > 0 && len(result) >= req.Limit {
			break
		}
	}
	return result, nil
}

// TemplatesGetRequest contains get parameters.
type TemplatesGetRequest struct {
	Name string `json:"name"`
}

// Get retrieves a template by name.
func (s *TemplatesService) Get(ctx context.Context, req TemplatesGetRequest) (*TemplateInfo, error) {
	if req.Name == "" {
		return nil, wrapError("templates", "Get", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("templates", "Get", ErrUnavailable)
	}

	tmpl := s.registry.Get(req.Name)
	if tmpl == nil {
		return nil, wrapError("templates", "Get", ErrNotFound)
	}

	return &TemplateInfo{
		Name:        tmpl.Name,
		Description: tmpl.Description,
		Scope:       tmpl.Scope,
		Path:        tmpl.Path,
		Priority:    tmpl.Priority,
		Body:        tmpl.Body,
	}, nil
}

// TemplatesInvokeRequest contains invocation parameters.
type TemplatesInvokeRequest struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
}

// TemplatesInvokeResult contains invocation results.
type TemplatesInvokeResult struct {
	Prompt  string `json:"prompt"`
	Output  string `json:"output,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Invoke substitutes arguments into a template and optionally executes it
// through the skill executor's LLM client.
func (s *TemplatesService) Invoke(ctx context.Context, req TemplatesInvokeRequest) (*TemplatesInvokeResult, error) {
	if req.Name == "" {
		return nil, wrapError("templates", "Invoke", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("templates", "Invoke", ErrUnavailable)
	}

	prompt, err := s.registry.Substitute(req.Name, req.Args)
	if err != nil {
		return nil, wrapError("templates", "Invoke", err)
	}

	// If no executor, return the substituted prompt without LLM execution.
	if s.executor == nil {
		return &TemplatesInvokeResult{
			Prompt:  prompt,
			Success: true,
		}, nil
	}

	// Execute via the skill executor by constructing a minimal skill.
	// The executor handles model resolution and LLM invocation.
	// We create a lightweight skill from the template for execution.
	tmpl := s.registry.Get(req.Name)
	if tmpl == nil {
		return nil, wrapError("templates", "Invoke", ErrNotFound)
	}
	result, err := s.executor.Execute(ctx, templateToSkill(tmpl), prompt)
	if err != nil {
		return &TemplatesInvokeResult{
			Prompt:  prompt,
			Success: false,
			Error:   err.Error(),
		}, wrapError("templates", "Invoke", err)
	}

	return &TemplatesInvokeResult{
		Prompt:  prompt,
		Output:  result.Content,
		Success: true,
	}, nil
}

// TemplatesClearRequest contains clear parameters.
type TemplatesClearRequest struct {
	ConversationID string `json:"conversation_id"`
	Name           string `json:"name,omitempty"` // If set, deactivate only this template
}

// TemplatesClearResult contains clear results.
type TemplatesClearResult struct {
	Cleared []string `json:"cleared"`
}

// ClearSession clears session-scoped templates for a conversation.
func (s *TemplatesService) ClearSession(ctx context.Context, req TemplatesClearRequest) (*TemplatesClearResult, error) {
	if req.ConversationID == "" {
		return nil, wrapError("templates", "ClearSession", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("templates", "ClearSession", ErrUnavailable)
	}

	var cleared []string
	if req.Name != "" {
		if s.registry.DeactivateSessionTemplate(req.ConversationID, req.Name) {
			cleared = []string{req.Name}
		}
	} else {
		cleared = s.registry.ClearSessionTemplates(req.ConversationID)
	}

	return &TemplatesClearResult{Cleared: cleared}, nil
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
