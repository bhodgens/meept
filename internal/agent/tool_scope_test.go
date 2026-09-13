package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// scopedStubTool is a tools.Tool fixture that carries declared parameters, so
// the zero-property check can be exercised against a tool that is NOT a
// hazard. (stubBaselineTool in spec_test.go declares no properties.)
type scopedStubTool struct {
	name   string
	params llm.FunctionParameters
}

var _ tools.Tool = (*scopedStubTool)(nil)

func (t *scopedStubTool) Name() string                          { return t.name }
func (t *scopedStubTool) Description() string                   { return "scoping fixture" }
func (t *scopedStubTool) Parameters() llm.FunctionParameters    { return t.params }
func (t *scopedStubTool) IsReadOnly(map[string]any) bool        { return false }
func (t *scopedStubTool) IsConcurrencySafe(map[string]any) bool { return false }
func (t *scopedStubTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"ok": true}, nil
}

// coderLikeSpec mirrors the coder's AGENT.md shape: it grants shell_execute
// (not the registered `shell`) plus the usual file tools.
func coderLikeSpec() *AgentSpec {
	return &AgentSpec{
		ID:          "coder",
		Name:        "coder",
		Role:        RoleExecutor,
		Enabled:     true,
		CanDelegate: true,
		AdditionalTools: []string{
			"file_read", "file_write", "file_delete",
			"list_directory", "shell_execute", "json_extract",
		},
	}
}

// registryWithGrants registers every grant a spec resolves to (normalized),
// plus any extra names, as lightweight stubs.
func registryWithGrants(spec *AgentSpec, extra ...string) *PlaceholderToolRegistry {
	reg := NewPlaceholderToolRegistry()
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		reg.Register(newStubBaselineTool(name))
	}
	for _, n := range spec.GrantedToolNames() {
		add(n)
	}
	for _, n := range extra {
		add(n)
	}
	return reg
}

// TestCanonicalToolName_ShellExecuteAlias: the agent-grant vocabulary token
// `shell_execute` resolves to the registered tool name `shell`; any unknown
// name passes through untouched.
func TestCanonicalToolName_ShellExecuteAlias(t *testing.T) {
	if got := CanonicalToolName("shell_execute"); got != "shell" {
		t.Fatalf("CanonicalToolName(shell_execute) = %q, want %q", got, "shell")
	}
	if got := CanonicalToolName("file_read"); got != "file_read" {
		t.Fatalf("CanonicalToolName(file_read) = %q, want unchanged", got)
	}
}

// TestFilterTools_ShellExecuteGrantYieldsShellTool is the regression guard for
// the coder's dead shell tool: granting `shell_execute` must actually offer the
// registered `shell` tool through the real filter.
func TestFilterTools_ShellExecuteGrantYieldsShellTool(t *testing.T) {
	spec := coderLikeSpec()
	reg := &AgentRegistry{tools: registryWithGrants(spec)}

	filtered := reg.filterTools(spec)
	if filtered == nil {
		t.Fatal("filterTools returned nil")
	}
	if filtered.Get("shell") == nil {
		t.Fatalf("grant of shell_execute did not expose the registered `shell` tool; offered=%v", toolDefNames(filtered))
	}
	// And the raw grant token is NOT a registered tool, so it must not appear.
	if filtered.Get("shell_execute") != nil {
		t.Fatalf("shell_execute should not resolve as a registered tool; offered=%v", toolDefNames(filtered))
	}
}

// TestFilterTools_DefaultScopeOffersEveryGrantedTool: with tool_scope_limit
// unset (0), the offered set is exactly the full (normalized, de-duplicated)
// grant set — the existing behavior, unchanged.
func TestFilterTools_DefaultScopeOffersEveryGrantedTool(t *testing.T) {
	spec := coderLikeSpec()
	if spec.ToolScopeLimit != 0 {
		t.Fatalf("fixture ToolScopeLimit = %d, want 0 (default)", spec.ToolScopeLimit)
	}
	reg := &AgentRegistry{tools: registryWithGrants(spec)}

	filtered := reg.filterTools(spec)
	unique := map[string]bool{}
	for _, n := range spec.GrantedToolNames() {
		unique[n] = true
	}
	if got := len(filtered.GetDefinitions()); got != len(unique) {
		// delegate_task is in the baseline and CanDelegate is true, so the
		// count is the full unique grant set.
		t.Fatalf("default scope offered %d tools, want %d (all grants)", got, len(unique))
	}
}

// TestScopedToolNames_CapsAndOrdersAdditionalFirst: the cap keeps at most
// `limit` names, with the agent's additional_tools first (task-relevant tools
// survive) and the baseline after.
func TestScopedToolNames_CapsAndOrdersAdditionalFirst(t *testing.T) {
	spec := coderLikeSpec()
	const limit = 4
	got := spec.ScopedToolNames(limit)
	if len(got) != limit {
		t.Fatalf("ScopedToolNames(%d) returned %d names: %v", limit, len(got), got)
	}
	// First four are the leading additional_tools, canonicalized.
	want := []string{"file_read", "file_write", "file_delete", "list_directory"}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("ScopedToolNames[%d] = %q, want %q (all=%v)", i, got[i], w, got)
		}
	}
	// A cap larger than the grant set returns the whole (normalized) set.
	all := spec.ScopedToolNames(0)
	if len(spec.ScopedToolNames(len(all)+5)) != len(all) {
		t.Fatalf("cap above the grant set must not grow the list")
	}
}

// TestFilterTools_ToolScopeLimitReducesOfferedCount is the end-to-end scoping
// guard: the same coder-like agent offers strictly fewer tools when
// tool_scope_limit is small than with it unset.
func TestFilterTools_ToolScopeLimitReducesOfferedCount(t *testing.T) {
	base := coderLikeSpec()
	reg := &AgentRegistry{tools: registryWithGrants(base)}

	unscoped := len(reg.filterTools(base).GetDefinitions())

	scopedSpec := coderLikeSpec()
	const limit = 6
	scopedSpec.ToolScopeLimit = limit
	scoped := reg.filterTools(scopedSpec).GetDefinitions()

	if len(scoped) != limit {
		t.Fatalf("scoped offered %d tools, want %d", len(scoped), limit)
	}
	if len(scoped) >= unscoped {
		t.Fatalf("scoping did not reduce offered count: scoped=%d unscoped=%d", len(scoped), unscoped)
	}
	// The forced call still has the agent's primary tools among its candidates.
	names := map[string]bool{}
	for _, d := range scoped {
		names[d.Function.Name] = true
	}
	if !names["shell"] {
		t.Fatalf("scoped set lost the shell tool: %v", toolDefNamesOf(scoped))
	}
	t.Logf("offered tools: unscoped=%d scoped(limit=%d)=%d", unscoped, limit, len(scoped))
}

// TestToolsWithoutProperties_FlagsPropertyLessTools: the startup/test-time
// hazard check reports property-less tools and leaves well-formed ones alone.
func TestToolsWithoutProperties_FlagsPropertyLessTools(t *testing.T) {
	reg := NewPlaceholderToolRegistry()
	reg.Register(&scopedStubTool{name: "with_props", params: llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{"path": {Type: "string"}},
	}})
	reg.Register(newStubBaselineTool("no_props")) // Parameters{Type:"object"}, no properties

	got := ToolsWithoutProperties(reg)
	if len(got) != 1 || got[0] != "no_props" {
		t.Fatalf("ToolsWithoutProperties = %v, want [no_props]", got)
	}
	warned := WarnToolsWithoutProperties(reg, slog.Default())
	if len(warned) != 1 || warned[0] != "no_props" {
		t.Fatalf("WarnToolsWithoutProperties = %v, want [no_props]", warned)
	}
	// Nil-safety.
	if ToolsWithoutProperties(nil) != nil {
		t.Fatal("ToolsWithoutProperties(nil) must be nil")
	}
}

func toolDefNames(reg ToolRegistry) []string {
	if reg == nil {
		return nil
	}
	return toolDefNamesOf(reg.GetDefinitions())
}

func toolDefNamesOf(defs []llm.ToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}
