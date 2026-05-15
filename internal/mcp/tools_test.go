package mcp

import (
	"encoding/json"
	"testing"
)

func TestToolDefinitions(t *testing.T) {
	tools := ToolDefinitions()
	if len(tools) == 0 {
		t.Fatal("expected at least one tool definition")
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		names[tool.Name] = true
	}

	expected := []string{"meept_sessions", "meept_send", "meept_events", "meept_status", "meept_session_history"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestToolDefinitionsJSON(t *testing.T) {
	tools := ToolDefinitions()
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}
}
