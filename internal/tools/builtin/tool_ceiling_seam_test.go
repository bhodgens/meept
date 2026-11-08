package builtin

import (
	"context"
	"testing"

	"encoding/json"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// decodeToolSearchPayload unwraps the tool_search result envelope
// (tools.ToolResult.Result carries the JSON payload).
func decodeToolSearchPayload(t *testing.T, res any) toolSearchPayload {
	t.Helper()
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	var p toolSearchPayload
	raw, _ := tr.Result.(string)
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("decode payload: %v (raw=%q)", err, raw)
	}
	return p
}

// bughunt config-tools F-1: the meta pair must obey the CALLING agent's
// role ceiling, not the daemon-wide full registry. The executor injects the
// agent's filtered registry per call (agent.ContextWithToolRegistry); both
// tool_search and tool_view read it ahead of their bound full-registry
// scope. These pins exercise that seam directly (unit level): the e2e
// dynamic-tools suite covers the live executor path.

func newCeilingFixture(t *testing.T) (full *tools.Registry, ceiling agent.ToolRegistry) {
	t.Helper()
	full = tools.NewRegistry(nil)
	for _, name := range []string{"shell", "web_fetch", "task_create", "admin_secret_tool"} {
		tool := &toolViewFixtureTool{name: name, desc: "the " + name + " tool",
			params: llm.FunctionParameters{Type: "object"}}
		full.Register(tool)
	}
	// A scoped agent's view: everything except the out-of-ceiling admin tool.
	ceiling = agent.NewFilteredToolRegistry(full,
		[]string{"shell", "web_fetch", "task_create"})
	return full, ceiling
}

// TestToolSearchHonorsContextCeiling: with a filtered registry on the ctx,
// searches must never surface an out-of-ceiling tool name, even though the
// tool instance itself is bound to the FULL registry (the daemon-wide
// registration shape).
func TestToolSearchHonorsContextCeiling(t *testing.T) {
	full, ceiling := newCeilingFixture(t)
	ts := NewToolSearchTool(full) // bound to the full registry (legacy shape)

	ctx := agent.ContextWithToolRegistry(context.Background(), ceiling)
	res, err := ts.Execute(ctx, map[string]any{"query": "tool", "limit": 20})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	payload := decodeToolSearchPayload(t, res)
	for _, m := range payload.Matches {
		if m.Name == "admin_secret_tool" {
			t.Errorf("out-of-ceiling tool %q leaked through a ceiling-scoped search", m.Name)
		}
	}
	// In-ceiling tools must STILL be found (the seam narrows, never starves).
	found := map[string]bool{}
	for _, m := range payload.Matches {
		found[m.Name] = true
	}
	for want := range map[string]bool{"shell": true, "web_fetch": true, "task_create": true} {
		if !found[want] {
			t.Errorf("in-ceiling tool %q missing from a ceiling-scoped search", want)
		}
	}

	// Legacy posture: no ctx value => the bound scope applies unchanged.
	res2, err := ts.Execute(context.Background(), map[string]any{"query": "admin", "limit": 5})
	if err != nil {
		t.Fatalf("legacy Execute: %v", err)
	}
	payload2 := decodeToolSearchPayload(t, res2)
	hit := false
	for _, m := range payload2.Matches {
		if m.Name == "admin_secret_tool" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("standalone caller (no ctx registry) must keep the bound full scope")
	}
}

// TestToolViewHonorsContextCeiling: an out-of-ceiling name is refused
// BEFORE the shared LRU cache is consulted (a cached schema from another
// agent must not leak), and an in-ceiling name expands normally.
func TestToolViewHonorsContextCeiling(t *testing.T) {
	full, ceiling := newCeilingFixture(t)
	tv := NewToolViewTool(full, 8)

	ctx := agent.ContextWithToolRegistry(context.Background(), ceiling)

	// Seed the daemon-wide LRU via a legacy (no-ctx) admin view, then prove
	// the ceiling-scoped call still refuses it from cache.
	if _, err := tv.Execute(context.Background(), map[string]any{"name": "admin_secret_tool"}); err != nil {
		t.Fatalf("legacy view: %v", err)
	}
	if _, err := tv.Execute(ctx, map[string]any{"name": "admin_secret_tool"}); err == nil {
		t.Errorf("out-of-ceiling tool_view must refuse even on a warm LRU entry")
	}
	if _, err := tv.Execute(ctx, map[string]any{"name": "task_create"}); err != nil {
		t.Errorf("in-ceiling tool_view must expand normally: %v", err)
	}
	// Legacy posture unchanged without the ctx value.
	if _, err := tv.Execute(context.Background(), map[string]any{"name": "admin_secret_tool"}); err != nil {
		t.Errorf("standalone caller (no ctx registry) must keep the bound full registry: %v", err)
	}
}
