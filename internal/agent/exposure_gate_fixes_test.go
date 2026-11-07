package agent

// Pins for bughunt 2026-11-07 agent-fixes F-A / F-B / F-C.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// fakeCeilingRegistry is a minimal ToolRegistry over a fixed tool set.
type fakeCeilingRegistry struct{ tools map[string]tools.Tool }

func (f *fakeCeilingRegistry) Get(name string) tools.Tool        { return f.tools[name] }
func (f *fakeCeilingRegistry) List() []tools.Tool {
	out := make([]tools.Tool, 0, len(f.tools))
	for _, t := range f.tools {
		out = append(out, t)
	}
	return out
}
func (f *fakeCeilingRegistry) GetDefinitions() []llm.ToolDefinition {
	var out []llm.ToolDefinition
	for name := range f.tools {
		out = append(out, llm.ToolDefinition{Function: llm.FunctionDef{Name: name}})
	}
	return out
}

func stubNamedTools(names ...string) map[string]tools.Tool {
	m := make(map[string]tools.Tool, len(names))
	for _, n := range names {
		m[n] = &stubTool{name: n}
	}
	return m
}

// TestExposurePlanLiveRegistryFallback (F-B): a tool registered AFTER the
// episode armed is absent from the frozen knownNames/ceiling snapshot. The
// gate must refuse it as known-but-inactive (not wave it through), and the
// ceiling must ACCEPT it when the filtered registry serves it (a legitimate
// post-arm grant), while a name absent from BOTH stays refused.
func TestExposurePlanLiveRegistryFallback(t *testing.T) {
	live := &fakeCeilingRegistry{tools: stubNamedTools("mcp_new", "web_fetch")}
	// Snapshot taken BEFORE mcp_new existed: known/ceiling hold web_fetch only.
	plan := NewExposurePlan(ExposurePredicted, nil, 0,
		[]string{"web_fetch", "tool_search", "tool_view"}, []string{"web_fetch", "tool_search", "tool_view"}, live)

	if !plan.IsKnown("mcp_new") {
		t.Fatal("post-arm tool must be known via the live filtered registry (fail-open hole, F-B)")
	}
	if plan.IsActive("mcp_new") {
		t.Fatal("precondition: mcp_new must start inactive")
	}
	refuse, directive := ExposureGateRefusal(WithExposurePlan(context.Background(), plan), "mcp_new")
	if !refuse {
		t.Fatal("gate must refuse a known (live) but inactive tool pre-promotion")
	}
	if directive == "" {
		t.Fatal("refusal must carry the tool_view recovery directive")
	}
	if err := plan.Promote("mcp_new"); err != nil {
		t.Fatalf("post-arm tool inside the live filtered registry must promote (F-B ceiling false-refusal): %v", err)
	}
	if refuse, _ := ExposureGateRefusal(WithExposurePlan(context.Background(), plan), "mcp_new"); refuse {
		t.Fatal("promoted tool must execute")
	}
	// A name in NEITHER snapshot nor live registry stays ceiling-refused.
	if err := plan.Promote("hallucinated_tool"); err == nil {
		t.Fatal("out-of-ceiling name must stay refused (row 5 keeps its bite)")
	}
	// Unknown to live registry too => not known => no gate refusal (legacy
	// unknown-tool path stays the error shape).
	if plan.IsKnown("hallucinated_tool") {
		t.Fatal("hallucinated name must not read as known")
	}
}

// TestExposureGateRefusalNilPlan pins the byte-identical legacy posture:
// no plan (flag off) or a non-enforcing plan never refuses.
func TestExposureGateRefusalNilPlan(t *testing.T) {
	if refuse, _ := ExposureGateRefusal(context.Background(), "web_fetch"); refuse {
		t.Fatal("nil plan must never gate (flag-off legacy)")
	}
	full := NewExposurePlan(ExposureFull, nil, 0, []string{"web_fetch"}, []string{"web_fetch"}, nil)
	if refuse, _ := ExposureGateRefusal(WithExposurePlan(context.Background(), full), "web_fetch"); refuse {
		t.Fatal("full/indexed plans are non-enforcing: never gate")
	}
}

// TestChatHandlerClearConversationPrunesExposurePlan (F-A): the production
// /reset path must route through AgentLoop.ClearConversation so the
// conversation's exposure plan dies with it. Before the fix the handler
// called l.conversations.Delete directly and the prune was dead code on
// every real reset.
func TestChatHandlerClearConversationPrunesExposurePlan(t *testing.T) {
	l := NewAgentLoop("sess-fa", t.TempDir())
	l.SetSchemaModeConfig(config.AgentToolsConfig{DynamicToolExposure: true})
	l.schemaModeSet = true

	// Arm a plan for the conversation (predicted mode; nil registry ->
	// ceiling from spec fallback path is fine unseeded).
	if plan := l.ensureExposurePlan("conv-fa"); plan == nil {
		t.Fatal("expected an exposure plan with the dynamic flag on")
	}
	l.exposureMu.Lock()
	_, armed := l.exposurePlans["conv-fa"]
	l.exposureMu.Unlock()
	if !armed {
		t.Fatal("precondition: plan armed")
	}

	h := &ChatHandler{loop: l}
	h.ClearConversation("conv-fa")

	l.exposureMu.Lock()
	_, still := l.exposurePlans["conv-fa"]
	l.exposureMu.Unlock()
	if still {
		t.Fatal("F-A leak: /reset path left the episode's exposure plan frozen (map growth + recycled-ID resurrection)")
	}
}
