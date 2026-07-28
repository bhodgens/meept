package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ProjectInfoTool returns information about the currently bound project:
// name, directory path, git branch, dirty status, and detected language.
//
// The tool resolves the working directory from (in priority order):
//  1. context.Context (set by the agent loop per-execution via
//     tools.ContextWithWorkingDir — no shared mutable state)
//  2. workingDirFunc (set via SetWorkingDirFunc, for non-loop callers)
//  3. the getInfo closure's own fallback (typically os.Getwd())
type ProjectInfoTool struct {
	tools.ToolDefaults
	getInfo        func(workingDir string) map[string]any
	workingDirFunc func() string
}

// NewProjectInfoTool creates a new project info tool.
func NewProjectInfoTool(getInfo func(workingDir string) map[string]any) *ProjectInfoTool {
	return &ProjectInfoTool{getInfo: getInfo}
}

// SetWorkingDirFunc sets a fallback working directory resolver for callers
// that don't inject workingDir via context. Prefer ContextWithWorkingDir.
func (t *ProjectInfoTool) SetWorkingDirFunc(fn func() string) {
	if fn != nil {
		t.workingDirFunc = fn
	}
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
	// Priority 1: context-injected working directory (per-execution,
	// set by the agent loop — no shared mutable state).
	workingDir := tools.WorkingDirFromContext(ctx)
	// Priority 2: fallback resolver (for non-loop callers).
	if workingDir == "" && t.workingDirFunc != nil {
		workingDir = t.workingDirFunc()
	}
	info := t.getInfo(workingDir)
	if info == nil {
		return map[string]any{"status": "no project bound"}, nil
	}
	return info, nil
}
