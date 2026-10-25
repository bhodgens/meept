package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestExecutorAgentJSONTag pins the wire name for the acp-employees phase-3
// executor_agent reference (D1): camel-case Go field, snake-case JSON key,
// omitted when empty so non-acp employees serialize byte-identically.
func TestExecutorAgentJSONTag(t *testing.T) {
	t.Parallel()

	withAgent := BotDefinition{ID: "e1", Prompt: "p", ExecutorAgent: "bob"}
	data, err := json.Marshal(withAgent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"executor_agent":"bob"`) {
		t.Errorf("marshal missing executor_agent key: %s", data)
	}

	bare := BotDefinition{ID: "e2", Prompt: "p"}
	data, err = json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "executor_agent") {
		t.Errorf("omitempty violated: %s", data)
	}
}

// TestStore_ExecutorAgentRoundTrip proves the field persists through the
// bot definition JSON blob with no schema migration: Create → Get and
// Update → Get both return the reference unchanged.
func TestStore_ExecutorAgentRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	def := testBotDef("acp-emp-1")
	def.ExecutorAgent = "bob"
	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(ctx, "acp-emp-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ExecutorAgent != "bob" {
		t.Errorf("ExecutorAgent after create = %q, want %q", got.ExecutorAgent, "bob")
	}

	// Update path: set, write, re-read.
	def.ExecutorAgent = "carol"
	if err := store.Update(ctx, def); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = store.Get(ctx, "acp-emp-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.ExecutorAgent != "carol" {
		t.Errorf("ExecutorAgent after update = %q, want %q", got.ExecutorAgent, "carol")
	}

	// Legacy blob compatibility: a definition written before the field
	// existed must unmarshal with ExecutorAgent empty, not error.
	legacy := `{"id":"old","name":"old","prompt":"p","triggers":null,"memory_scope":"","tools":null,"constraints":{"max_iterations":0,"timeout":0},"enabled":true,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	var d BotDefinition
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if d.ExecutorAgent != "" {
		t.Errorf("legacy def should load with empty ExecutorAgent, got %q", d.ExecutorAgent)
	}
}
