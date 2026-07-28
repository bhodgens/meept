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
//
// The getInfo closure accepts a workingDir string resolved at Execute time.
// When workingDirFunc is set (via SetWorkingDirFunc), Execute calls it to
// obtain the session-scoped directory; otherwise it passes an empty string
// and the closure falls back to its own resolution (typically os.Getwd()).
type ProjectInfoTool struct {
	tools.ToolDefaults
	getInfo       func(workingDir string) map[string]any
	workingDirFunc func() string
}

// NewProjectInfoTool creates a new project info tool.
// getInfo may be nil; Execute falls back to a "no project bound" result.
// The getInfo closure receives a workingDir string: when non-empty, it should
// use that directory for probing; when empty, it should fall back to its own
// resolution (e.g. os.Getwd()).
func NewProjectInfoTool(getInfo func(workingDir string) map[string]any) *ProjectInfoTool {
	return &ProjectInfoTool{getInfo: getInfo}
}

// SetWorkingDirFunc sets a session-scoped working directory resolver. When set,
// Execute calls fn() to obtain the working directory and passes it to the
// getInfo closure, overriding the closure's own directory resolution. If fn
// returns an empty string, the closure falls back to its own resolution.
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
	// Resolve session-scoped working directory from the injected func.
	// When unset or empty, pass empty so the closure falls back to os.Getwd().
	var workingDir string
	if t.workingDirFunc != nil {
		workingDir = t.workingDirFunc()
	}
	info := t.getInfo(workingDir)
	if info == nil {
		return map[string]any{"status": "no project bound"}, nil
	}
	return info, nil
}
