package employee

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// TestEmployeeValidate_ExecutorAgentShape covers the D1 shape check that
// lives in Employee.Validate(): a whitespace-padded executor_agent is a
// typo and rejected; a clean value (and the absent case) pass. Target
// existence/acp-backing is validated at daemon wiring time (registry not
// reachable from this package — see types.go comment).
func TestEmployeeValidate_ExecutorAgentShape(t *testing.T) {
	t.Parallel()

	t.Run("clean reference passes", func(t *testing.T) {
		e := Employee{BotDefinition: validBotDefinition(), Constitution: validConstitution()}
		e.ExecutorAgent = "bob"
		if err := e.Validate(); err != nil {
			t.Errorf("executor_agent=bob should validate, got: %v", err)
		}
	})

	t.Run("absent reference passes", func(t *testing.T) {
		e := Employee{BotDefinition: validBotDefinition(), Constitution: validConstitution()}
		if err := e.Validate(); err != nil {
			t.Errorf("empty executor_agent should validate, got: %v", err)
		}
	})

	t.Run("whitespace-padded reference rejected", func(t *testing.T) {
		e := Employee{BotDefinition: validBotDefinition(), Constitution: validConstitution()}
		e.ExecutorAgent = " bob "
		err := e.Validate()
		if err == nil {
			t.Fatal("expected error for padded executor_agent")
		}
		if !strings.Contains(err.Error(), "executor_agent") {
			t.Errorf("error should mention executor_agent: %v", err)
		}
	})
}

// TestHire_ExecutorAgentPersistence proves the field round-trips through
// the real employee persistence stack (bot definition JSON blob +
// constitution store), mirroring newGoalTestManager's SQLite scaffolding.
func TestHire_ExecutorAgentPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })

	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { constStore.Close() })

	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, nil, nil, nil)

	ctx := context.Background()
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	_, err = m.Hire(ctx, HireRequest{
		ID:            "acp-emp-hire",
		Name:          "acp hire test",
		Prompt:        "test prompt",
		Enabled:       true,
		Triggers:      []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		ExecutorAgent: "bob",
		Constitution:  constitutionToMap(t, c),
	})
	if err != nil {
		t.Fatalf("hire: %v", err)
	}

	got, err := m.GetEmployee(ctx, "acp-emp-hire")
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	if got == nil {
		t.Fatal("GetEmployee returned nil")
	}
	if got.ExecutorAgent != "bob" {
		t.Errorf("ExecutorAgent after hire = %q, want %q", got.ExecutorAgent, "bob")
	}

	// List path must carry it too (the daemon wiring enumerates via
	// ListEmployees).
	emps, err := m.ListEmployees(ctx, "")
	if err != nil {
		t.Fatalf("ListEmployees: %v", err)
	}
	if len(emps) != 1 {
		t.Fatalf("expected 1 employee, got %d", len(emps))
	}
	if emps[0].ExecutorAgent != "bob" {
		t.Errorf("ListEmployees ExecutorAgent = %q, want %q", emps[0].ExecutorAgent, "bob")
	}
}
