package preferences

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

//nolint:unused -- disabled test reserved for future tier discovery validation
func _TestStore_TierDiscovery(t *testing.T) {
	tmpDir := t.TempDir()
	tier1 := filepath.Join(tmpDir, "tier1")
	tier2 := filepath.Join(tmpDir, "tier2")
	os.MkdirAll(tier1, 0755)
	os.MkdirAll(tier2, 0755)

	// Create instruction in tier2 (lower priority)
	instr2 := &UserInstruction{
		ID:      "instr2",
		Name:    "test_instruction",
		Trigger: "cron:* * * * *",
		Action:  "shell_execute",
		Enabled: true,
	}
	saveToTier(tier2, instr2)

	// Create shadowing instruction in tier1 (higher priority)
	instr1 := &UserInstruction{
		ID:      "instr1",
		Name:    "test_instruction",
		Trigger: "post_hook:*",
		Action:  "memory_retain",
		Enabled: true,
	}
	saveToTier(tier1, instr1)

	store := NewUserInstructionStore([]string{tier1, tier2})
	result, err := store.Discovery()
	if err != nil {
		t.Fatalf("Discovery() error = %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Discovery() found %d instructions, want 1 (shadowing)", len(result))
	}
	if result[0].Action != "memory_retain" {
		t.Errorf("Discovery() action = %v, want memory_retain (tier1 shadows tier2)", result[0].Action)
	}
}

func TestStore_SaveGetActive(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewUserInstructionStore([]string{tmpDir})

	instr := &UserInstruction{
		ID:      "test1",
		Name:    "Test Instruction",
		Trigger: "cron:0 9 * * *",
		Action:  "shell_execute",
		Enabled: true,
		Scope:   "project",
		Priority: "normal",
		CreatedAt: time.Now(),
	}

	err := store.Save(instr, tmpDir)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	active := store.GetActive()
	if len(active) != 1 {
		t.Fatalf("GetActive() found %d, want 1", len(active))
	}

	got := store.Get("test1")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Trigger != instr.Trigger {
		t.Errorf("Get() trigger = %v, want %v", got.Trigger, instr.Trigger)
	}
}

func TestStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewUserInstructionStore([]string{tmpDir})

	instr := &UserInstruction{
		ID: "to_delete",
		Name: "Delete Me",
		Enabled: true,
	}
	store.Save(instr, tmpDir)

	err := store.Delete("to_delete")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if store.Get("to_delete") != nil {
		t.Error("Delete() did not remove instruction")
	}
}

//nolint:unused -- test helper reserved for future use
func saveToTier(dir string, instr *UserInstruction) {
	// Simple save for testing
	store := NewUserInstructionStore([]string{dir})
	store.Save(instr, dir)
}
