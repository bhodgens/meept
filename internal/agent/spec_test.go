package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestAgentSpec_EscalationModel_DefaultEmpty verifies that the zero-value
// AgentSpec leaves EscalationModel empty, i.e. escalation is disabled
// (llm-resilience-forest tree 01 leaf 01-spec-config; D14: config surface
// only — the field is inert until the escalation hook consumes it).
func TestAgentSpec_EscalationModel_DefaultEmpty(t *testing.T) {
	spec := &AgentSpec{ID: "x", Enabled: true}
	if spec.EscalationModel != "" {
		t.Errorf("EscalationModel = %q, want empty (disabled)", spec.EscalationModel)
	}
}

// TestAgentSpec_BaselineToolsIncludeRequestHandoff verifies request_handoff is
// a baseline tool: docs/features.md (Built-in Tools, Platform line) and
// docs/workflows/tool-routing.md advertise it as available to all agents via
// the baseline. The tool itself only validates its input and publishes a
// handoff event on the bus — it grants no privileges — and runaway handoff
// cascades are bounded at the orchestrator by maxHandoffSteps (tactical.go),
// so holding it in every specialist's resolved tool set is safe.
func TestAgentSpec_BaselineToolsIncludeRequestHandoff(t *testing.T) {
	found := slices.Contains(BaselineTools, ToolRequestHandoff)
	if !found {
		t.Errorf("BaselineTools must include %q so specialists can request mid-task handoffs; got %v",
			ToolRequestHandoff, BaselineTools)
	}

	// HasTool / AllTools resolve from the baseline for any spec, including
	// one with no additional tools.
	spec := &AgentSpec{ID: "coder", Enabled: true}
	if !spec.HasTool(ToolRequestHandoff) {
		t.Error("spec.HasTool(request_handoff) = false, want true (baseline tool)")
	}
	if !slices.Contains(spec.AllTools(), ToolRequestHandoff) {
		t.Errorf("spec.AllTools() = %v, want it to include request_handoff", spec.AllTools())
	}
}

// TestAgentSpec_AllExecutorSpecsExposeRequestHandoff walks the canonical
// executor roster and verifies each spec's resolved tool surface (baseline +
// additional, delegate stripped when CanDelegate is false) still exposes
// request_handoff through the registry's real filterTools. This is the
// end-to-end guarantee the docs imply: any specialist holding the tool can
// hand work sideways.
func TestAgentSpec_AllExecutorSpecsExposeRequestHandoff(t *testing.T) {
	r := &AgentRegistry{tools: NewPlaceholderToolRegistry()}
	// The parent registry must actually hold the tool: filterTools does not
	// verify that a production registry has every baseline name registered —
	// it only gates by name. Registering request_handoff here proves the
	// resolved surface exposes it; absence from the parent is a daemon wiring
	// concern (components.go registers the real RequestHandoffTool).
	for _, name := range []string{ToolRequestHandoff, ToolPlatformAgents, "delegate_task", "file_read", "shell_execute"} {
		r.tools.(*PlaceholderToolRegistry).Register(newStubBaselineTool(name))
	}
	for _, id := range ExecutorAgentIDs() {
		spec := &AgentSpec{
			ID:              id,
			Name:            id,
			Role:            RoleExecutor,
			Enabled:         true,
			CanDelegate:     false, // strictest shape: delegate stripped, baseline kept
			AdditionalTools: []string{"file_read", "shell_execute"},
		}

		allowed := spec.AllTools()
		if !spec.CanDelegate {
			allowed = removeString(allowed, "delegate_task")
		}
		// Exercising the real filter (NewFilteredToolRegistry) rather than a
		// mirror of its logic keeps this test honest across refactors.
		filtered := NewFilteredToolRegistry(r.tools, allowed)
		if filtered.Get(ToolRequestHandoff) == nil {
			t.Errorf("agent %q: filterTools-resolved registry does not expose request_handoff", id)
		}
		if filtered.Get(ToolPlatformAgents) == nil {
			t.Errorf("agent %q: filterTools-resolved registry lost baseline platform_agents", id)
		}
		if filtered.Get("delegate_task") != nil {
			t.Errorf("agent %q: delegate_task should be stripped when CanDelegate=false", id)
		}
	}
}

// stubBaselineTool is a minimal tools.Tool fixture carrying only a name. It
// exists so filterTools tests can prove allowlist resolution without pulling
// real tool implementations (the agent package cannot import
// internal/tools/builtin — import cycle).
type stubBaselineTool struct {
	name string
}

func newStubBaselineTool(name string) *stubBaselineTool {
	return &stubBaselineTool{name: name}
}

func (t *stubBaselineTool) Name() string        { return t.name }
func (t *stubBaselineTool) Description() string { return "stub fixture for allowlist tests" }
func (t *stubBaselineTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object"}
}
func (t *stubBaselineTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (t *stubBaselineTool) IsReadOnly(map[string]any) bool        { return true }
func (t *stubBaselineTool) IsConcurrencySafe(map[string]any) bool { return true }
