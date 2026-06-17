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
	UIType       string   `json:"ui_type,omitempty"`
}

// skillUIType returns the UI type for a skill, preferring explicit UIType
// from metadata and falling back to tag-based derivation.
func skillUIType(skill *skills.Skill) string {
	if skill.UIType != "" {
		return skill.UIType
	}
	return deriveUIType(skill.Tags)
}

// SkillUIDescriptor describes the UI rendering hints for a skill.
type SkillUIDescriptor struct {
	Slug        string        `json:"slug"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	UIType      string        `json:"ui_type"`
	Category    string        `json:"category,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
	Examples    []string      `json:"examples,omitempty"`
	RiskLevel   string        `json:"risk_level,omitempty"`
	Body        string        `json:"body,omitempty"`
	Fields      []UIFieldDef  `json:"fields,omitempty"`
	Actions     []UIActionDef `json:"actions,omitempty"`
}

// UIFieldDef describes a form field for skill UI rendering.
type UIFieldDef struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // "text", "textarea", "select", "number", "boolean"
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
}

// UIActionDef describes an action button for skill UI rendering.
type UIActionDef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`            // "execute", "cancel"
	Style string `json:"style,omitempty"` // "primary", "secondary", "danger"
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
			UIType:       skillUIType(skill),
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
		UIType:       skillUIType(skill),
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

// GetUIDescriptor returns the UI descriptor for a skill, including form fields
// and actions for rendering in the Flutter UI.
func (s *SkillsService) GetUIDescriptor(ctx context.Context, req SkillsGetRequest) (*SkillUIDescriptor, error) {
	if req.Slug == "" {
		return nil, wrapError("skills", "GetUIDescriptor", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("skills", "GetUIDescriptor", ErrUnavailable)
	}

	skill := s.registry.Get(req.Slug)
	if skill == nil {
		return nil, wrapError("skills", "GetUIDescriptor", ErrNotFound)
	}

	// Derive UI type from tags or frontmatter metadata.
	// Skills can declare a "ui" tag (e.g., "panel", "dialog", "external")
	// to hint at how the Flutter UI should render them.
	uiType := deriveUIType(skill.Tags)

	descriptor := &SkillUIDescriptor{
		Slug:        skill.Name,
		Name:        skill.Name,
		Description: skill.Description,
		UIType:      uiType,
		Tags:        skill.Tags,
		Examples:    skill.Examples,
		RiskLevel:   skill.RiskLevel,
		Body:        skill.Body,
	}

	// Build default actions
	descriptor.Actions = []UIActionDef{
		{
			ID:    "execute",
			Label: "execute",
			Type:  "execute",
			Style: "primary",
		},
	}

	// Build default fields: a prompt textarea
	descriptor.Fields = []UIFieldDef{
		{
			Name:        "prompt",
			Label:       "prompt",
			Type:        "textarea",
			Required:    true,
			Placeholder: "describe what you want this skill to do",
		},
	}

	// If the skill has risk_level "high", add a confirmation field
	if skill.RiskLevel == "high" {
		descriptor.Fields = append(descriptor.Fields, UIFieldDef{
			Name:     "confirm",
			Label:    "confirm high-risk execution",
			Type:     "boolean",
			Required: true,
			Help:     "this skill has high risk — confirm before executing",
		})
	}

	return descriptor, nil
}

// deriveUIType extracts the UI type hint from skill tags.
// Looks for tags like "ui:panel", "ui:dialog", "ui:external".
func deriveUIType(tags []string) string {
	for _, tag := range tags {
		if len(tag) > 3 && tag[:3] == "ui:" {
			uiType := tag[3:]
			switch uiType {
			case "panel", "dialog", "external":
				return uiType
			}
		}
	}
	return ""
}
