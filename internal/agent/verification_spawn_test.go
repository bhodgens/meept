package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSpawner implements VerifierSpawner for testing.
type mockSpawner struct {
	output     string
	err        error
	called     int
	lastPrompt string
	lastModel  string
}

func (m *mockSpawner) SpawnVerifier(_ context.Context, prompt string, modelRef string) (string, error) {
	m.called++
	m.lastPrompt = prompt
	m.lastModel = modelRef
	return m.output, m.err
}

func TestAutoTriggerWithSpawnerPass(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "main.go")

	spawner := &mockSpawner{output: "### Check: build\n**Result: PASS**\n\nVERDICT: PASS"}
	hook := NewVerificationAutoTrigger(tr, VerificationConfig{Enabled: true, AutoTrigger: true, MaxFixLoops: 3})
	hook.SetSpawner(spawner)

	mod := hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "test-model"})
	assert.False(t, mod.Modified, "PASS verdict should not modify the turn")
	assert.Equal(t, 1, spawner.called)
	assert.Equal(t, "test-model", spawner.lastModel)
}

func TestAutoTriggerWithSpawnerFail(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "main.go")

	spawner := &mockSpawner{output: "### Check: build\n**Command run:**\n  go build\n**Output observed:**\n  error\n**Result: FAIL**\n\nVERDICT: FAIL"}
	hook := NewVerificationAutoTrigger(tr, VerificationConfig{Enabled: true, AutoTrigger: true, MaxFixLoops: 3})
	hook.SetSpawner(spawner)

	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	require.True(t, mod.Modified)
	assert.Contains(t, mod.Reason, "fix loop")
	require.Len(t, mod.ExtraMessages, 1)
	assert.Contains(t, mod.ExtraMessages[0].Content, "FAILED")
	assert.Contains(t, mod.ExtraMessages[0].Content, "iteration 1/3")
}

func TestFixLoopEscalation(t *testing.T) {
	spawner := &mockSpawner{output: "VERDICT: FAIL"}
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{Enabled: true, AutoTrigger: true, MaxFixLoops: 2})
	hook.SetSpawner(spawner)

	// First 2 FAILs produce fix instructions (iteration 1/2, 2/2).
	for i := 0; i < 2; i++ {
		tr := NewVerificationTracker(1)
		tr.RecordToolCall("file_write", "f.go")
		hook.tracker = tr
		mod := hook.PrepareNextTurn(context.Background(), TurnState{})
		require.True(t, mod.Modified)
		assert.Contains(t, mod.Reason, "fix loop", "iteration %d should be a fix loop", i+1)
	}

	// 3rd FAIL exceeds MaxFixLoops=2 → escalation, counter resets.
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "f.go")
	hook.tracker = tr
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	require.True(t, mod.Modified)
	assert.Equal(t, "verification escalation", mod.Reason)
	assert.Contains(t, mod.ExtraMessages[0].Content, "Manual review needed")
}

func TestAutoTriggerSpawnError(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "main.go")

	spawner := &mockSpawner{err: fmt.Errorf("connection refused")}
	hook := NewVerificationAutoTrigger(tr, VerificationConfig{Enabled: true, AutoTrigger: true})
	hook.SetSpawner(spawner)

	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	require.True(t, mod.Modified)
	assert.Contains(t, mod.Reason, "nudge fallback")
	assert.Contains(t, mod.ExtraMessages[0].Content, "verify your work")
}

func TestAutoTriggerModelOverride(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "main.go")

	spawner := &mockSpawner{output: "VERDICT: PASS"}
	hook := NewVerificationAutoTrigger(tr, VerificationConfig{
		Enabled: true, AutoTrigger: true, Model: "verifier-model",
	})
	hook.SetSpawner(spawner)

	hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "agent-model"})
	assert.Equal(t, "verifier-model", spawner.lastModel, "should use config model override")
}

func TestExtractFilePathFromToolCall(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"path key", `{"path": "/tmp/foo.go"}`, "/tmp/foo.go"},
		{"file_path key", `{"file_path": "/tmp/bar.go"}`, "/tmp/bar.go"},
		{"file key", `{"file": "/tmp/baz.go"}`, "/tmp/baz.go"},
		{"target key", `{"target": "/tmp/qux.go"}`, "/tmp/qux.go"},
		{"no path", `{"command": "ls"}`, ""},
		{"empty args", "", ""},
		{"invalid json", "not json", ""},
		{"priority path over file", `{"path": "/a", "file": "/b"}`, "/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := llm.ToolCall{Function: llm.ToolCallFunction{Arguments: tt.args}}
			assert.Equal(t, tt.want, extractFilePathFromToolCall(tc))
		})
	}
}

// -----------------------------------------------------------------------
// filteredToolRegistry tests
// -----------------------------------------------------------------------

// stubTool implements tools.Tool for testing.
type stubTool struct {
	tools.ToolDefaults
	name string
}

func (t *stubTool) Name() string                                             { return t.name }
func (t *stubTool) Description() string                                      { return t.name + " tool" }
func (t *stubTool) Parameters() llm.FunctionParameters                       { return llm.FunctionParameters{} }
func (t *stubTool) Execute(_ context.Context, _ map[string]any) (any, error) { return nil, nil }

// stubRegistry implements ToolRegistry for testing.
type stubRegistry struct {
	tools map[string]tools.Tool
}

func (r *stubRegistry) Get(name string) tools.Tool { return r.tools[name] }
func (r *stubRegistry) List() []tools.Tool {
	var result []tools.Tool
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}
func (r *stubRegistry) GetDefinitions() []llm.ToolDefinition {
	var result []llm.ToolDefinition
	for name := range r.tools {
		result = append(result, llm.ToolDefinition{Function: llm.FunctionDef{Name: name}})
	}
	return result
}

func TestFilteredToolRegistry(t *testing.T) {
	parent := &stubRegistry{tools: map[string]tools.Tool{
		"file_read":     &stubTool{name: "file_read"},
		"file_write":    &stubTool{name: "file_write"},
		"shell_execute": &stubTool{name: "shell_execute"},
		"memory_store":  &stubTool{name: "memory_store"},
	}}
	filtered := &filteredToolRegistry{parent: parent, allowed: verifierAllowedTools}

	assert.NotNil(t, filtered.Get("file_read"), "file_read should be allowed")
	assert.NotNil(t, filtered.Get("shell_execute"), "shell_execute should be allowed")
	assert.Nil(t, filtered.Get("file_write"), "file_write should be blocked")
	assert.Nil(t, filtered.Get("memory_store"), "memory_store should be blocked")

	list := filtered.List()
	assert.Len(t, list, 2, "only file_read and shell_execute should be listed")

	defs := filtered.GetDefinitions()
	for _, d := range defs {
		assert.True(t, verifierAllowedTools[d.Function.Name], "definition for non-allowed tool: %s", d.Function.Name)
	}
}
