package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
)

// executionBusGateExecutor is a deterministic BotExecutor. Injecting it into
// a daemon-constructed GoalLoop lets the gate drive a real Execute turn without
// an LLM, so the only variable under test is whether NewComponents/Start wired
// that loop's bus seam to c.msgBus.
type executionBusGateExecutor struct {
	output string
	tokens int
}

func (e *executionBusGateExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	return e.output, e.tokens, nil
}

// executionBusGateResult mirrors the frozen result wire payload (master C1).
// Decoding into a local shape keeps this gate independent of the internal/agent
// result type, which is owned by another branch.
type executionBusGateResult struct {
	AgentID    string `json:"agent_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

// recvExecutionBusResult waits (bounded) for one result event on a subscriber
// and decodes it. A timeout here is the RED signal: no event reached the bus.
func recvExecutionBusResult(t *testing.T, sub *bus.Subscriber, what string) (executionBusGateResult, string) {
	t.Helper()
	select {
	case m, ok := <-sub.Channel:
		if !ok {
			t.Fatalf("channel closed before %s arrived", what)
		}
		var r executionBusGateResult
		if err := json.Unmarshal(m.Payload, &r); err != nil {
			t.Fatalf("decode %s payload: %v (raw=%s)", what, err, m.Payload)
		}
		return r, m.Source
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s — daemon did not wire the execution bus onto the emitter", what)
	}
	return executionBusGateResult{}, ""
}

// TestComponents_WiresExecutionBus (F7/T1) pins the daemon wiring behaviorally:
// after NewComponents + Start, the GoalLoop constructed by the daemon carries
// c.msgBus on its SetBus seam. The gate seeds a bot + constitution so the daemon
// builds a real bot.Manager and registers a real GoalLoop, injects a fake
// executor into that daemon-constructed loop (WithExecutor copies the struct
// including the wired bus field), runs one Execute turn, and asserts the
// employee.result event lands on the SAME bus passed to NewComponents.
//
// If the daemon never calls loop.SetBus(c.msgBus), the loop's bus is nil, no
// event is published, and this test fails on the 2s timeout — RED for missing
// wiring, not for a missing symbol. Subscriptions use the frozen topic NAME,
// not agent.TopicEmployeeResult (owned by another branch), for the same reason.
func TestComponents_WiresExecutionBus(t *testing.T) {
	const agentID = "wiring-gate-employee"

	ctx := context.Background()
	dir := t.TempDir()
	botDir := filepath.Join(dir, "bots")
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		t.Fatalf("create bot data dir: %v", err)
	}
	dbPath := filepath.Join(botDir, "bots.db")

	// Seed a bot definition and a matching constitution into the DB the daemon
	// will open. Both Bots and Employees are enabled, so the employee stack
	// shares this DB and NewComponents/Start register a GoalLoop for agentID.
	seedStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("seed bot store: %v", err)
	}
	if err := seedStore.Create(ctx, bot.BotDefinition{
		ID:       agentID,
		Name:     "wiring gate",
		Prompt:   "do work",
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeBusEvent, Topic: "wiring.gate.trigger", Enabled: true}},
		Enabled:  true,
	}); err != nil {
		_ = seedStore.Close()
		t.Fatalf("seed bot definition: %v", err)
	}
	_ = seedStore.Close()

	seedConst, err := employee.NewConstitutionStore(dbPath, testLogger(t))
	if err != nil {
		t.Fatalf("seed constitution store: %v", err)
	}
	if err := seedConst.Put(ctx, agentID, employee.Constitution{Purpose: "wiring gate probe"}); err != nil {
		_ = seedConst.Close()
		t.Fatalf("seed constitution: %v", err)
	}
	_ = seedConst.Close()

	cfg := &config.Config{
		Daemon:    config.DaemonConfig{DataDir: dir},
		Bots:      config.BotsConfig{Enabled: true, DataDir: botDir},
		Employees: config.EmployeesConfig{Enabled: true},
		Security:  config.SecurityConfig{AllowedPaths: []string{dir}},
	}
	msgBus := bus.New(nil, testLogger(t))
	comps, err := NewComponents(ctx, cfg, msgBus, testLogger(t))
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() { _ = comps.Stop(context.Background()) })
	if err := comps.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if comps.BotManager == nil {
		t.Fatal("daemon did not construct a bot.Manager for the seeded roster")
	}

	// Subscribe by frozen topic name; the daemon-constructed loop must publish
	// its result onto msgBus for this subscriber to see it.
	results := msgBus.Subscribe("wiring-gate-result", "employee.result")
	defer bus.Unsubscribe(msgBus, results)

	loop := comps.EmployeeManager.GetGoalLoop(agentID)
	if loop == nil {
		t.Fatal("daemon did not register a GoalLoop for the seeded employee")
	}

	// WithExecutor returns a copy that retains every wired field — including
	// the bus the daemon stamped via SetBus. We never call SetBus ourselves:
	// doing so would mask a missing daemon wiring.
	driven := loop.WithExecutor(&executionBusGateExecutor{output: "wired", tokens: 7})
	if _, err := driven.Execute(ctx, employee.PlanRef{ID: "wiring-gate-plan", State: "executing"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, source := recvExecutionBusResult(t, results, "employee.result")
	if got.AgentID != agentID {
		t.Errorf("employee.result agent_id = %q, want %q", got.AgentID, agentID)
	}
	if got.Status != "completed" {
		t.Errorf("employee.result status = %q, want %q", got.Status, "completed")
	}
	if source != "employee" {
		t.Errorf("employee.result source = %q, want %q", source, "employee")
	}
}
