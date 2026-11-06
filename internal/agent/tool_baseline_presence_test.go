package agent

import (
	"testing"
)

// bughunt config-tools F-3 pins: a conditional baseline tool that is NOT
// registered (knowledge_* while memory.knowledge.enabled=false) must not
// consume a tool_scope_limit slot and thereby displace real tail tools
// (project_info, delegate_task) from a capped agent's grant.

// registryWithNames registers exactly the given names as stub tools.
func registryWithNames(names ...string) *PlaceholderToolRegistry {
	reg := NewPlaceholderToolRegistry()
	for _, n := range names {
		reg.Register(newStubBaselineTool(n))
	}
	return reg
}

// TestScopedToolNamesPresent_DropsDeadBaselineIntoRealTail: with the
// knowledge_* baseline names absent from the registry, the cap backfills
// them with the next real baseline names instead of counting dead slots.
func TestScopedToolNamesPresent_DropsDeadBaselineIntoRealTail(t *testing.T) {
	spec := &AgentSpec{
		AdditionalTools: []string{"file_read", "file_write"},
	}
	present := func(name string) bool {
		switch name {
		case "knowledge_search", "knowledge_read", "knowledge_impact":
			return false // knowledge disabled at boot
		}
		return true
	}
	const limit = 8
	got := spec.ScopedToolNamesPresent(limit, present)

	if len(got) != limit {
		t.Fatalf("got %d names, want %d (cap fully filled): %v", len(got), limit, got)
	}
	for _, dead := range []string{"knowledge_search", "knowledge_read", "knowledge_impact"} {
		for _, n := range got {
			if n == dead {
				t.Fatalf("unregistered baseline %q kept a cap slot: %v", dead, got)
			}
		}
	}
	// Pre-fix slots 6-8 were the dead knowledge_* trio; post-fix they
	// backfill to the task_* baseline names that follow.
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	for _, want := range []string{"task_create", "task_get", "task_list"} {
		if !seen[want] {
			t.Fatalf("real baseline %q was displaced by dead knowledge names: %v", want, got)
		}
	}
	// AdditionalTools still lead.
	if got[0] != "file_read" || got[1] != "file_write" {
		t.Fatalf("additional tools must keep priority order, got %v", got)
	}
}

// TestScopedToolNamesPresent_KeepsDeadAdditionalGrants: presence filtering
// applies to BASELINE names only. An explicit per-agent grant whose tool is
// temporarily unregistered (e.g. an MCP server reconnecting) must keep its
// slot — dropping it would change the agent's own declared grant.
func TestScopedToolNamesPresent_KeepsDeadAdditionalGrants(t *testing.T) {
	spec := &AgentSpec{
		AdditionalTools: []string{"mcp_gone", "file_read"},
	}
	present := func(name string) bool { return name != "mcp_gone" }
	got := spec.ScopedToolNamesPresent(2, present)
	if len(got) != 2 || got[0] != "mcp_gone" || got[1] != "file_read" {
		t.Fatalf("dead additional grant must survive the cap, got %v", got)
	}
}

// TestScopedToolNamesPresent_NilPredicateMatchesLegacy: nil predicate falls
// through to ScopedToolNames byte-identically, and limit<=0 equals
// GrantedToolNames regardless of presence.
func TestScopedToolNamesPresent_NilPredicateMatchesLegacy(t *testing.T) {
	spec := coderLikeSpec()
	spec.ToolScopeLimit = 7
	gotL, wantL := spec.ScopedToolNamesPresent(7, nil), spec.ScopedToolNames(7)
	if len(gotL) != len(wantL) {
		t.Fatalf("nil predicate diverged from legacy cap length: %d vs %d", len(gotL), len(wantL))
	}
	for i := range gotL {
		if gotL[i] != wantL[i] {
			t.Fatalf("nil predicate diverged at %d: %v vs %v", i, gotL, wantL)
		}
	}
	gotU := spec.ScopedToolNamesPresent(0, func(string) bool { return false })
	wantU := spec.GrantedToolNames()
	if len(gotU) != len(wantU) {
		t.Fatalf("limit<=0 must equal GrantedToolNames regardless of presence: %v vs %v", gotU, wantU)
	}
	for i := range gotU {
		if gotU[i] != wantU[i] {
			t.Fatalf("limit<=0 diverged at %d", i)
		}
	}
}

// TestFilterTools_DeadBaselineDoesNotConsumeCap: end-to-end through the
// real filter. The registry lacks the knowledge_* tools (disabled install)
// AND lacks the three dead names entirely; a capped coder-like agent must
// still receive its full cap of REAL tools via baseline backfill.
func TestFilterTools_DeadBaselineDoesNotConsumeCap(t *testing.T) {
	spec := coderLikeSpec() // 6 additional tools, all present below
	const limit = 9
	spec.ToolScopeLimit = limit

	reg := &AgentRegistry{tools: registryWithNames(
		// coder additional tools, normalized:
		"file_read", "file_write", "file_delete",
		"list_directory", "shell", "json_extract",
		// baseline EXCEPT the knowledge_* trio (knowledge disabled):
		"memory_store", "memory_search", "memory_get_context",
		"task_create", "task_get", "task_list", "task_update",
		"platform_status", "platform_agents", "platform_tools",
		"request_handoff", "project_info", "delegate_task",
	)}

	defs := reg.filterTools(spec).GetDefinitions()
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	for _, dead := range []string{"knowledge_search", "knowledge_read", "knowledge_impact"} {
		if names[dead] {
			t.Fatalf("unregistered baseline leaked into the grant: %v", toolDefNamesOf(defs))
		}
	}
	// The cap must fully fill with REAL tools: 6 additional + 3 baseline
	// backfilled past the dead trio (memory_store/search/get_context).
	if len(defs) != limit {
		t.Fatalf("cap under-delivered: got %d, want %d (%v)", len(defs), limit, toolDefNamesOf(defs))
	}
	for _, want := range []string{"memory_store", "memory_search", "memory_get_context"} {
		if !names[want] {
			t.Fatalf("baseline head tools lost: %v", toolDefNamesOf(defs))
		}
	}
	// Mutation proof of the backfill: at limit=12, pre-fix slots 10-12
	// were the dead knowledge_* trio — the filtered registry drops them at
	// definitions time, so the agent got 9 real tools under a 12 cap.
	// Post-fix slots 10-12 backfill to task_create/get/list.
	spec.ToolScopeLimit = 12
	tailDefs := reg.filterTools(spec).GetDefinitions()
	tail := map[string]bool{}
	for _, d := range tailDefs {
		tail[d.Function.Name] = true
	}
	if len(tailDefs) != 12 {
		t.Fatalf("limit=12 cap under-delivered: %d (%v)", len(tailDefs), toolDefNamesOf(tailDefs))
	}
	for _, want := range []string{"task_create", "task_get", "task_list"} {
		if !tail[want] {
			t.Fatalf("backfill tool %q missing at limit=12 — cap still counts dead baseline slots: %v", want, toolDefNamesOf(tailDefs))
		}
	}
	for _, dead := range []string{"knowledge_search", "knowledge_read", "knowledge_impact"} {
		if tail[dead] {
			t.Fatalf("unregistered baseline leaked at limit=12: %v", toolDefNamesOf(tailDefs))
		}
	}
}
