package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestFormatToolResult_AgentsMap verifies that structured results with an
// "agents" key are formatted as human-readable text, not raw JSON.
func TestFormatToolResult_AgentsMap(t *testing.T) {
	result := map[string]any{
		"agents": []any{
			map[string]any{"name": "coder", "description": "writes code"},
			map[string]any{"name": "reviewer", "description": "reviews code"},
		},
	}
	got := formatToolResult(result)
	assert.Contains(t, got, "available agents:")
	assert.Contains(t, got, "coder")
	assert.Contains(t, got, "reviewer")
	assert.NotContains(t, got, `\"name\"`)
}

// TestFormatToolResult_ToolsMap verifies the tools-key formatting path.
func TestFormatToolResult_ToolsMap(t *testing.T) {
	result := map[string]any{
		"tools": []any{
			map[string]any{"name": "bash", "description": "shell"},
		},
	}
	got := formatToolResult(result)
	assert.Contains(t, got, "available tools:")
	assert.Contains(t, got, "bash")
}

// TestFormatToolResult_GenericMap falls back to indented JSON.
func TestFormatToolResult_GenericMap(t *testing.T) {
	result := map[string]any{"answer": 42}
	got := formatToolResult(result)
	assert.Contains(t, got, "answer")
	assert.Contains(t, got, "42")
}

// TestFormatToolResult_NonMap falls back to indented JSON.
func TestFormatToolResult_NonMap(t *testing.T) {
	got := formatToolResult([]string{"a", "b"})
	assert.Contains(t, got, "a")
	assert.Contains(t, got, "b")
}

// TestBuildTerminateResponse_FormatsAgents verifies the full path from
// buildTerminateResponse through formatToolResult for platform_agents results.
func TestBuildTerminateResponse_FormatsAgents(t *testing.T) {
	loop := NewAgentLoop("test", "/tmp")
	results := []*ExecutionResult{
		{ToolCallID: "c1", Success: true, Result: map[string]any{
			"agents": []any{
				map[string]any{"name": "builder"},
			},
		}},
	}
	got := loop.buildTerminateResponse(results)
	assert.Contains(t, got, "available agents:")
	assert.Contains(t, got, "builder")
}

// TestBuildTerminateResponse_StringPassthrough verifies string results
// are still passed through unchanged.
func TestBuildTerminateResponse_StringPassthrough(t *testing.T) {
	loop := NewAgentLoop("test", "/tmp")
	results := []*ExecutionResult{
		{ToolCallID: "c1", Success: true, Result: "formatted markdown **bold**"},
	}
	got := loop.buildTerminateResponse(results)
	assert.Equal(t, "formatted markdown **bold**", got)
}

// TestGitProbeCache_GetSet verifies the cache returns cached data within TTL.
func TestGitProbeCache_GetSet(t *testing.T) {
	// use a fresh cache to avoid interference with the package-level one
	c := &gitProbeCache{
		entries: make(map[string]*gitProbeEntry),
		ttl:     5 * time.Second,
	}

	// miss
	_, _, _, ok := c.get("/nonexistent")
	assert.False(t, ok)

	// set + hit
	c.set("/test", "main", true, "go")
	branch, dirty, lang, ok := c.get("/test")
	assert.True(t, ok)
	assert.Equal(t, "main", branch)
	assert.True(t, dirty)
	assert.Equal(t, "go", lang)
}

// TestGitProbeCache_DifferentDirs verifies cache key is per-directory.
func TestGitProbeCache_DifferentDirs(t *testing.T) {
	c := &gitProbeCache{
		entries: make(map[string]*gitProbeEntry),
		ttl:     5 * time.Second,
	}
	c.set("/dir1", "main", false, "go")
	c.set("/dir2", "dev", true, "rust")

	b, _, _, ok := c.get("/dir1")
	assert.True(t, ok)
	assert.Equal(t, "main", b)

	b, _, _, ok = c.get("/dir2")
	assert.True(t, ok)
	assert.Equal(t, "dev", b)
}

// TestAgentLoop_Close verifies Close is safe on a fresh loop, idempotent,
// and sets the closed flag.
func TestAgentLoop_Close(t *testing.T) {
	loop := NewAgentLoop("test-close", "/tmp")
	assert.NotNil(t, loop)

	// Close should not panic
	loop.Close()
	assert.True(t, loop.closed.Load(), "expected closed flag after Close")

	// Double-close is safe
	loop.Close()

	// Nil-safe
	var nilLoop *AgentLoop
	nilLoop.Close()
}
