package employee

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/runtime"
)

// gateLoopHarness builds a GoalLoop with a scripted stub backend whose hash
// commands return a fixed state and whose third call (the gate) returns the
// given exit code/output.
func gateLoopHarness(t *testing.T, gateOutput string, gateExit int, skip bool) (*GoalLoop, *stubReflector) {
	t.Helper()
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithGateBackend(&stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "head\n", ExitCode: 0},
			{Output: gateOutput, ExitCode: gateExit},
			{Output: "", ExitCode: 0}, // repeat hash cmds for later rounds
			{Output: "head\n", ExitCode: 0},
			{Output: gateOutput, ExitCode: gateExit},
		}}).
		WithGateWorkdir("/ws")
	cfg := &GateConfig{
		Command:           "make check",
		TimeoutSeconds:    30,
		SkipWhenUnchanged: skip,
	}
	loop.SetGateConfig(cfg)
	return loop, reflector
}

func TestReflect_GateFail_CannotComplete_FeedbackPresent(t *testing.T) {
	loop, reflector := gateLoopHarness(t, "FAIL tests broke\n", 1, false)
	reflector.queueResponse(`{"health":"healthy","reasoning":"looks done"}`)

	result := &bot.BotExecutionResult{Success: true, Output: "done"}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
	if health != GoalAtRisk {
		t.Errorf("health = %s with failing gate, want at_risk", health.String())
	}

	fb := loop.GateFeedbackBlock()
	if fb == "" {
		t.Fatal("expected non-empty gate feedback block after failure")
	}
	for _, want := range []string{"quality gate feedback", "FAILED", "FAIL tests broke"} {
		if !strings.Contains(fb, want) {
			t.Errorf("feedback block missing %q:\n%s", want, fb)
		}
	}

	// Second round: the feedback block must be injected into prompts.
	reflector.queueResponse(`{"health":"healthy","reasoning":"still looks done"}`)
	_, err = loop.Reflect(context.Background(), PlanRef{ID: "p2"}, result)
	if err != nil {
		t.Fatalf("Reflect round 2 error: %v", err)
	}
}

func TestReflect_GatePass_Completes(t *testing.T) {
	loop, reflector := gateLoopHarness(t, "all green\n", 0, false)
	reflector.queueResponse(`{"health":"healthy","reasoning":"done"}`)

	result := &bot.BotExecutionResult{Success: true, Output: "done"}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
	if health != GoalHealthy {
		t.Errorf("health = %s with passing gate, want healthy", health.String())
	}
	if fb := loop.GateFeedbackBlock(); fb != "" {
		t.Errorf("no feedback expected after passing gate, got:\n%s", fb)
	}
}

func TestReflect_NoGate_LegacyBehavior(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"model says so"}`)

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).WithReflector(reflector)

	result := &bot.BotExecutionResult{Success: true, Output: "model judgment"}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
	if health != GoalHealthy {
		t.Errorf("legacy health = %s, want healthy (model judgment alone)", health.String())
	}
	if loop.GateFeedbackBlock() != "" {
		t.Error("legacy loop must never produce gate feedback")
	}
}

func TestReflect_GateSkip_StillCannotComplete(t *testing.T) {
	// Round 1: gate fails, records state. SkipWhenUnchanged=true.
	loop, reflector := gateLoopHarness(t, "boom\n", 1, true)
	reflector.queueResponse(`{"health":"healthy","reasoning":""}`)
	h1, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, &bot.BotExecutionResult{Success: true})
	if h1 != GoalAtRisk {
		t.Fatalf("round 1 health = %s, want at_risk", h1.String())
	}

	// Round 2 with unchanged workspace: gate is skipped but the goal still
	// cannot complete while the previous failure stands.
	reflector.queueResponse(`{"health":"healthy","reasoning":""}`)
	h2, _ := loop.Reflect(context.Background(), PlanRef{ID: "p2"}, &bot.BotExecutionResult{Success: true})
	if h2 == GoalHealthy {
		t.Error("skipped-but-unresolved gate must not allow completion")
	}
}

func TestGateConfigForGoal_KillSwitch(t *testing.T) {
	loop, _ := gateLoopHarness(t, "ok\n", 0, false)
	if cfg := loop.gateConfigForGoal(context.Background()); cfg == nil || cfg.Command == "" {
		t.Fatal("want loop-level gate while enabled")
	}
	loop.SetGateEnabled(false)
	if cfg := loop.gateConfigForGoal(context.Background()); cfg != nil {
		t.Errorf("kill switch off still returned %+v", cfg)
	}
}

func TestGateConfigForGoal_PerGoalWins(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	g := &Goal{
		EmployeeID: "bot-test-1",
		Title:      "ci",
		Mandate:    "green",
		Source:     SourceUser,
		Gate:       &GateConfig{Command: "make test"},
	}
	if err := store.Create(ctx, g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	loop := NewGoalLoop("bot-test-1", testTier2Constitution(), store, nil)
	loop.SetDefaultGate(&GateConfig{Command: "make check"})
	cfg := loop.gateConfigForGoal(ctx)
	if cfg == nil || cfg.Command != "make test" {
		t.Fatalf("per-goal command = %v, want make test", cfg)
	}
}
