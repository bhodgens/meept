package agent

import (
	"context"
			"testing"
	"time"

	"github.com/caimlas/meept/internal/preferences"
)

// TestPhase1E2E tests the full Phase 1 flow: parse -> verify -> save -> list
func TestPhase1E2E(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create components
	store := preferences.NewUserInstructionStore([]string{tmpDir})
	parser := NewInstructionParser()
	verifier := preferences.NewInstructionVerifier(nil)

	// Test input
	input := "Every day at 9am run tests in this project"

	// Step 1: Parse
	parsed, err := parser.Parse(ctx, input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Trigger.Type != "cron" {
		t.Errorf("Parse() trigger type = %v, want cron", parsed.Trigger.Type)
	}
	if parsed.Action.Tool != "shell_execute" {
		t.Errorf("Parse() action tool = %v, want shell_execute", parsed.Action.Tool)
	}
	if parsed.Scope != "project" {
		t.Errorf("Parse() scope = %v, want project", parsed.Scope)
	}

	// Step 2: Verify
	result := verifier.Verify(parsed)
	if !result.Valid {
		t.Fatalf("Verify() invalid: %v", result.Errors)
	}

	// Step 3: Save
	instr := &preferences.UserInstruction{
		ID:         "e2e_test",
		Trigger:    parsed.Trigger.Type + ":" + parsed.Trigger.Pattern,
		Action:     parsed.Action.Tool,
		ActionArgs: parsed.Action.Args,
		Enabled:    true,
		Scope:      parsed.Scope,
		Priority:   parsed.Priority,
		CreatedAt:  time.Now(),
	}

	err = store.Save(instr, tmpDir)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Step 4: List/GetActive
	active := store.GetActive()
	if len(active) != 1 {
		t.Fatalf("GetActive() found %d, want 1", len(active))
	}
	if active[0].ID != "e2e_test" {
		t.Errorf("GetActive()[0].ID = %v, want e2e_test", active[0].ID)
	}

	// Step 5: Delete
	err = store.Delete("e2e_test")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	active = store.GetActive()
	if len(active) != 0 {
		t.Fatalf("After Delete(), GetActive() = %d, want 0", len(active))
	}
}

// TestPhase1IntentInstructionType verifies IntentInstruction is registered
func TestPhase1IntentInstructionType(t *testing.T) {
	// This test verifies the IntentInstruction constant exists and is wired
	intent := IntentInstruction
	if intent != "instruction" {
		t.Errorf("IntentInstruction = %v, want 'instruction'", intent)
	}

	// Verify keywords are registered
	keywords := intent.Keywords()
	found := false
	for _, kw := range keywords {
		if kw == "always" {
			found = true
			break
		}
	}
	if !found {
		t.Error("IntentInstruction.Keywords() missing 'always'")
	}
}
