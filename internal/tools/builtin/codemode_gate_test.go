package builtin

// bughunt 2026-11-07 agent-fixes F-C pin: the codemode sandbox bridge
// re-enters the agent's filtered registry directly, bypassing
// Executor.Execute — where the §6 row-4 hard gate lives. The bridge must
// apply the same verdict via agent.ExposureGateRefusal: a known-but-
// inactive tool is refused with the tool_view recovery directive; a plan-
// less ctx (flag off) executes untouched (byte-identical legacy).

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

func TestCodemodeBridgeHonorsExposureGate(t *testing.T) {
	full := tools.NewRegistry(nil)
	// mcp_thing is NOT in DefaultAlwaysFullTools, so a predicted-mode plan
	// starts it INACTIVE; shell IS core, so it starts active.
	for _, name := range []string{"shell", "mcp_thing"} {
		full.Register(&toolViewFixtureTool{name: name, desc: "the " + name + " tool",
			params: llm.FunctionDef{}.Parameters})
	}
	reg := agent.NewFilteredToolRegistry(full, []string{"shell", "mcp_thing"})

	// Enforcing plan where mcp_thing is KNOWN but NOT active (shell is).
	plan := agent.NewExposurePlan(agent.ExposurePredicted, nil, 0,
		[]string{"shell", "mcp_thing"}, []string{"shell", "mcp_thing"}, reg)
	ctx := agent.WithExposurePlan(context.Background(), plan)

	bridge := codemodeBridgeCall(reg, "mcp_thing")
	if _, err := bridge(ctx, `{}`); err == nil {
		t.Fatal("F-C bypass: sandbox bridge executed a known-but-inactive tool without promotion")
	} else if !strings.Contains(err.Error(), "tool_view") {
		t.Fatalf("bridge refusal must carry the tool_view recovery directive, got: %v", err)
	}

	// An ACTIVE tool still runs through the bridge (the gate narrows, never
	// starves the promoted path).
	bridgeOK := codemodeBridgeCall(reg, "shell")
	if _, err := bridgeOK(ctx, `{}`); err != nil {
		t.Fatalf("active tool must still execute via the bridge: %v", err)
	}

	// Plan-less ctx: legacy posture, the bridge never gates.
	if _, err := bridge(context.Background(), `{}`); err != nil {
		t.Fatalf("no plan = no gate (flag-off legacy): %v", err)
	}
}
