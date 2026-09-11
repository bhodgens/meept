package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// Pins for the e2e T1 spec_pair degradation fix (2026-09-10).
//
// Run 1: a compound request entered spec_pair planning; the daemon's
// strategic planner had NO PairManager wired, planPairSession failed, and
// the old code silently fell through to createFallbackSteps — a single step
// executed by the chat agent with a truncated prompt ("I understand. How may
// I assist you today?"). The daemon wiring is now fixed (components.go wires
// a strategic pair manager); these pins make the agent-layer contract
// explicit:
//
//  1. A spec_pair plan with a wired pair manager produces coder/planner
//     pair steps (actor/reviewer), never a chat-default fallback.
//  2. A spec_pair plan WITHOUT a pair manager FAILS LOUDLY — it must not
//     fall back to direct-mode steps.

func newSpecPairTestPlanner(t *testing.T, withPair bool) (*StrategicPlanner, *task.Store) {
	t.Helper()
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	cfg := StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            bus.New(nil, slogDiscardLogger()),
		Logger:         slogDiscardLogger(),
		PlannerTimeout: 10 * time.Second,
	}
	if withPair {
		cfg.PairManager = NewPairManager(PairManagerConfig{
			TaskStore: taskStore,
			StepStore: taskStore.StepStore(),
			Bus:       bus.New(nil, slogDiscardLogger()),
			Logger:    slogDiscardLogger(),
		})
	}
	sp := NewStrategicPlanner(cfg)
	return sp, taskStore
}

// spec_pair + wired pair manager → pair steps with explicit actor/reviewer
// agent assignments (coder/planner by the default routing table), and the
// actor step must NOT be empty-AgentID (which selectAgent would default to
// chat).
func TestPlanPairSession_WiredManagerAssignsExecutorAgents(t *testing.T) {
	sp, _ := newSpecPairTestPlanner(t, true)

	req := PlanRequest{
		TaskID:       "task-pair-pin",
		SessionID:    "sess-pair-pin",
		Intent:       string(IntentCompound),
		IsCompound:   true,
		Input:        "create a file named hello.txt containing hello, then tell me the full path",
		CompoundType: "sequential",
	}

	steps, err := sp.planPairSession(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("planPairSession: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("want 2 pair steps (actor+reviewer), got %d", len(steps))
	}

	actor, reviewer := steps[0], steps[1]
	if !strings.HasPrefix(actor.Description, "[pair:actor]") {
		t.Errorf("actor step description %q lacks [pair:actor] prefix", actor.Description)
	}
	if actor.AgentID == "" || actor.AgentID == config.AgentIDChat {
		t.Errorf("actor agent = %q; pair actor must be an executor persona (coder), not chat/empty", actor.AgentID)
	}
	if reviewer.AgentID == "" {
		t.Errorf("reviewer agent empty; pair reviewer must be explicitly assigned")
	}
	// Reviewer depends on the actor.
	if len(reviewer.DependsOn) != 1 || reviewer.DependsOn[0] != actor.ID {
		t.Errorf("reviewer DependsOn = %v, want [actor.ID]", reviewer.DependsOn)
	}
	// The full input must survive into the step description — this is what
	// the executor's prompt is built from.
	if !strings.Contains(actor.Description, "tell me the full path") {
		t.Errorf("actor step description truncated: %q", actor.Description)
	}
}

// spec_pair + MISSING pair manager must fail loudly, not degrade to
// direct-mode fallback steps. This is the exact silent-degradation path
// that turned e2e T1 into a chat deflection.
func TestPlan_StrategicSpecPairWithoutPairManagerFailsLoudly(t *testing.T) {
	sp, taskStore := newSpecPairTestPlanner(t, false)

	// Create the task the Plan call operates on.
	tk := task.NewTask("pair-pin", "do two things, then report back")
	tk.Description = "do two things, then report back"
	tk.LinkSession("sess-loud")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := PlanRequest{
		TaskID:     tk.ID,
		SessionID:  "sess-loud",
		Intent:     string(IntentCompound),
		IsCompound: true,
		Input:      tk.Description,
		Mode:       "spec_pair",
	}

	err := sp.Plan(context.Background(), req)
	if err == nil {
		t.Fatal("spec_pair Plan without pair manager returned nil error; silent fallback regression")
	}
	if !strings.Contains(err.Error(), "pair session") {
		t.Errorf("error %v does not mention pair session", err)
	}

	// No steps may have been created for the task.
	steps, listErr := taskStore.StepStore().ListByTaskID(tk.ID)
	if listErr != nil {
		t.Fatalf("list steps: %v", listErr)
	}
	if len(steps) != 0 {
		t.Errorf("spec_pair failure leaked %d fallback steps; must fail without creating steps", len(steps))
	}
}
