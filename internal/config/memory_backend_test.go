package config

import "testing"

// TestMemoryBackendValues pins the memory.backend vocabulary: "local" is the
// default (built-in SQLite store), "sqlite" is a legacy alias of "local",
// and "memvid" is the legacy name kept for existing configs (uses the memvid
// service when [memvid] enabled, falling back to the local store otherwise).
func TestMemoryBackendValues(t *testing.T) {
	tests := []struct {
		name  string
		value MemoryBackend
		want  string
	}{
		{"local", MemoryBackendLocal, "local"},
		{"sqlite legacy alias", MemoryBackendSQLite, "sqlite"},
		{"memvid legacy", MemoryBackendMemvid, "memvid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("backend value = %q, want %q", tt.value, tt.want)
			}
		})
	}
}

// TestDefaultConfigMemoryBackendLocal keeps the shipped default on the
// built-in local store rather than the legacy memvid name.
func TestDefaultConfigMemoryBackendLocal(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Memory.Backend != MemoryBackendLocal {
		t.Errorf("DefaultConfig memory.backend = %q, want %q",
			cfg.Memory.Backend, MemoryBackendLocal)
	}
}

// TestDefaultConfigMCPConfigFileJSON5 pins the MCP catalog default to the
// path the file actually installs as (mcp_servers.json5, not .json).
func TestDefaultConfigMCPConfigFileJSON5(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MCP.ConfigFile != "~/.meept/mcp_servers.json5" {
		t.Errorf("DefaultConfig mcp.config_file = %q, want %q",
			cfg.MCP.ConfigFile, "~/.meept/mcp_servers.json5")
	}
}
