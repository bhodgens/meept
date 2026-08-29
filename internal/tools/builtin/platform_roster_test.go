package builtin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

func newRosterRegistry(t *testing.T) *agent.AgentRegistry {
	t.Helper()
	reg := agent.NewAgentRegistry(agent.RegistryConfig{})
	for _, id := range []string{"coder", "planner"} {
		if err := reg.RegisterSpec(&agent.AgentSpec{
			ID: id, Name: id, Role: agent.RoleExecutor, Enabled: true,
		}); err != nil {
			t.Fatalf("RegisterSpec(%s): %v", id, err)
		}
	}
	return reg
}

func TestPlatformAgentsTool_ReachabilityFields(t *testing.T) {
	reg := newRosterRegistry(t)
	tool := NewPlatformAgentsTool(reg)
	lastActive := time.Now().UTC().Add(-2 * time.Minute)
	tool.SetReachability(func(agentID string) (bool, time.Time, bool) {
		if agentID == "coder" {
			return true, lastActive, true // employee with heartbeat
		}
		if agentID == "planner" {
			return false, time.Time{}, true // employee never seen
		}
		return false, time.Time{}, false // in-process specialist
	})
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out, ok := res.(string)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if !strings.Contains(out, "**reachable**: true | **last_seen**: ") ||
		!strings.Contains(out, "**reachable**: false | **last_seen**: never") {
		t.Fatalf("roster missing reachability fields:\n%s", out)
	}
}
