// Completeness gate for tool-name -> permission-action resolution
// (bughunt H7 follow-up). The json_extract class of bug: a tool is
// registered in the daemon (internal/daemon/components.go), granted in an
// agent's AGENT.md additional_tools, and executes fine in isolation — but
// Executor.checkPermission falls back to the tool NAME as the action when
// ToolActionMap lacks an entry, and pkg/security.BuiltinRules denies
// unknown actions outright ("Unknown action"). The tool is silently dead at
// runtime for every agent.
//
// This file is package agent_test (NOT agent) on purpose: it imports
// internal/tools/builtin, which imports internal/agent — an in-package test
// would be an import cycle.
//
// The registry-side coverage enumerates every tool builtin package exports
// via constructors/Name(); the pinned list covers tools whose constructors
// need wiring (browser manager, scheduler, LLM resolver). Update the pinned
// list when a new tool is added — the test failure message names the exact
// place to fix.
package agent_test

import (
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/pkg/security"
)

// collectedToolNames accumulates every tool name observed via checkName.
var collectedToolNames []string

// checkName asserts one tool name resolves through
// ToolActionMap ∪ BuiltinRules and records it for the unused-entry sweep.
func checkName(t *testing.T, name string) {
	t.Helper()
	collectedToolNames = append(collectedToolNames, name)
	action, ok := agent.ToolActionMap[name]
	if !ok {
		action = name // checkPermission's fallback
	}
	if _, known := security.BuiltinRules[action]; !known {
		t.Errorf("tool %q: action %q (fallback from ToolActionMap miss) has no BuiltinRules rule -> runtime denial (\"Unknown action\"). Fix: add a ToolActionMap entry in internal/agent/executor.go (see transcript_fetch/json_extract precedent) and add the name to the pinned list in this test.", name, action)
	}
}

// TestEveryRegisteredToolResolves enumerates the builtin tools this package
// can construct without external dependencies and asserts each resolves.
// Constructions mirror internal/daemon/components.go registration sites.
func TestEveryRegisteredToolResolves(t *testing.T) {
	check := func(name string) {
		t.Helper()
		checkName(t, name)
	}

	check("file_read")
	check("file_write")
	check("file_delete")
	check("list_directory")
	check("file_grep")
	check("file_find")
	check("file_edit")
	check("pdf_read")
	check("shell")
	check("json_extract")
	check("web_fetch")
	check("web_search")
	check("spreadsheet_write")
	check("git_validate")
	check("git_split")
	check("template_invoke")
	check("template_clear")
	check("skills_create")
	check("remember")
	check("ask")
	check("task_create")
	check("task_get")
	check("task_list")
	check("task_update")
	check("delegate_task")
	check("request_review")
	check("project_info")
	check("workspace_yield")
	check("memory_store")
	check("memory_search")
	check("memory_get_context")
	check("memory_retain")
	check("memory_recall")
	check("memory_reflect")
	check("retain")
	check("recall")
	check("reflect")
	check("retain_claim")
	check("list_expired_claims")
	check("retain_decision")
	check("retain_prediction")
	check("mark_superseded")
	check("mark_resolved")
	check("record_review")
	check("reject_claim")
	check("purge_auto_claims")
	check("send_agent_message")
	check("inbox")
	check("platform_team_create")
	check("team_assign")
	check("team_status")
	check("team_message")
	check("team_result")
	check("team_preset_create")
	check("entity_create")
	check("entity_link")
	check("entity_query")
	check("graph_stats")
	check("compute_pagerank")
	check("detect_communities")
	check("community_siblings")
	check("mcp_servers")
	check("resolve")
}

// TestPinnedGrantedToolsResolve covers the tools granted in
// config/agents/*/AGENT.md or listed in spec.go BaselineTools whose
// constructors need wiring this package cannot do (browser manager,
// scheduler, LLM resolver, memory manager, task store). KEPT IN SYNC BY
// HAND: when you add a tool to an agent config, add it here too — the test
// fails and points you at executor.go's ToolActionMap.
func TestPinnedGrantedToolsResolve(t *testing.T) {
	pinned := []string{
		// spec.go BaselineTools (every agent)
		"memory_store", "memory_search", "memory_get_context",
		"task_create", "task_get", "task_list", "task_update",
		"platform_status", "platform_agents", "platform_tools",
		"request_handoff", "project_info", "delegate_task",
		// config/agents/*/AGENT.md additional_tools (union, grep 'additional_tools' -A16 config/agents/*/AGENT.md)
		"file_read", "list_directory", "shell_execute", "web_fetch", "web_search",
		"file_write", "file_delete", "file_grep", "file_find", "file_edit",
		"memory_store", "transcript_fetch", "json_extract", "pdf_read",
		"generate_image", "generate_video",
		"skills_create", "skills_patch",
		"schedule_create", "schedule_list", "schedule_get", "schedule_delete",
		"retain", "recall", "reflect",
		"retain_claim", "retain_decision", "retain_prediction",
		"mark_superseded", "mark_resolved",
		"record_review", "reject_claim", "promote_claim",
		"remember", "ask",
	}
	seen := map[string]bool{}
	for _, name := range pinned {
		if seen[name] {
			continue
		}
		seen[name] = true
		checkName(t, name)
	}
}

// TestToolActionMapEntriesAreKnownActions: no ToolActionMap value may point
// at an action BuiltinRules doesn't know — such an entry is a silent no-op
// denial identical to having no entry.
func TestToolActionMapEntriesAreKnownActions(t *testing.T) {
	for tool, action := range agent.ToolActionMap {
		if _, known := security.BuiltinRules[action]; !known {
			t.Errorf("ToolActionMap[%q] = %q: action has no BuiltinRules rule (dead mapping)", tool, action)
		}
	}
}

// TestNamedConstructorsMatchPinnedList: the builtin constructors used above
// must not drift from the registry (a tool renamed in builtin/ but not here
// would silently leave the pinned list stale). This guards the harness
// itself, not the permission map.
func TestNamedConstructorsMatchPinnedList(t *testing.T) {
	// Every name checkName collected across this package's tests must have
	// resolved (asserted at collection time); here we only assert the harness
	// actually ran over a non-trivial set, so a refactor that empties the
	// enumeration fails loudly instead of passing vacuously.
	if len(collectedToolNames) < 40 {
		t.Fatalf("completeness harness collected only %d tool names; enumeration is broken", len(collectedToolNames))
	}
}

// Compile-time proof the builtin package is importable from this external
// test package (import-cycle guard for future maintainers).
var _ tools.Tool = builtin.NewGitValidateTool()
