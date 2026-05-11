package services

import (
	"context"

	"github.com/caimlas/meept/internal/skills"
)

// SkillsService handles skills operations.
type SkillsService struct {
	registry *skills.Registry
	executor *skills.Executor
}

// NewSkillsService creates a skills service.
func NewSkillsService(reg *skills.Registry, exec *skills.Executor) *SkillsService {
	return &SkillsService{
		registry: reg,
		executor: exec,
	}
}

// SkillsListRequest contains list parameters.
type SkillsListRequest struct {
	Category string `json:"category,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// SkillInfo contains skill information for API responses.
type SkillInfo struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Category     string   `json:"category,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Enabled      bool     `json:"enabled"`
}

// List returns available skills.
func (s *SkillsService) List(ctx context.Context, req SkillsListRequest) ([]SkillInfo, error) {
	if s.registry == nil {
		return nil, wrapError("skills", "List", ErrUnavailable)
	}

	names := s.registry.Names()
	result := make([]SkillInfo, 0, len(names))
	for _, name := range names {
		skill := s.registry.Get(name)
		if skill == nil {
			continue
		}
		result = append(result, SkillInfo{
			Slug:         skill.Name,
			Name:         skill.Name,
			Description:  skill.Description,
			Capabilities: skill.Requires,
			Enabled:      true,
		})
		if req.Limit > 0 && len(result) >= req.Limit {
			break
		}
	}
	return result, nil
}

// SkillsGetRequest contains get parameters.
type SkillsGetRequest struct {
	Slug string `json:"slug"`
}

// Get retrieves a skill by slug.
func (s *SkillsService) Get(ctx context.Context, req SkillsGetRequest) (*SkillInfo, error) {
	if req.Slug == "" {
		return nil, wrapError("skills", "Get", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("skills", "Get", ErrUnavailable)
	}

	skill := s.registry.Get(req.Slug)
	if skill == nil {
		return nil, wrapError("skills", "Get", ErrNotFound)
	}

	return &SkillInfo{
		Slug:         skill.Name,
		Name:         skill.Name,
		Description:  skill.Description,
		Capabilities: skill.Requires,
		Enabled:      true,
	}, nil
}

// ExecuteRequest contains execution parameters.
type ExecuteRequest struct {
	Slug   string `json:"slug"`
	Prompt string `json:"prompt"`
}

// ExecuteResult contains execution results.
type ExecuteResult struct {
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Execute runs a skill.
func (s *SkillsService) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	if req.Slug == "" {
		return nil, wrapError("skills", "Execute", ErrInvalidInput)
	}
	if req.Prompt == "" {
		return nil, wrapError("skills", "Execute", ErrInvalidInput)
	}
	if s.executor == nil {
		return &ExecuteResult{
			Success: false,
			Error:   "skill executor not available",
		}, wrapError("skills", "Execute", ErrUnavailable)
	}

	skill := s.registry.Get(req.Slug)
	if skill == nil {
		return &ExecuteResult{
			Success: false,
			Error:   "skill not found: " + req.Slug,
		}, nil
	}

	// Execute the skill
	result, err := s.executor.Execute(ctx, skill, req.Prompt)
	if err != nil {
		return &ExecuteResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	return &ExecuteResult{
		Output:  result.Content,
		Success: true,
	}, nil
}
