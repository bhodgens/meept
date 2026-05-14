package templates

import (
	"errors"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	tmpl := &Template{
		Name:        "summarize",
		Description: "summarize text concisely",
		Scope:       ScopeTurn,
		Body:        "Summarize: $@",
	}

	reg.Register(tmpl)

	got := reg.Get("summarize")
	if got == nil {
		t.Fatal("Get(summarize) returned nil")
	}
	if got.Name != "summarize" {
		t.Errorf("Name = %q, want summarize", got.Name)
	}
}

func TestRegistry_GetCaseInsensitive(t *testing.T) {
	reg := NewRegistry()

	tmpl := &Template{
		Name:        "Summarize",
		Description: "summarize text",
		Body:        "Summarize: $@",
	}
	reg.Register(tmpl)

	tests := []string{"summarize", "SUMMARIZE", "Summarize", "sUmMaRiZe"}
	for _, name := range tests {
		got := reg.Get(name)
		if got == nil {
			t.Errorf("Get(%q) returned nil, want non-nil", name)
		}
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	reg := NewRegistry()

	got := reg.Get("nonexistent")
	if got != nil {
		t.Error("Get(nonexistent) should return nil")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()

	names := []string{"beta", "alpha", "gamma"}
	for _, name := range names {
		reg.Register(&Template{
			Name: name,
			Body: "body",
		})
	}

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("List returned %d templates, want 3", len(list))
	}

	// Should be sorted by name.
	if list[0].Name != "alpha" || list[1].Name != "beta" || list[2].Name != "gamma" {
		t.Errorf("List not sorted: got %v", list)
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{Name: "beta", Body: "b"})
	reg.Register(&Template{Name: "alpha", Body: "a"})

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("Names returned %d, want 2", len(names))
	}

	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("Names not sorted: got %v", names)
	}
}

func TestRegistry_Count(t *testing.T) {
	reg := NewRegistry()

	if reg.Count() != 0 {
		t.Errorf("Count() = %d, want 0", reg.Count())
	}

	reg.Register(&Template{Name: "a", Body: "a"})
	reg.Register(&Template{Name: "b", Body: "b"})

	if reg.Count() != 2 {
		t.Errorf("Count() = %d, want 2", reg.Count())
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{Name: "test", Body: "body"})
	if !reg.Unregister("test") {
		t.Error("Unregister should return true for existing template")
	}

	if reg.Get("test") != nil {
		t.Error("Get should return nil after unregister")
	}
}

func TestRegistry_UnregisterNonExistent(t *testing.T) {
	reg := NewRegistry()

	if reg.Unregister("nonexistent") {
		t.Error("Unregister should return false for non-existent template")
	}
}

func TestRegistry_Clear(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{Name: "a", Body: "a"})
	reg.Register(&Template{Name: "b", Body: "b"})
	reg.Clear()

	if reg.Count() != 0 {
		t.Errorf("Count() after Clear = %d, want 0", reg.Count())
	}
}

func TestRegistry_Substitute(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{
		Name:        "translate",
		Description: "translate text",
		Body:        "Translate to $1: $2",
	})

	result, err := reg.Substitute("translate", []string{"fr", "hello"})
	if err != nil {
		t.Fatalf("Substitute failed: %v", err)
	}

	if result != "Translate to fr: hello" {
		t.Errorf("Substitute() = %q, want 'Translate to fr: hello'", result)
	}
}

func TestRegistry_SubstituteNotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.Substitute("nonexistent", []string{"arg"})
	if err == nil {
		t.Error("Substitute should return error for non-existent template")
	}
}

func TestRegistry_RegisterAll(t *testing.T) {
	reg := NewRegistry()

	templates := []*Template{
		{Name: "a", Body: "a"},
		{Name: "b", Body: "b"},
		{Name: "c", Body: "c"},
	}
	reg.RegisterAll(templates)

	if reg.Count() != 3 {
		t.Errorf("Count() = %d, want 3", reg.Count())
	}
}

func TestRegistry_ActivateSessionTemplate(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{
		Name:        "role-dev",
		Description: "developer persona",
		Scope:       ScopeSession,
		Body:        "You are a developer. Focus on $@.",
	})

	err := reg.ActivateSessionTemplate("conv-1", "role-dev", []string{"Go", "backend"})
	if err != nil {
		t.Fatalf("ActivateSessionTemplate failed: %v", err)
	}

	active := reg.GetActiveTemplates("conv-1")
	if len(active) != 1 {
		t.Fatalf("GetActiveTemplates returned %d, want 1", len(active))
	}

	if active[0].Name != "role-dev" {
		t.Errorf("Name = %q, want role-dev", active[0].Name)
	}

	wantBody := "You are a developer. Focus on Go backend."
	if active[0].SubstitutedBody != wantBody {
		t.Errorf("SubstitutedBody = %q, want %q", active[0].SubstitutedBody, wantBody)
	}
}

func TestRegistry_ActivateSessionTemplateNotFound(t *testing.T) {
	reg := NewRegistry()

	err := reg.ActivateSessionTemplate("conv-1", "nonexistent", nil)
	if err == nil {
		t.Error("ActivateSessionTemplate should return error for non-existent template")
	}
}

func TestRegistry_DeactivateSessionTemplate(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{
		Name:  "role-dev",
		Scope: ScopeSession,
		Body:  "You are a developer.",
	})

	if err := reg.ActivateSessionTemplate("conv-1", "role-dev", nil); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	removed := reg.DeactivateSessionTemplate("conv-1", "role-dev")
	if !removed {
		t.Error("DeactivateSessionTemplate should return true")
	}

	active := reg.GetActiveTemplates("conv-1")
	if active != nil {
		t.Errorf("GetActiveTemplates should return nil, got %v", active)
	}
}

func TestRegistry_ClearSessionTemplates(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{Name: "a", Scope: ScopeSession, Body: "a"})
	reg.Register(&Template{Name: "b", Scope: ScopeSession, Body: "b"})

	reg.ActivateSessionTemplate("conv-1", "a", nil)
	reg.ActivateSessionTemplate("conv-1", "b", nil)

	names := reg.ClearSessionTemplates("conv-1")
	if len(names) != 2 {
		t.Errorf("ClearSessionTemplates returned %d names, want 2", len(names))
	}

	if reg.GetActiveTemplates("conv-1") != nil {
		t.Error("GetActiveTemplates should return nil after clear")
	}
}

func TestRegistry_SessionTemplateContext(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{
		Name:  "role-dev",
		Scope: ScopeSession,
		Body:  "You are a Go developer.",
	})

	reg.ActivateSessionTemplate("conv-1", "role-dev", nil)

	ctx := reg.SessionTemplateContext("conv-1")
	if ctx == "" {
		t.Fatal("SessionTemplateContext should not be empty")
	}
}

func TestRegistry_SessionTemplateContextEmpty(t *testing.T) {
	reg := NewRegistry()

	ctx := reg.SessionTemplateContext("conv-1")
	if ctx != "" {
		t.Errorf("SessionTemplateContext should be empty for no templates, got %q", ctx)
	}
}

func TestRegistry_ReplaceExisting(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Template{
		Name:        "test",
		Description: "version 1",
		Body:        "v1",
	})
	reg.Register(&Template{
		Name:        "test",
		Description: "version 2",
		Body:        "v2",
	})

	got := reg.Get("test")
	if got.Description != "version 2" {
		t.Errorf("Description = %q, want version 2 (replaced)", got.Description)
	}

	if reg.Count() != 1 {
		t.Errorf("Count() = %d, want 1 (replaced not duplicated)", reg.Count())
	}
}

func TestErrTemplateNotFound(t *testing.T) {
	if !errors.Is(ErrTemplateNotFound, ErrTemplateNotFound) {
		t.Error("ErrTemplateNotFound should match itself")
	}
}
