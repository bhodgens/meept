package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/templates"
)

func TestTemplateListTool_NameAndDescription(t *testing.T) {
	tool := NewTemplateListTool(nil)

	if tool.Name() != "template_list" {
		t.Errorf("expected name 'template_list', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(desc, "template") {
		t.Error("description should mention template")
	}
}

func TestTemplateListTool_Parameters(t *testing.T) {
	tool := NewTemplateListTool(nil)
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("expected type 'object', got %q", params.Type)
	}

	if len(params.Properties) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(params.Properties))
	}

	if len(params.Required) != 0 {
		t.Errorf("expected no required parameters, got %v", params.Required)
	}
}

func TestTemplateListTool_NilRegistry(t *testing.T) {
	tool := NewTemplateListTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listResult := result.(TemplateListResult)
	if listResult.Count != 0 {
		t.Errorf("expected 0 templates, got %d", listResult.Count)
	}
	if listResult.Mode != "all" {
		t.Errorf("expected mode 'all', got %q", listResult.Mode)
	}
}

func TestTemplateListTool_ListAllTemplates(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "summarize",
		Description: "Summarize text concisely",
		Scope:       templates.ScopeTurn,
		Body:        "Summarize: $@",
	})
	reg.Register(&templates.Template{
		Name:        "always-french",
		Description: "Always respond in French",
		Scope:       templates.ScopeSession,
		Body:        "Respond in French.",
	})

	tool := NewTemplateListTool(reg)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listResult := result.(TemplateListResult)
	if listResult.Count != 2 {
		t.Fatalf("expected 2 templates, got %d", listResult.Count)
	}
	if listResult.Mode != "all" {
		t.Errorf("expected mode 'all', got %q", listResult.Mode)
	}

	// Check sorted order
	if listResult.Templates[0].Name != "always-french" {
		t.Errorf("expected first template 'always-french', got %q", listResult.Templates[0].Name)
	}
	if listResult.Templates[1].Name != "summarize" {
		t.Errorf("expected second template 'summarize', got %q", listResult.Templates[1].Name)
	}

	// Check scope field
	if listResult.Templates[0].Scope != "session" {
		t.Errorf("expected scope 'session', got %q", listResult.Templates[0].Scope)
	}
	if listResult.Templates[1].Scope != "turn" {
		t.Errorf("expected scope 'turn', got %q", listResult.Templates[1].Scope)
	}
}

func TestTemplateListTool_ListEmpty(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateListTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listResult := result.(TemplateListResult)
	if listResult.Count != 0 {
		t.Errorf("expected 0 templates, got %d", listResult.Count)
	}
}

func TestTemplateListTool_ActiveTemplates(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "always-french",
		Scope: templates.ScopeSession,
		Body:  "Respond in French.",
	})

	// Activate a session template
	err := reg.ActivateSessionTemplate("conv-1", "always-french", nil)
	if err != nil {
		t.Fatalf("failed to activate session template: %v", err)
	}

	tool := NewTemplateListTool(reg)
	result, err := tool.Execute(context.Background(), map[string]any{
		"active":          true,
		"conversation_id": "conv-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listResult := result.(TemplateListResult)
	if listResult.Count != 1 {
		t.Fatalf("expected 1 active template, got %d", listResult.Count)
	}
	if listResult.Mode != "active" {
		t.Errorf("expected mode 'active', got %q", listResult.Mode)
	}
	if listResult.Active[0].Name != "always-french" {
		t.Errorf("expected active template 'always-french', got %q", listResult.Active[0].Name)
	}
}

func TestTemplateListTool_ActiveMissingConversationID(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateListTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"active": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listResult := result.(TemplateListResult)
	if listResult.Error != "conversation_id is required when active=true" {
		t.Errorf("expected error about missing conversation_id, got %q", listResult.Error)
	}
}

func TestTemplateListTool_ActiveNoTemplates(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateListTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"active":          true,
		"conversation_id": "conv-no-templates",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listResult := result.(TemplateListResult)
	if listResult.Count != 0 {
		t.Errorf("expected 0 active templates, got %d", listResult.Count)
	}
}
