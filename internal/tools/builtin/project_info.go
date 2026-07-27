package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ProjectInfoTool returns information about the currently bound project:
// name, directory path, git branch, dirty status, and detected language.
// It uses an injected closure (resolved at wiring time from the loop/session
// context) so the tool stays decoupled from agent internals.
type ProjectInfoTool struct {
	tools.ToolDefaults
	getInfo func() map[string]any
}

// NewProjectInfoTool creates a new project info tool.
// getInfo may be nil; Execute falls back to a "no project bound" result.
func NewProjectInfoTool(getInfo func() map[string]any) *ProjectInfoTool {
	return &ProjectInfoTool{getInfo: getInfo}
}

func (t *ProjectInfoTool) Name() string { return "project_info" }

func (t *ProjectInfoTool) Category() string { return "platform" }

func (t *ProjectInfoTool) Description() string {
	return "Get information about the current project: name, directory path, git branch, dirty status, and detected language. Use this when asked about the current project."
}

func (t *ProjectInfoTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

func (t *ProjectInfoTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.getInfo == nil {
		return map[string]any{"status": "no project bound"}, nil
	}
	info := t.getInfo()
	if info == nil {
		return map[string]any{"status": "no project bound"}, nil
	}
	return info, nil
}
