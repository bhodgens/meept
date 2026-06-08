package bot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func testRPCHandler(t *testing.T) *RPCHandler {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	mgr := NewManager(store, nil)
	return NewRPCHandler(mgr)
}

func TestRPCHandler_Handlers_ReturnsAllMethods(t *testing.T) {
	h := testRPCHandler(t)
	handlers := h.Handlers()

	expected := []string{
		"bot.create", "bot.get", "bot.list", "bot.update",
		"bot.delete", "bot.pause", "bot.resume", "bot.status",
	}
	for _, method := range expected {
		if _, ok := handlers[method]; !ok {
			t.Errorf("missing handler for method %q", method)
		}
	}
}

func TestRPCHandler_CreateAndGet(t *testing.T) {
	h := testRPCHandler(t)
	ctx := context.Background()

	def := testBotDef("handler-test")
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	raw, _ := json.Marshal(def)
	result, err := h.Handlers()["bot.create"](ctx, raw)
	if err != nil {
		t.Fatalf("bot.create: %v", err)
	}
	resp := result.(map[string]any)
	if resp["id"] != "handler-test" {
		t.Errorf("id = %v, want %q", resp["id"], "handler-test")
	}
	if resp["status"] != "created" {
		t.Errorf("status = %v, want %q", resp["status"], "created")
	}

	getRaw, _ := json.Marshal(map[string]any{"id": "handler-test"})
	getResult, err := h.Handlers()["bot.get"](ctx, getRaw)
	if err != nil {
		t.Fatalf("bot.get: %v", err)
	}
	got := getResult.(*BotDefinition)
	if got.ID != "handler-test" {
		t.Errorf("got ID = %q, want %q", got.ID, "handler-test")
	}
}

func TestRPCHandler_List(t *testing.T) {
	h := testRPCHandler(t)
	ctx := context.Background()

	for _, id := range []string{"hlist-a", "hlist-b"} {
		def := testBotDef(id)
		def.CreatedAt = time.Now().UTC().Truncate(time.Second)
		def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
		raw, _ := json.Marshal(def)
		if _, err := h.Handlers()["bot.create"](ctx, raw); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	listRaw, _ := json.Marshal(map[string]any{})
	listResult, err := h.Handlers()["bot.list"](ctx, listRaw)
	if err != nil {
		t.Fatalf("bot.list: %v", err)
	}
	resp := listResult.(map[string]any)
	bots := resp["bots"].([]BotDefinition)
	if len(bots) != 2 {
		t.Errorf("list returned %d bots, want 2", len(bots))
	}
}

func TestRPCHandler_PauseResume(t *testing.T) {
	h := testRPCHandler(t)
	ctx := context.Background()

	def := testBotDef("pause-handler-test")
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	raw, _ := json.Marshal(def)
	h.Handlers()["bot.create"](ctx, raw)

	pauseRaw, _ := json.Marshal(map[string]any{"id": "pause-handler-test"})
	_, err := h.Handlers()["bot.pause"](ctx, pauseRaw)
	if err != nil {
		t.Fatalf("bot.pause: %v", err)
	}

	getResult, _ := h.Handlers()["bot.get"](ctx, pauseRaw)
	got := getResult.(*BotDefinition)
	if got.Enabled {
		t.Error("bot should be disabled after pause")
	}

	_, err = h.Handlers()["bot.resume"](ctx, pauseRaw)
	if err != nil {
		t.Fatalf("bot.resume: %v", err)
	}

	getResult, _ = h.Handlers()["bot.get"](ctx, pauseRaw)
	got = getResult.(*BotDefinition)
	if !got.Enabled {
		t.Error("bot should be enabled after resume")
	}
}

func TestRPCHandler_Delete(t *testing.T) {
	h := testRPCHandler(t)
	ctx := context.Background()

	def := testBotDef("delete-handler-test")
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	raw, _ := json.Marshal(def)
	h.Handlers()["bot.create"](ctx, raw)

	deleteRaw, _ := json.Marshal(map[string]any{"id": "delete-handler-test"})
	result, err := h.Handlers()["bot.delete"](ctx, deleteRaw)
	if err != nil {
		t.Fatalf("bot.delete: %v", err)
	}
	resp := result.(map[string]any)
	if resp["status"] != "deleted" {
		t.Errorf("status = %v, want %q", resp["status"], "deleted")
	}

	_, err = h.Handlers()["bot.get"](ctx, deleteRaw)
	if err == nil {
		t.Error("expected error getting deleted bot")
	}
}

func TestRPCHandler_Status(t *testing.T) {
	h := testRPCHandler(t)
	ctx := context.Background()

	def := testBotDef("status-handler-test")
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	raw, _ := json.Marshal(def)
	h.Handlers()["bot.create"](ctx, raw)

	statusRaw, _ := json.Marshal(map[string]any{"id": "status-handler-test"})
	result, err := h.Handlers()["bot.status"](ctx, statusRaw)
	if err != nil {
		t.Fatalf("bot.status: %v", err)
	}
	state := result.(*BotState)
	if state.Status != BotStatusStopped {
		t.Errorf("status = %q, want %q", state.Status, BotStatusStopped)
	}
}
