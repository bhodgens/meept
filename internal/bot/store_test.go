package bot

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func testBotDef(id string) BotDefinition {
	return BotDefinition{
		ID:          id,
		Name:        "Test Bot " + id,
		Description: "A test bot",
		Prompt:      "You are a test bot.",
		Triggers: []BotTrigger{
			{Type: TriggerTypeCron, Schedule: "*/5 * * * *", Enabled: true},
		},
		MemoryScope: MemoryScopePrivate,
		Tools:       []string{"web_fetch"},
		Constraints: BotConstraints{
			MaxIterations:    5,
			Timeout:          2 * time.Minute,
			DailyBudgetCents: 100,
		},
		Enabled:   true,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
}

func TestStore_CreateAndGet(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	def := testBotDef("test-bot-1")

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, "test-bot-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != def.ID {
		t.Errorf("ID = %q, want %q", got.ID, def.ID)
	}
	if got.Name != def.Name {
		t.Errorf("Name = %q, want %q", got.Name, def.Name)
	}
	if len(got.Triggers) != 1 {
		t.Fatalf("Triggers len = %d, want 1", len(got.Triggers))
	}
	if got.Triggers[0].Schedule != "*/5 * * * *" {
		t.Errorf("Trigger Schedule = %q, want %q", got.Triggers[0].Schedule, "*/5 * * * *")
	}
}

func TestStore_List(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for _, id := range []string{"bot-a", "bot-b", "bot-c"} {
		def := testBotDef(id)
		if err := store.Create(ctx, def); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	bots, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(bots) != 3 {
		t.Errorf("List returned %d bots, want 3", len(bots))
	}
}

func TestStore_Update(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	def := testBotDef("test-update")

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	def.Name = "Updated Name"
	def.Prompt = "New prompt"

	if err := store.Update(ctx, def); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get(ctx, "test-update")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Updated Name" {
		t.Errorf("Name = %q, want %q", got.Name, "Updated Name")
	}
	if got.Prompt != "New prompt" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "New prompt")
	}
}

func TestStore_Delete(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	def := testBotDef("test-delete")

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(ctx, "test-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Get(ctx, "test-delete")
	if err == nil {
		t.Fatal("expected error getting deleted bot")
	}
}

func TestStore_DuplicateID(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	def := testBotDef("test-dup")

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := store.Create(ctx, def)
	if err == nil {
		t.Fatal("expected error creating duplicate bot")
	}
}

func TestStore_UpdateState(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	def := testBotDef("test-state")

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	state := BotState{
		DefinitionID:        "test-state",
		Status:              BotStatusRunning,
		LastRunAt:           &now,
		TotalRuns:           42,
		TotalTokensUsed:     50000,
		TotalCostCents:      37,
		ConsecutiveFailures: 0,
	}

	if err := store.UpdateState(ctx, state); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	got, err := store.GetState(ctx, "test-state")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got.TotalRuns != 42 {
		t.Errorf("TotalRuns = %d, want 42", got.TotalRuns)
	}
	if got.Status != BotStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, BotStatusRunning)
	}
}
