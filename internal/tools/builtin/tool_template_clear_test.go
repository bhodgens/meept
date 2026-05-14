package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/templates"
)

func TestTemplateClearTool_NameAndDescription(t *testing.T) {
	tool := NewTemplateClearTool(nil)

	if tool.Name() != "template_clear" {
		t.Errorf("expected name 'template_clear', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(desc, "template") {
		t.Error("description should mention template")
	}
}

func TestTemplateClearTool_Parameters(t *testing.T) {
	tool := NewTemplateClearTool(nil)
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("expected type 'object', got %q", params.Type)
	}

	if len(params.Properties) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(params.Properties))
	}

	// conversation_id is required
	if len(params.Required) != 1 || params.Required[0] != "conversation_id" {
		t.Errorf("expected required=['conversation_id'], got %v", params.Required)
	}
}

func TestTemplateClearTool_NilRegistry(t *testing.T) {
	tool := NewTemplateClearTool(nil)
	result, err := tool.Execute(context.TODO(), map[string]any{
		"conversation_id": "conv-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clearResult := result.(TemplateClearResult)
	if clearResult.Success {
		t.Error("expected failure with nil registry")
	}
	if clearResult.Error != "template registry not available" {
		t.Errorf("unexpected error: %q", clearResult.Error)
	}
}

func TestTemplateClearTool_MissingConversationID(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateClearTool(reg)

	result, err := tool.Execute(context.TODO(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clearResult := result.(TemplateClearResult)
	if clearResult.Success {
		t.Error("expected failure with missing conversation_id")
	}
	if clearResult.Error != "conversation_id is required" {
		t.Errorf("unexpected error: %q", clearResult.Error)
	}
}

func TestTemplateClearTool_ClearSpecificTemplate(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "always-french",
		Scope: templates.ScopeSession,
		Body:  "Respond in French.",
	})
	reg.Register(&templates.Template{
		Name:  "always-formal",
		Scope: templates.ScopeSession,
		Body:  "Always use formal tone.",
	})

	// Activate both
	if err := reg.ActivateSessionTemplate("conv-1", "always-french", nil); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}
	if err := reg.ActivateSessionTemplate("conv-1", "always-formal", nil); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	tool := NewTemplateClearTool(reg)

	result, err := tool.Execute(context.TODO(), map[string]any{
		"conversation_id": "conv-1",
		"name":            "always-french",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clearResult := result.(TemplateClearResult)
	if !clearResult.Success {
		t.Fatalf("expected success, got error: %q", clearResult.Error)
	}
	if clearResult.Count != 1 {
		t.Errorf("expected 1 cleared, got %d", clearResult.Count)
	}
	if len(clearResult.Cleared) != 1 || clearResult.Cleared[0] != "always-french" {
		t.Errorf("expected cleared=['always-french'], got %v", clearResult.Cleared)
	}

	// Verify the other template is still active
	active := reg.GetActiveTemplates("conv-1")
	if len(active) != 1 {
		t.Fatalf("expected 1 remaining active template, got %d", len(active))
	}
	if active[0].Name != "always-formal" {
		t.Errorf("expected remaining 'always-formal', got %q", active[0].Name)
	}
}

func TestTemplateClearTool_ClearAllTemplates(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "always-french",
		Scope: templates.ScopeSession,
		Body:  "Respond in French.",
	})
	reg.Register(&templates.Template{
		Name:  "always-formal",
		Scope: templates.ScopeSession,
		Body:  "Always use formal tone.",
	})

	// Activate both
	if err := reg.ActivateSessionTemplate("conv-1", "always-french", nil); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}
	if err := reg.ActivateSessionTemplate("conv-1", "always-formal", nil); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	tool := NewTemplateClearTool(reg)

	result, err := tool.Execute(context.TODO(), map[string]any{
		"conversation_id": "conv-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clearResult := result.(TemplateClearResult)
	if !clearResult.Success {
		t.Fatalf("expected success, got error: %q", clearResult.Error)
	}
	if clearResult.Count != 2 {
		t.Errorf("expected 2 cleared, got %d", clearResult.Count)
	}

	// Verify no templates remain active
	active := reg.GetActiveTemplates("conv-1")
	if len(active) != 0 {
		t.Errorf("expected 0 active templates, got %d", len(active))
	}
}

func TestTemplateClearTool_ClearNonexistentTemplate(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateClearTool(reg)

	result, err := tool.Execute(context.TODO(), map[string]any{
		"conversation_id": "conv-1",
		"name":            "nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clearResult := result.(TemplateClearResult)
	if clearResult.Success {
		t.Error("expected failure when clearing nonexistent template")
	}
	if !strings.Contains(clearResult.Error, "not active") {
		t.Errorf("expected 'not active' error, got %q", clearResult.Error)
	}
}

func TestTemplateClearTool_ClearAllNoTemplates(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateClearTool(reg)

	result, err := tool.Execute(context.TODO(), map[string]any{
		"conversation_id": "conv-empty",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clearResult := result.(TemplateClearResult)
	if !clearResult.Success {
		t.Fatalf("expected success, got error: %q", clearResult.Error)
	}
	if clearResult.Count != 0 {
		t.Errorf("expected 0 cleared, got %d", clearResult.Count)
	}
}
