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
	tool := NewProjectInfoTool(func() map[string]any {
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
	tool := NewProjectInfoTool(func() map[string]any {
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
