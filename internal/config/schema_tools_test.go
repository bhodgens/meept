package config

import (
	"strings"
	"testing"
)

// TestAgentToolsConfig_SchemaModeValidation covers [agent.tools] schema_mode
// validation: only "", "full", and "indexed" are accepted; anything else is
// rejected with an error naming agent.tools.schema_mode (loop-economics
// leaf 02: indexed is the default-on mode; "full" restores legacy).
func TestAgentToolsConfig_SchemaModeValidation(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"empty means indexed default", "", false},
		{"full restores legacy", "full", false},
		{"indexed explicit", "indexed", false},
		{"unknown rejected", "lazy", true},
		{"case-sensitive", "Full", true},
		{"whitespace not trimmed", " indexed", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := AgentToolsConfig{SchemaMode: tc.mode}
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error for mode %q", tc.mode)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil for mode %q", err, tc.mode)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "agent.tools.schema_mode") {
				t.Fatalf("Validate() error %q must name agent.tools.schema_mode", err)
			}
		})
	}
}

// TestConfigValidateAll_SchemaModeWired verifies the [agent.tools] validation
// is reachable from the top-level config validation path.
func TestConfigValidateAll_SchemaModeWired(t *testing.T) {
	cfg := &Config{}
	cfg.Agent.Tools.SchemaMode = "bogus"
	err := cfg.ValidateAll()
	if err == nil {
		t.Fatal("ValidateAll() = nil, want error for bogus agent.tools.schema_mode")
	}
	if !strings.Contains(err.Error(), "agent.tools.schema_mode") {
		t.Fatalf("ValidateAll() error %q must point at agent.tools.schema_mode", err)
	}
}

// TestDefaultAlwaysFullTools checks the curated core-tool list that stays
// full-schema under indexed mode. Order-stable copy each call.
func TestDefaultAlwaysFullTools(t *testing.T) {
	got := DefaultAlwaysFullTools()
	want := []string{
		"shell", "file_read", "file_edit", "file_write",
		"memory_search", "memory_store", "web_fetch", "websearch",
		"platform_status", "tool_view",
	}
	if len(got) != len(want) {
		t.Fatalf("DefaultAlwaysFullTools() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DefaultAlwaysFullTools()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Mutating the returned slice must not corrupt the next call's result.
	got[0] = "hijacked"
	again := DefaultAlwaysFullTools()
	if again[0] != "shell" {
		t.Fatalf("DefaultAlwaysFullTools() returned shared backing array: %v", again)
	}
}
