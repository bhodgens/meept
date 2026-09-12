package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestJSONExtract_TypedNilChatterNoPanic (2026-09-10 panic, outcome-loop
// session): the daemon registers json_extract even when extract_model
// resolves to nothing, passing (*llm.Client)(nil). The interface holds
// a typed nil, so the `chatter == nil` check passed and Chat panicked
// dereferencing the nil receiver's mutex. The reflect guard must return
// the actionable config error instead.
func TestJSONExtract_TypedNilChatterNoPanic(t *testing.T) {
	var typedNil *llm.Client // typed nil in Chatter interface
	tool := NewJSONExtractTool(typedNil, 0)
	if tool == nil {
		t.Fatal("tool must construct")
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"schema": map[string]any{"type": "object"},
		"text":   "some text",
	})
	if err == nil {
		t.Fatal("expected config error, got nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error should be the actionable config hint, got: %v", err)
	}
}

func TestJSONExtract_NilToolNoPanic(t *testing.T) {
	var tool *JSONExtractTool
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error from nil tool")
	}
}
