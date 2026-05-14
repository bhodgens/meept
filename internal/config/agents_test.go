package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentDefinitions(t *testing.T) {
	// Test with the actual config files
	agents, err := LoadAgentDefinitions([]string{"../../config/agents"})
	if err != nil {
		t.Fatalf("Failed to load agent definitions: %v", err)
	}

	// Check that we got some agents
	if len(agents) == 0 {
		t.Error("Expected at least one agent definition")
	}

	// Check for expected core agents
	expectedAgents := []string{"dispatcher", "chat", "coder", "debugger", "researcher", "analyst", "planner", "committer", "scheduler"}
	for _, id := range expectedAgents {
		if _, ok := agents[id]; !ok {
			t.Errorf("Expected agent %q not found", id)
		}
	}

	// Validate dispatcher
	if dispatcher, ok := agents["dispatcher"]; ok {
		if dispatcher.Role != "dispatcher" {
			t.Errorf("Expected dispatcher role to be 'dispatcher', got %q", dispatcher.Role)
		}
		if len(dispatcher.PromptComponents) == 0 {
			t.Error("Expected dispatcher to have prompt components")
		}
	}

	// Validate chat agent
	if chat, ok := agents["chat"]; ok {
		if chat.Role != "conversational" {
			t.Errorf("Expected chat role to be 'conversational', got %q", chat.Role)
		}
		if !chat.CanDelegate {
			t.Error("Expected chat agent to have can_delegate = true")
		}
	}

	// Validate coder agent
	if coder, ok := agents["coder"]; ok {
		if coder.Role != "executor" {
			t.Errorf("Expected coder role to be 'executor', got %q", coder.Role)
		}
		if len(coder.AdditionalTools) == 0 {
			t.Error("Expected coder to have additional tools")
		}
	}
}

func TestLoadAgentFile(t *testing.T) {
	// Create a temp directory with a test TOML file
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.toml")

	content := `
[[agent]]
id = "test-agent"
name = "Test Agent"
role = "executor"
description = "A test agent"
model = ""
enabled = true
additional_tools = ["tool1", "tool2"]
capabilities = ["code"]
prompt_components = ["base.constitution", "specialist.coder"]

[agent.constraints]
max_iterations = 10
timeout_seconds = 300
max_tokens_per_turn = 4096
max_memory_refs = 20
`

	//nolint:gosec // test directory/file
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	agents, err := loadAgentFile(testFile)
	if err != nil {
		t.Fatalf("Failed to load agent file: %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(agents))
	}

	agent := agents[0]
	if agent.ID != "test-agent" {
		t.Errorf("Expected ID 'test-agent', got %q", agent.ID)
	}
	if agent.Role != "executor" {
		t.Errorf("Expected role 'executor', got %q", agent.Role)
	}
	if len(agent.AdditionalTools) != 2 {
		t.Errorf("Expected 2 additional tools, got %d", len(agent.AdditionalTools))
	}
	if agent.Constraints.MaxIterations != 10 {
		t.Errorf("Expected max_iterations 10, got %d", agent.Constraints.MaxIterations)
	}
}

func TestValidateAgentDefinition(t *testing.T) {
	tests := []struct {
		name    string
		agent   *AgentDefinition
		wantErr bool
	}{
		{
			name: "valid agent",
			agent: &AgentDefinition{
				ID:   "test",
				Name: "Test",
				Role: "executor",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			agent: &AgentDefinition{
				Name: "Test",
				Role: "executor",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			agent: &AgentDefinition{
				ID:   "test",
				Role: "executor",
			},
			wantErr: true,
		},
		{
			name: "invalid role",
			agent: &AgentDefinition{
				ID:   "test",
				Name: "Test",
				Role: "invalid",
			},
			wantErr: true,
		},
		{
			name: "empty role is valid",
			agent: &AgentDefinition{
				ID:   "test",
				Name: "Test",
				Role: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAgentDefinition(tt.agent)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAgentDefinition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeAgentDefaults(t *testing.T) {
	agent := &AgentDefinition{
		ID:   "test",
		Name: "Test",
	}

	MergeAgentDefaults(agent)

	if agent.Constraints.MaxIterations != 25 {
		t.Errorf("Expected default max_iterations 25, got %d", agent.Constraints.MaxIterations)
	}
	if agent.Constraints.TimeoutSeconds != 300 {
		t.Errorf("Expected default timeout_seconds 300, got %d", agent.Constraints.TimeoutSeconds)
	}
	if agent.Constraints.MaxTokensPerTurn != 4096 {
		t.Errorf("Expected default max_tokens_per_turn 4096, got %d", agent.Constraints.MaxTokensPerTurn)
	}
	if agent.Constraints.MaxMemoryRefs != 20 {
		t.Errorf("Expected default max_memory_refs 20, got %d", agent.Constraints.MaxMemoryRefs)
	}
}

func TestFilterEnabledAgents(t *testing.T) {
	agents := map[string]*AgentDefinition{
		"enabled1":  {ID: "enabled1", Enabled: true},
		"enabled2":  {ID: "enabled2", Enabled: true},
		"disabled1": {ID: "disabled1", Enabled: false},
	}

	filtered := FilterEnabledAgents(agents)

	if len(filtered) != 2 {
		t.Errorf("Expected 2 enabled agents, got %d", len(filtered))
	}
	if _, ok := filtered["enabled1"]; !ok {
		t.Error("Expected enabled1 in filtered results")
	}
	if _, ok := filtered["disabled1"]; ok {
		t.Error("Did not expect disabled1 in filtered results")
	}
}

func TestGetAgentsByRole(t *testing.T) {
	agents := map[string]*AgentDefinition{
		"dispatcher": {ID: "dispatcher", Role: "dispatcher"},
		"coder":      {ID: "coder", Role: "executor"},
		"debugger":   {ID: "debugger", Role: "executor"},
	}

	executors := GetAgentsByRole(agents, "executor")
	if len(executors) != 2 {
		t.Errorf("Expected 2 executor agents, got %d", len(executors))
	}

	dispatchers := GetAgentsByRole(agents, "dispatcher")
	if len(dispatchers) != 1 {
		t.Errorf("Expected 1 dispatcher agent, got %d", len(dispatchers))
	}
}
