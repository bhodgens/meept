package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/templates"
)

func TestRegisterTemplateHandlers_List(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "summarize",
		Description: "summarize text",
		Scope:       templates.ScopeTurn,
		Body:        "Summarize: $@",
		Priority:    1,
	})
	reg.Register(&templates.Template{
		Name:        "translate",
		Description: "translate text",
		Scope:       templates.ScopeTurn,
		Body:        "Translate to $1: ${@:2}",
		Priority:    1,
	})

	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler, ok := srv.handlers["templates.list"]
	if !ok {
		t.Fatal("templates.list handler not registered")
	}

	result, err := handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("templates.list error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result is not a map")
	}

	templatesList, ok := resultMap["templates"].([]map[string]any)
	if !ok {
		t.Fatal("templates field is not a slice of maps")
	}
	if len(templatesList) != 2 {
		t.Fatalf("got %d templates, want 2", len(templatesList))
	}
	if resultMap["count"].(int) != 2 {
		t.Errorf("count = %v, want 2", resultMap["count"])
	}
}

func TestRegisterTemplateHandlers_Get(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "explain",
		Description: "explain code",
		Scope:       templates.ScopeTurn,
		Body:        "Explain: $@",
		Path:        "/path/explain.md",
		Priority:    0,
	})

	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.get"]

	result, err := handler(context.Background(), json.RawMessage(`{"name":"explain"}`))
	if err != nil {
		t.Fatalf("templates.get error = %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["name"] != "explain" {
		t.Errorf("name = %v, want explain", resultMap["name"])
	}
	if resultMap["body"] != "Explain: $@" {
		t.Errorf("body = %v, want 'Explain: $@'", resultMap["body"])
	}
	if resultMap["scope"] != "turn" {
		t.Errorf("scope = %v, want turn", resultMap["scope"])
	}
}

func TestRegisterTemplateHandlers_GetNotFound(t *testing.T) {
	reg := templates.NewRegistry()
	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.get"]

	_, err := handler(context.Background(), json.RawMessage(`{"name":"nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

func TestRegisterTemplateHandlers_Invoke(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:        "greet",
		Description: "greet someone",
		Scope:       templates.ScopeTurn,
		Body:        "Hello $1!",
	})

	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.invoke"]

	result, err := handler(context.Background(), json.RawMessage(`{"name":"greet","args":["World"]}`))
	if err != nil {
		t.Fatalf("templates.invoke error = %v", err)
	}

	resultMap := result.(map[string]any)
	if resultMap["prompt"] != "Hello World!" {
		t.Errorf("prompt = %v, want 'Hello World!'", resultMap["prompt"])
	}
	if resultMap["success"] != true {
		t.Errorf("success = %v, want true", resultMap["success"])
	}
}

func TestRegisterTemplateHandlers_InvokeNotFound(t *testing.T) {
	reg := templates.NewRegistry()
	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.invoke"]

	_, err := handler(context.Background(), json.RawMessage(`{"name":"missing"}`))
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestRegisterTemplateHandlers_Clear(t *testing.T) {
	reg := templates.NewRegistry()
	reg.Register(&templates.Template{
		Name:  "role-dev",
		Scope: templates.ScopeSession,
		Body:  "You are a developer.",
	})

	if err := reg.ActivateSessionTemplate("conv-123", "role-dev", nil); err != nil {
		t.Fatalf("ActivateSessionTemplate() error = %v", err)
	}

	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.clear"]

	result, err := handler(context.Background(), json.RawMessage(`{"conversation_id":"conv-123"}`))
	if err != nil {
		t.Fatalf("templates.clear error = %v", err)
	}

	resultMap := result.(map[string]any)
	cleared, ok := resultMap["cleared"].([]string)
	if !ok || len(cleared) != 1 {
		t.Fatalf("cleared = %v, want [role-dev]", resultMap["cleared"])
	}
	if cleared[0] != "role-dev" {
		t.Errorf("cleared[0] = %q, want %q", cleared[0], "role-dev")
	}
}

func TestRegisterTemplateHandlers_ClearMissingID(t *testing.T) {
	reg := templates.NewRegistry()
	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.clear"]

	_, err := handler(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing conversation_id")
	}
}

func TestRegisterTemplateHandlers_NilRegistry(t *testing.T) {
	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, nil, nil)

	handler := srv.handlers["templates.list"]

	_, err := handler(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestRegisterTemplateHandlers_InvalidParams(t *testing.T) {
	reg := templates.NewRegistry()
	srv := New(&Config{SocketPath: t.TempDir() + "/test.sock"}, nil, nil)
	RegisterTemplateHandlers(srv, reg, nil)

	handler := srv.handlers["templates.get"]

	_, err := handler(context.Background(), json.RawMessage(`invalid json`))
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}
