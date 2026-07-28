package builtin

import (
	"context"
	"testing"
)

func TestProjectInfoToolNameCategory(t *testing.T) {
	tool := NewProjectInfoTool(nil)
	if tool.Name() != "project_info" {
		t.Errorf("expected name 'project_info', got %q", tool.Name())
	}
	if tool.Category() != "platform" {
		t.Errorf("expected category 'platform', got %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestProjectInfoToolParameters(t *testing.T) {
	tool := NewProjectInfoTool(nil)
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("expected object type, got %q", params.Type)
	}
	if len(params.Required) != 0 {
		t.Errorf("expected no required params, got %v", params.Required)
	}
}

func TestProjectInfoToolNilFunc(t *testing.T) {
	tool := NewProjectInfoTool(nil)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["status"] != "no project bound" {
		t.Errorf("expected 'no project bound', got %v", m["status"])
	}
}

func TestProjectInfoToolNilReturn(t *testing.T) {
	tool := NewProjectInfoTool(func(workingDir string) map[string]any {
		return nil
	})
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["status"] != "no project bound" {
		t.Errorf("expected 'no project bound', got %v", m["status"])
	}
}

func TestProjectInfoToolWithData(t *testing.T) {
	expected := map[string]any{
		"name":   "meept",
		"path":   "/Users/caimlas/git/meept",
		"branch": "main",
		"dirty":  true,
	}
	tool := NewProjectInfoTool(func(workingDir string) map[string]any {
		return expected
	})
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["name"] != "meept" {
		t.Errorf("expected name 'meept', got %v", m["name"])
	}
	if m["branch"] != "main" {
		t.Errorf("expected branch 'main', got %v", m["branch"])
	}
}

// TestProjectInfoToolWorkingDirFunc verifies that SetWorkingDirFunc causes
// Execute to pass the resolved directory to the getInfo closure.
func TestProjectInfoToolWorkingDirFunc(t *testing.T) {
	var receivedDir string
	tool := NewProjectInfoTool(func(workingDir string) map[string]any {
		receivedDir = workingDir
		return map[string]any{"name": "test", "path": workingDir}
	})
	tool.SetWorkingDirFunc(func() string { return "/custom/session/path" })
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["path"] != "/custom/session/path" {
		t.Errorf("expected path '/custom/session/path', got %v", m["path"])
	}
	if receivedDir != "/custom/session/path" {
		t.Errorf("closure received %q, want '/custom/session/path'", receivedDir)
	}
}

// TestProjectInfoToolWorkingDirFuncEmptyFallack verifies that when
// workingDirFunc returns an empty string, Execute passes empty to the
// closure (which is expected to fall back to os.Getwd()).
func TestProjectInfoToolWorkingDirFuncEmptyFallback(t *testing.T) {
	var receivedDir string
	tool := NewProjectInfoTool(func(workingDir string) map[string]any {
		receivedDir = workingDir
		return map[string]any{"status": "fallback"}
	})
	tool.SetWorkingDirFunc(func() string { return "" })
	_, _ = tool.Execute(context.Background(), nil)
	if receivedDir != "" {
		t.Errorf("closure received %q, want empty (fallback)", receivedDir)
	}
}
