package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
)

// capturingDelegateRegistry records the message a delegation hands the child.
type capturingDelegateRegistry struct {
	mockDelegateRegistry
	capture func(message string)
}

func (c *capturingDelegateRegistry) RunAgent(_ context.Context, _, message, _ string) (string, error) {
	c.capture(message)
	return "ok", nil
}

// TestDelegateTaskTool_ChildMessageIsArtifactOnlyBrief is the subagent-side
// canary for leaf 10-isolation: the child's input must be the structured
// brief (+ caller context), and the spawn context behind it must carry zero
// transcript messages under the artifact_only default.
func TestDelegateTaskTool_ChildMessageIsArtifactOnlyBrief(t *testing.T) {
	var captured string
	reg := &capturingDelegateRegistry{
		capture: func(message string) { captured = message },
	}
	tool := &DelegateTaskTool{registry: reg}

	result, err := tool.Execute(context.Background(), map[string]any{
		"agent_id": "coder",
		"message":  "implement the parser",
		"context":  "prior findings summary",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	dr, ok := result.(DelegateResult)
	if !ok || !dr.Success {
		t.Fatalf("unexpected delegate result: %+v (%T)", result, result)
	}

	// The spawn context the tool builds is artifact_only: Transcript empty
	// by construction even when a parent message list would be supplied.
	spawn := agent.BuildSpawnContext(agent.IsolationArtifactOnly, "implement the parser", nil, nil,
		[]llm.ChatMessage{{Role: llm.RoleTool, Content: "PARENT_TOOL_DUMP_CANARY"}})
	if len(spawn.Transcript) != 0 {
		t.Errorf("delegate spawn context carried %d transcript messages; artifact_only must send none", len(spawn.Transcript))
	}
	if strings.Contains(captured, "PARENT_TOOL_DUMP_CANARY") {
		t.Error("child message contains the parent tool-dump canary")
	}

	for _, want := range []string{"implement the parser", "prior findings summary"} {
		if !strings.Contains(captured, want) {
			t.Errorf("child message missing %q:\n%s", want, captured)
		}
	}
}

// TestDelegateTaskTool_UnknownIsolationFailsClosed mirrors the fail-closed
// contract at the delegate boundary: a bogus isolation value degrades to
// artifact_only (empty transcript), never to a transcript copy.
func TestDelegateTaskTool_UnknownIsolationFailsClosed(t *testing.T) {
	spawn := agent.BuildSpawnContext(agent.ContextIsolation("bogus"), "m", nil, nil,
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "PARENT_TOOL_DUMP_CANARY"}})
	if spawn.Isolation != agent.IsolationArtifactOnly {
		t.Errorf("bogus isolation resolved to %q, want fail-closed artifact_only", spawn.Isolation)
	}
	if len(spawn.Transcript) != 0 {
		t.Errorf("bogus isolation leaked %d transcript messages to the child", len(spawn.Transcript))
	}
}
