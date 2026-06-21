package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

func TestMemoryReflectTool_NilManager(t *testing.T) {
	tool := NewMemoryReflectTool(nil, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestMemoryReflectTool_NilLLM(t *testing.T) {
	tool := NewMemoryReflectTool(&memory.Manager{}, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("expected error for nil LLM client")
	}
}

func TestMemoryReflectTool_MissingPrompt(t *testing.T) {
	tool := NewMemoryReflectTool(&memory.Manager{}, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing prompt")
	}
}

func TestMemoryReflectTool_Metadata(t *testing.T) {
	tool := NewMemoryReflectTool(nil, nil)
	if tool.Name() != "memory_reflect" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
}

// TestMemoryReflectTool_SetGraph_NilSafe verifies the nil-safe setter
// pattern per CLAUDE.md.
func TestMemoryReflectTool_SetGraph_NilSafe(t *testing.T) {
	tool := &MemoryReflectTool{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetGraph(nil) panicked: %v", r)
		}
	}()
	tool.SetGraph(nil)
}
