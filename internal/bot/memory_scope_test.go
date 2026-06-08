package bot

import (
	"testing"
)

func TestMemoryNamespace_Prefix(t *testing.T) {
	ns := MemoryNamespace{BotID: "ci-monitor"}
	if got := ns.Prefix(); got != "bot:ci-monitor" {
		t.Errorf("Prefix() = %q, want %q", got, "bot:ci-monitor")
	}
}

func TestMemoryNamespace_ScopeQuery(t *testing.T) {
	ns := MemoryNamespace{BotID: "ci-monitor"}

	tests := []struct {
		name   string
		scope  MemoryScope
		input  string
		expect string
	}{
		{
			name:   "private scope adds bot prefix",
			scope:  MemoryScopePrivate,
			input:  "ci failures",
			expect: "bot:ci-monitor ci failures",
		},
		{
			name:   "shared scope passes through",
			scope:  MemoryScopeShared,
			input:  "ci failures",
			expect: "ci failures",
		},
		{
			name:   "read_only scope adds bot prefix",
			scope:  MemoryScopeReadOnly,
			input:  "ci failures",
			expect: "bot:ci-monitor ci failures",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ns.ScopeQuery(tt.scope, tt.input)
			if got != tt.expect {
				t.Errorf("ScopeQuery() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestMemoryNamespace_TagMemory(t *testing.T) {
	ns := MemoryNamespace{BotID: "ci-monitor"}

	meta := map[string]any{
		"category": "observation",
	}

	tagged := ns.TagMemory(meta)
	if tagged["bot_id"] != "ci-monitor" {
		t.Errorf("TagMemory bot_id = %v, want %q", tagged["bot_id"], "ci-monitor")
	}
	if tagged["category"] != "observation" {
		t.Error("TagMemory should preserve existing keys")
	}
}

func TestMemoryNamespace_TagMemory_NilMeta(t *testing.T) {
	ns := MemoryNamespace{BotID: "test-bot"}

	tagged := ns.TagMemory(nil)
	if tagged["bot_id"] != "test-bot" {
		t.Errorf("TagMemory should handle nil meta")
	}
}

func TestMemoryNamespace_FilterBotMemories(t *testing.T) {
	ns := MemoryNamespace{BotID: "ci-monitor"}

	results := []map[string]any{
		{"bot_id": "ci-monitor", "content": "build failed"},
		{"bot_id": "other-bot", "content": "unrelated"},
		{"content": "no bot id"},
		{"bot_id": "ci-monitor", "content": "tests passed"},
	}

	// Private scope filters to own bot only
	filtered := ns.FilterBotMemories(MemoryScopePrivate, results)
	if len(filtered) != 2 {
		t.Errorf("private: got %d results, want 2", len(filtered))
	}

	// Shared scope returns all
	shared := ns.FilterBotMemories(MemoryScopeShared, results)
	if len(shared) != 4 {
		t.Errorf("shared: got %d results, want 4", len(shared))
	}

	// ReadOnly scope filters like private
	ro := ns.FilterBotMemories(MemoryScopeReadOnly, results)
	if len(ro) != 2 {
		t.Errorf("read_only: got %d results, want 2", len(ro))
	}
}
