package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func executorSpec() *AgentSpec {
	return &AgentSpec{ID: "coder", Role: RoleExecutor}
}

func toolDefs(names ...string) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, 0, len(names))
	for _, n := range names {
		out = append(out, llm.ToolDefinition{Type: "function", Function: llm.FunctionDef{Name: n, Description: "test"}})
	}
	return out
}

// TestToolExemplar_Gating pins the operator decision: exemplar only on
// executor role AND LFM-family models with tools present.
func TestToolExemplar_Gating(t *testing.T) {
	spec := executorSpec()
	tools := toolDefs("file_write", "file_read")

	if got := buildToolExemplarSection(spec, "LFM2.5-8B-A1B-Q4_K_M.gguf", "local-gguf", tools); got == "" {
		t.Error("executor + LFM + tools: exemplar must render")
	}
	if got := buildToolExemplarSection(spec, "qwen2.5-7b-instruct", "local-gguf", tools); got != "" {
		t.Error("non-LFM model must not get the exemplar")
	}
	chat := &AgentSpec{ID: "chat", Role: RoleReviewer}
	if got := buildToolExemplarSection(chat, "LFM2.5", "local-gguf", tools); got != "" {
		t.Error("conversational role must not get the exemplar")
	}
	if got := buildToolExemplarSection(spec, "LFM2.5", "local-gguf", nil); got != "" {
		t.Error("no tools: exemplar must not render")
	}
}

// TestToolExemplar_UsesRealToolNames pins the no-drift property: the example
// names tools from the actual request definition list.
func TestToolExemplar_UsesRealToolNames(t *testing.T) {
	got := buildToolExemplarSection(executorSpec(), "lfm2.5", "local-mlx", toolDefs("file_write", "file_read"))
	if !strings.Contains(got, "file_write") || !strings.Contains(got, "file_read") {
		t.Errorf("exemplar must name real tools, got: %s", got)
	}
	if strings.Contains(got, "<file-to-create>") == false && !strings.Contains(got, "placeholder") {
		t.Errorf("exemplar must carry the parrot guard (placeholder note), got: %s", got)
	}
}

// TestContainsFold basic cases.
func TestContainsFold(t *testing.T) {
	if !containsFold("LFM2.5-8B", "lfm") || !containsFold("local-gguf", "GGUF") {
		t.Error("containsFold case-insensitivity broken")
	}
	if containsFold("qwen", "lfm") {
		t.Error("false positive")
	}
}

// TestPrefillHint_PrefersWriteTools pins the hint selection.
func TestPrefillHint_PrefersWriteTools(t *testing.T) {
	reg := NewPlaceholderToolRegistry()
	reg.Register(NewMockTool("file_read", "read", func(ctx context.Context, _ map[string]any) (any, error) { return "", nil }))
	reg.Register(NewMockTool("file_write", "write", func(ctx context.Context, _ map[string]any) (any, error) { return "", nil }))
	l := &AgentLoop{registry: reg}
	got := l.prefillToolCallHint()
	if !strings.Contains(got, "file_write") {
		t.Errorf("hint must prefer file_write, got %q", got)
	}
	// Fallback: first tool when no write-capable tool exists.
	reg2 := NewPlaceholderToolRegistry()
	reg2.Register(NewMockTool("memory_search", "search", func(ctx context.Context, _ map[string]any) (any, error) { return "", nil }))
	l2 := &AgentLoop{registry: reg2}
	if got2 := l2.prefillToolCallHint(); !strings.Contains(got2, "memory_search") {
		t.Errorf("hint fallback must use first tool, got %q", got2)
	}
	// Nil registry: empty hint, never panic.
	l3 := &AgentLoop{}
	if got3 := l3.prefillToolCallHint(); got3 != "" {
		t.Errorf("nil registry must yield empty hint, got %q", got3)
	}
}
