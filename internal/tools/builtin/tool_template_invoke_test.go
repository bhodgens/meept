package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/templates"
)

func TestTemplateInvokeTool_NameAndDescription(t *testing.T) {
	tool := NewTemplateInvokeTool(nil)

	if tool.Name() != "template_invoke" {
		t.Errorf("expected name 'template_invoke', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(desc, "template") {
		t.Error("description should mention template")
	}
}

func TestTemplateInvokeTool_Parameters(t *testing.T) {
	tool := NewTemplateInvokeTool(nil)
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("expected type 'object', got %q", params.Type)
	}

	if len(params.Properties) != 4 {
		t.Errorf("expected 4 parameters, got %d", len(params.Properties))
	}

	// Check required fields
	if len(params.Required) != 1 || params.Required[0] != "name" {
		t.Errorf("expected required=['name'], got %v", params.Required)
	}

	// Check name parameter
	nameParam, ok := params.Properties["name"]
	if !ok {
		t.Fatal("name parameter missing")
	}
	if nameParam.Type != "string" {
		t.Errorf("expected name type 'string', got %q", nameParam.Type)
	}
}

func TestTemplateInvokeTool_NilRegistry(t *testing.T) {
	tool := NewTemplateInvokeTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if invokeResult.Success {
		t.Error("expected failure with nil registry")
	}
	if invokeResult.Error != "template registry not available" {
		t.Errorf("unexpected error: %q", invokeResult.Error)
	}
}

func TestTemplateInvokeTool_MissingName(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if invokeResult.Success {
		t.Error("expected failure with missing name")
	}
	if invokeResult.Error != "name is required" {
		t.Errorf("unexpected error: %q", invokeResult.Error)
	}
}

func TestTemplateInvokeTool_TemplateNotFound(t *testing.T) {
	reg := templates.NewRegistry()
	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if invokeResult.Success {
		t.Error("expected failure for missing template")
	}
	if !strings.Contains(invokeResult.Error, "template not found") {
		t.Errorf("expected 'template not found' error, got %q", invokeResult.Error)
	}
}

func TestTemplateInvokeTool_BasicInvoke(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "summarize",
		Description: "Summarize text",
		Scope:       templates.ScopeTurn,
		Body:        "Summarize the following: $@",
	})

	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "summarize",
		"args": []any{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if !invokeResult.Success {
		t.Fatalf("expected success, got error: %q", invokeResult.Error)
	}
	if invokeResult.Body != "Summarize the following: hello world" {
		t.Errorf("unexpected body: %q", invokeResult.Body)
	}
	if invokeResult.Injected {
		t.Error("should not be injected in default mode")
	}
	if invokeResult.Scope != "turn" {
		t.Errorf("expected scope 'turn', got %q", invokeResult.Scope)
	}
}

func TestTemplateInvokeTool_InvokeWithPositionalArgs(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "translate",
		Scope: templates.ScopeTurn,
		Body:  "Translate the following to $1: ${@:2}",
	})

	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "translate",
		"args": []any{"french", "bonjour", "monde"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if !invokeResult.Success {
		t.Fatalf("expected success, got error: %q", invokeResult.Error)
	}
	// ${@:2} means args from index 2 onward (1-indexed), which is args[1:] = "bonjour monde"
	if invokeResult.Body != "Translate the following to french: bonjour monde" {
		t.Errorf("unexpected body: %q", invokeResult.Body)
	}
}

func TestTemplateInvokeTool_TurnInject(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "format-json",
		Scope: templates.ScopeTurn,
		Body:  "Pretty-print the following JSON: $@",
	})

	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":   "format-json",
		"args":   []any{"{}"},
		"inject": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if !invokeResult.Success {
		t.Fatalf("expected success, got error: %q", invokeResult.Error)
	}
	if !invokeResult.Injected {
		t.Error("expected injected=true")
	}
	if invokeResult.SessionActive {
		t.Error("turn-scoped template should not be session active")
	}
}

func TestTemplateInvokeTool_SessionInject(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "always-french",
		Scope: templates.ScopeSession,
		Body:  "Always respond in French.",
	})

	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":            "always-french",
		"inject":          true,
		"conversation_id": "conv-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if !invokeResult.Success {
		t.Fatalf("expected success, got error: %q", invokeResult.Error)
	}
	if !invokeResult.Injected {
		t.Error("expected injected=true")
	}
	if !invokeResult.SessionActive {
		t.Error("session-scoped template should be session active")
	}
	if invokeResult.Scope != "session" {
		t.Errorf("expected scope 'session', got %q", invokeResult.Scope)
	}
}

func TestTemplateInvokeTool_SessionInjectMissingConversationID(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "always-french",
		Scope: templates.ScopeSession,
		Body:  "Always respond in French.",
	})

	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":   "always-french",
		"inject": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if invokeResult.Success {
		t.Error("expected failure when conversation_id missing for session inject")
	}
	if !strings.Contains(invokeResult.Error, "conversation_id is required") {
		t.Errorf("unexpected error: %q", invokeResult.Error)
	}
}

func TestTemplateInvokeTool_BodySizeLimit(t *testing.T) {
	reg := templates.NewRegistry()

	// Create a template with a body that exceeds the limit
	longBody := strings.Repeat("x", MaxTemplateBodySize+1)
	reg.Register(&templates.Template{
		Name:  "big-template",
		Scope: templates.ScopeTurn,
		Body:  longBody,
	})

	tool := NewTemplateInvokeTool(reg)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "big-template",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if invokeResult.Success {
		t.Error("expected failure for oversized template")
	}
	if !strings.Contains(invokeResult.Error, "exceeds maximum size") {
		t.Errorf("unexpected error: %q", invokeResult.Error)
	}
}

func TestTemplateInvokeTool_ArgParsingFromString(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "echo",
		Scope: templates.ScopeTurn,
		Body:  "Echo: $@",
	})

	tool := NewTemplateInvokeTool(reg)

	// Test args passed as a string (should be split on whitespace)
	result, err := tool.Execute(context.Background(), map[string]any{
		"name": "echo",
		"args": "hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invokeResult := result.(TemplateInvokeResult)
	if !invokeResult.Success {
		t.Fatalf("expected success, got error: %q", invokeResult.Error)
	}
	if invokeResult.Body != "Echo: hello world" {
		t.Errorf("unexpected body: %q", invokeResult.Body)
	}
}
