package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agents"
)

// TestRosterGateRealBackendEndToEnd exercises Evaluate against the real local
// backend with `true`/`false` commands in a t.TempDir workdir (leaf contract).
func TestRosterGateRealBackendEndToEnd(t *testing.T) {
	dir := t.TempDir()

	t.Run("passing command", func(t *testing.T) {
		g := NewRosterGate(RosterGateConfig{Command: "true", SkipWhenUnchanged: true})
		passed, _, skipped, err := g.Evaluate(context.Background(), dir, true)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if skipped || !passed {
			t.Fatalf("passed=%v skipped=%v, want passed=true skipped=false", passed, skipped)
		}
	})

	t.Run("failing command blocks", func(t *testing.T) {
		g := NewRosterGate(RosterGateConfig{Command: "false", SkipWhenUnchanged: true})
		passed, out, skipped, err := g.Evaluate(context.Background(), dir, true)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if skipped || passed {
			t.Fatalf("passed=%v skipped=%v, want passed=false skipped=false", passed, skipped)
		}
		if !strings.Contains(rosterGateFailureMessage("false", out), "ROSTER GATE FAILED") {
			t.Errorf("failure notice not built from output")
		}
	})
}

// TestRosterGateTimeoutDefaults asserts an unset TimeoutSeconds maps to the
// 300s default at spec conversion and on the runtime config.
func TestRosterGateTimeoutDefaults(t *testing.T) {
	fm := "---\nid: coder\nname: C\ngate:\n  command: \"go test ./...\"\n---\n\nBody.\n"
	def, err := agents.ParseAgentText(fm)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	spec := (&AgentRegistry{}).definitionToSpecForTest(def)
	if spec.Gate == nil {
		t.Fatal("spec.Gate = nil, want config")
	}
	if spec.Gate.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds = %d, want 300 (default)", spec.Gate.TimeoutSeconds)
	}
	if !spec.Gate.SkipWhenUnchanged {
		t.Errorf("SkipWhenUnchanged = false, want true (default)")
	}
}

// TestRosterGateNoGateNoSpecField asserts an AGENT.md without a gate block
// converts to a nil spec.Gate.
func TestRosterGateNoGateNoSpecField(t *testing.T) {
	fm := "---\nid: chat\nname: Chat\n---\n\nBody.\n"
	def, err := agents.ParseAgentText(fm)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	spec := (&AgentRegistry{}).definitionToSpecForTest(def)
	if spec.Gate != nil {
		t.Fatalf("spec.Gate = %+v, want nil", spec.Gate)
	}
	if g := NewRosterGate(RosterGateConfig{}); g != nil {
		t.Fatalf("NewRosterGate(empty) = %+v, want nil", g)
	}
}

// TestRosterGateReadmeWorkflow simulates the documented loop: a read-only
// turn never runs the gate even when the agent has one configured.
func TestRosterGateReadmeWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := NewRosterGate(RosterGateConfig{Command: "false"})
	passed, _, skipped, err := g.Evaluate(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !skipped {
		t.Fatalf("read-only turn skipped = false, want true")
	}
	if passed {
		t.Fatal("read-only turn passed = true, want false")
	}
}

// definitionToSpecForTest exposes the registry conversion for tests without
// constructing a full AgentRegistry (the conversion function is stateless for
// the gate path).
func (r *AgentRegistry) definitionToSpecForTest(def *agents.AgentDefinition) *AgentSpec {
	return r.definitionToSpec(def)
}
