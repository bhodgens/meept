package services

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/templates"
)

func TestTemplatesService_List(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "summarize",
		Description: "summarize text concisely",
		Scope:       templates.ScopeTurn,
		Body:        "Summarize: $@",
	})
	reg.Register(&templates.Template{
		Name:        "translate",
		Description: "translate text",
		Scope:       templates.ScopeTurn,
		Body:        "Translate to $1: ${@:2}",
	})

	svc := NewTemplatesService(reg, nil)

	result, err := svc.List(context.Background(), TemplatesListRequest{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("List() got %d templates, want 2", len(result))
	}
	if result[0].Name != "summarize" {
		t.Errorf("List()[0].Name = %q, want %q", result[0].Name, "summarize")
	}
	if result[1].Name != "translate" {
		t.Errorf("List()[1].Name = %q, want %q", result[1].Name, "translate")
	}
}

func TestTemplatesService_ListWithLimit(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{Name: "a", Body: "body-a"})
	reg.Register(&templates.Template{Name: "b", Body: "body-b"})
	reg.Register(&templates.Template{Name: "c", Body: "body-c"})

	svc := NewTemplatesService(reg, nil)

	result, err := svc.List(context.Background(), TemplatesListRequest{Limit: 2})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("List() got %d templates, want 2", len(result))
	}
}

func TestTemplatesService_ListNilRegistry(t *testing.T) {
	svc := NewTemplatesService(nil, nil)
	_, err := svc.List(context.Background(), TemplatesListRequest{})
	if err == nil {
		t.Fatal("List() with nil registry should return error")
	}
}

func TestTemplatesService_Get(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "summarize",
		Description: "summarize text concisely",
		Scope:       templates.ScopeTurn,
		Body:        "Summarize: $@",
		Path:        "/some/path/summarize.md",
		Priority:    1,
	})

	svc := NewTemplatesService(reg, nil)

	result, err := svc.Get(context.Background(), TemplatesGetRequest{Name: "summarize"})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.Name != "summarize" {
		t.Errorf("Get().Name = %q, want %q", result.Name, "summarize")
	}
	if result.Body != "Summarize: $@" {
		t.Errorf("Get().Body = %q, want template body", result.Body)
	}
	if result.Scope != templates.ScopeTurn {
		t.Errorf("Get().Scope = %q, want %q", result.Scope, templates.ScopeTurn)
	}
}

func TestTemplatesService_GetNotFound(t *testing.T) {
	reg := templates.NewRegistry()
	svc := NewTemplatesService(reg, nil)

	_, err := svc.Get(context.Background(), TemplatesGetRequest{Name: "nonexistent"})
	if err == nil {
		t.Fatal("Get() with nonexistent template should return error")
	}
}

func TestTemplatesService_GetEmptyName(t *testing.T) {
	reg := templates.NewRegistry()
	svc := NewTemplatesService(reg, nil)

	_, err := svc.Get(context.Background(), TemplatesGetRequest{Name: ""})
	if err == nil {
		t.Fatal("Get() with empty name should return error")
	}
}

func TestTemplatesService_Invoke(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "translate",
		Description: "translate text",
		Body:        "Translate to $1: ${@:2}",
	})

	svc := NewTemplatesService(reg, nil)

	result, err := svc.Invoke(context.Background(), TemplatesInvokeRequest{
		Name: "translate",
		Args: []string{"Spanish", "Hello", "world"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Prompt != "Translate to Spanish: Hello world" {
		t.Errorf("Invoke().Prompt = %q, want substituted result", result.Prompt)
	}
	if !result.Success {
		t.Error("Invoke().Success should be true")
	}
	// Without executor, Output should be empty
	if result.Output != "" {
		t.Errorf("Invoke().Output = %q, want empty (no executor)", result.Output)
	}
}

func TestTemplatesService_InvokeNotFound(t *testing.T) {
	reg := templates.NewRegistry()
	svc := NewTemplatesService(reg, nil)

	_, err := svc.Invoke(context.Background(), TemplatesInvokeRequest{
		Name: "nonexistent",
	})
	if err == nil {
		t.Fatal("Invoke() with nonexistent template should return error")
	}
}

func TestTemplatesService_InvokeEmptyName(t *testing.T) {
	reg := templates.NewRegistry()
	svc := NewTemplatesService(reg, nil)

	_, err := svc.Invoke(context.Background(), TemplatesInvokeRequest{
		Name: "",
	})
	if err == nil {
		t.Fatal("Invoke() with empty name should return error")
	}
}

func TestTemplatesService_ClearSession(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "role-dev",
		Description: "dev role",
		Scope:       templates.ScopeSession,
		Body:        "You are a developer.",
	})

	// Activate first
	if err := reg.ActivateSessionTemplate("conv-1", "role-dev", nil); err != nil {
		t.Fatalf("ActivateSessionTemplate() error = %v", err)
	}

	svc := NewTemplatesService(reg, nil)

	result, err := svc.ClearSession(context.Background(), TemplatesClearRequest{
		ConversationID: "conv-1",
	})
	if err != nil {
		t.Fatalf("ClearSession() error = %v", err)
	}
	if len(result.Cleared) != 1 {
		t.Fatalf("ClearSession().Cleared = %v, want 1 entry", result.Cleared)
	}
	if result.Cleared[0] != "role-dev" {
		t.Errorf("ClearSession().Cleared[0] = %q, want %q", result.Cleared[0], "role-dev")
	}
}

func TestTemplatesService_ClearSessionByName(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "a",
		Scope: templates.ScopeSession,
		Body:  "Template A",
	})
	reg.Register(&templates.Template{
		Name:  "b",
		Scope: templates.ScopeSession,
		Body:  "Template B",
	})

	if err := reg.ActivateSessionTemplate("conv-1", "a", nil); err != nil {
		t.Fatalf("ActivateSessionTemplate() error = %v", err)
	}
	if err := reg.ActivateSessionTemplate("conv-1", "b", nil); err != nil {
		t.Fatalf("ActivateSessionTemplate() error = %v", err)
	}

	svc := NewTemplatesService(reg, nil)

	result, err := svc.ClearSession(context.Background(), TemplatesClearRequest{
		ConversationID: "conv-1",
		Name:           "a",
	})
	if err != nil {
		t.Fatalf("ClearSession() error = %v", err)
	}
	if len(result.Cleared) != 1 || result.Cleared[0] != "a" {
		t.Fatalf("ClearSession().Cleared = %v, want [a]", result.Cleared)
	}

	// Verify "b" is still active
	active := reg.GetActiveTemplates("conv-1")
	if len(active) != 1 || active[0].Name != "b" {
		t.Fatalf("After clearing a, active = %v, want [b]", active)
	}
}

func TestTemplatesService_ClearSessionEmptyID(t *testing.T) {
	reg := templates.NewRegistry()
	svc := NewTemplatesService(reg, nil)

	_, err := svc.ClearSession(context.Background(), TemplatesClearRequest{
		ConversationID: "",
	})
	if err == nil {
		t.Fatal("ClearSession() with empty conversation_id should return error")
	}
}

func TestTemplatesService_ClearSessionNilRegistry(t *testing.T) {
	svc := NewTemplatesService(nil, nil)
	_, err := svc.ClearSession(context.Background(), TemplatesClearRequest{
		ConversationID: "conv-1",
	})
	if err == nil {
		t.Fatal("ClearSession() with nil registry should return error")
	}
}
