package bot

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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

// TestStore_DeleteThenRecreate covers the 2026-10-25 orphan-bot_states bug:
// the FK ON DELETE CASCADE is inert (no foreign_keys pragma on this store's
// connection), so Delete must explicitly remove the bot_states row or a
// re-create of the same id fails with "UNIQUE constraint failed:
// bot_states.definition_id".
func TestStore_DeleteThenRecreate(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	def := testBotDef("recreate-me")

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Touch the state row so it exists (Create seeds it; UpdateState proves
	// the row is live, mirroring a bot that actually ran).
	if err := store.UpdateState(ctx, BotState{DefinitionID: "recreate-me", Status: BotStatusRunning}); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if err := store.Delete(ctx, "recreate-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// The state row must be gone with the definition — no orphan.
	if _, err := store.GetState(ctx, "recreate-me"); err == nil {
		t.Fatal("bot_states row survived Delete (orphan)")
	}
	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("re-Create same id after Delete: %v", err)
	}
	if _, err := store.Get(ctx, "recreate-me"); err != nil {
		t.Fatalf("Get after re-Create: %v", err)
	}
}

// TestStore_DeleteLegacyOrphans proves the fixed Delete cleans up orphan
// state rows written by the old (definition-only) Delete path: a row that
// outlived its definition is removed and the id becomes reusable.
func TestStore_DeleteLegacyOrphans(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bots.db")

	// Seed an orphan the way the live store got one: create, then delete
	// ONLY the definition row via raw SQL (the pre-fix behaviour).
	seed, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore seed: %v", err)
	}
	ctx := context.Background()
	if err := seed.Create(ctx, testBotDef("legacy-orphan")); err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if _, err := seed.db.ExecContext(ctx, `DELETE FROM bot_definitions WHERE id = ?`, "legacy-orphan"); err != nil {
		t.Fatalf("raw delete definition: %v", err)
	}
	seed.Close()

	// Reopen with the fixed store: Delete must sweep the orphan state row.
	store2, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore fixed: %v", err)
	}
	defer store2.Close()
	if err := store2.Delete(ctx, "legacy-orphan"); err != nil {
		t.Fatalf("Delete legacy orphan: %v", err)
	}
	// No bot_states rows may remain for the id.
	var n int
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open check db: %v", err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_states WHERE definition_id = ?`, "legacy-orphan").Scan(&n); err != nil {
		t.Fatalf("count states: %v", err)
	}
	if n != 0 {
		t.Fatalf("orphan bot_states rows after fixed Delete: %d, want 0", n)
	}
	// And the id is free for re-creation.
	if err := store2.Create(ctx, testBotDef("legacy-orphan")); err != nil {
		t.Fatalf("re-Create legacy id: %v", err)
	}
}
