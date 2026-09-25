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

	expected := []string{"meept_sessions", "meept_send", "meept_chat_submit", "meept_wait_turn", "meept_events", "meept_subscribe", "meept_unsubscribe", "meept_status", "meept_session_history"}
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

func TestAsyncTurnToolSchemas(t *testing.T) {
	tools := ToolDefinitions()
	byName := make(map[string]ToolDefinition)
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"meept_chat_submit", "meept_wait_turn"} {
		td, ok := byName[name]
		if !ok {
			t.Fatalf("missing tool: %s", name)
		}
		if td.InputSchema["type"] != "object" {
			t.Errorf("%s: inputSchema.type = %v, want object", name, td.InputSchema["type"])
		}
		props, _ := td.InputSchema["properties"].(map[string]any)
		if len(props) == 0 {
			t.Errorf("%s: inputSchema has no properties", name)
		}
	}
	if _, ok := byName["meept_chat_submit"].InputSchema["properties"].(map[string]any)["session_id"]; !ok {
		t.Error("meept_chat_submit: missing session_id property")
	}
	for _, prop := range []string{"turn_id", "subscription_id"} {
		if _, ok := byName["meept_wait_turn"].InputSchema["properties"].(map[string]any)[prop]; !ok {
			t.Errorf("meept_wait_turn: missing %s property", prop)
		}
	}
}
