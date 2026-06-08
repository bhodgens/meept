package bot

import (
	"context"
	"path/filepath"
	"testing"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewManager(store, nil)
}

func TestManager_CreateBot(t *testing.T) {
	mgr := testManager(t)
	ctx := context.Background()

	def := testBotDef("lifecycle-test")
	err := mgr.CreateBot(ctx, def)
	if err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	got, err := mgr.GetBot(ctx, "lifecycle-test")
	if err != nil {
		t.Fatalf("GetBot: %v", err)
	}
	if got.ID != "lifecycle-test" {
		t.Errorf("ID = %q, want %q", got.ID, "lifecycle-test")
	}
}

func TestManager_DeleteBot_StopsRunning(t *testing.T) {
	mgr := testManager(t)
	ctx := context.Background()

	def := testBotDef("delete-test")
	mgr.CreateBot(ctx, def)

	mgr.mu.Lock()
	mgr.running["delete-test"] = &runningBot{
		cancel: func() {},
		state:  &BotState{Status: BotStatusRunning},
	}
	mgr.mu.Unlock()

	err := mgr.DeleteBot(ctx, "delete-test")
	if err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}

	mgr.mu.RLock()
	_, ok := mgr.running["delete-test"]
	mgr.mu.RUnlock()

	if ok {
		t.Error("bot should be removed from running map after delete")
	}
}

func TestManager_PauseResumeBot(t *testing.T) {
	mgr := testManager(t)
	ctx := context.Background()

	def := testBotDef("pause-test")
	mgr.CreateBot(ctx, def)

	err := mgr.PauseBot(ctx, "pause-test")
	if err != nil {
		t.Fatalf("PauseBot: %v", err)
	}

	got, _ := mgr.GetBot(ctx, "pause-test")
	if got.Enabled {
		t.Error("bot should be disabled after pause")
	}

	err = mgr.ResumeBot(ctx, "pause-test")
	if err != nil {
		t.Fatalf("ResumeBot: %v", err)
	}

	got, _ = mgr.GetBot(ctx, "pause-test")
	if !got.Enabled {
		t.Error("bot should be enabled after resume")
	}
}

func TestManager_ListBots(t *testing.T) {
	mgr := testManager(t)
	ctx := context.Background()

	for _, id := range []string{"bot-a", "bot-b", "bot-c"} {
		def := testBotDef(id)
		mgr.CreateBot(ctx, def)
	}

	bots, err := mgr.ListBots(ctx)
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 3 {
		t.Errorf("ListBots returned %d, want 3", len(bots))
	}
}

func TestManager_GetBotStatus_NotRunning(t *testing.T) {
	mgr := testManager(t)
	ctx := context.Background()

	def := testBotDef("status-test")
	mgr.CreateBot(ctx, def)

	state, err := mgr.GetBotStatus(ctx, "status-test")
	if err != nil {
		t.Fatalf("GetBotStatus: %v", err)
	}
	if state.Status != BotStatusStopped {
		t.Errorf("Status = %q, want %q", state.Status, BotStatusStopped)
	}
}
