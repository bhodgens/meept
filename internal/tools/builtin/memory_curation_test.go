package builtin

import (
	"context"
	"testing"
)

func TestRetainTool_NilManager(t *testing.T) {
	tool := NewRetainTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"content": "test fact",
	})
	if err == nil {
		t.Error("expected error when manager is nil")
	}
}

func TestRetainTool_MissingContent(t *testing.T) {
	tool := NewRetainTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestRetainTool_Name(t *testing.T) {
	tool := NewRetainTool(nil)
	if tool.Name() != "retain" {
		t.Errorf("expected name 'retain', got %q", tool.Name())
	}
}

func TestRetainTool_Category(t *testing.T) {
	tool := NewRetainTool(nil)
	if tool.Category() != "memory" {
		t.Errorf("expected category 'memory', got %q", tool.Category())
	}
}

func TestRetainTool_TerminateHint(t *testing.T) {
	tool := NewRetainTool(nil)
	if !tool.TerminateHint(nil) {
		t.Error("expected TerminateHint to return true")
	}
}

func TestRecallTool_NilManager(t *testing.T) {
	tool := NewRecallTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
	})
	if err == nil {
		t.Error("expected error when manager is nil")
	}
}

func TestRecallTool_MissingQuery(t *testing.T) {
	tool := NewRecallTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing query")
	}
}

func TestRecallTool_Name(t *testing.T) {
	tool := NewRecallTool(nil)
	if tool.Name() != "recall" {
		t.Errorf("expected name 'recall', got %q", tool.Name())
	}
}

func TestReflectTool_NilManager(t *testing.T) {
	tool := NewReflectTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error when manager is nil")
	}
}

func TestReflectTool_Name(t *testing.T) {
	tool := NewReflectTool(nil)
	if tool.Name() != "reflect" {
		t.Errorf("expected name 'reflect', got %q", tool.Name())
	}
}

func TestReflectTool_TerminateHint(t *testing.T) {
	tool := NewReflectTool(nil)
	if !tool.TerminateHint(nil) {
		t.Error("expected TerminateHint to return true")
	}
}
