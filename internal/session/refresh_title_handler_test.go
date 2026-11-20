package session

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// TestHandler_RefreshTitle_NilRefresher verifies that handleRefreshTitle
// returns an error when the refresher is not configured.
func TestHandler_RefreshTitle_NilRefresher(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())
	// handler.refresher is nil

	params := RefreshRequest{
		SessionID: "test-123",
		Topic:     "debugging",
	}
	payload, _ := json.Marshal(params)

	_, err := handler.handleRefreshTitle(&models.BusMessage{Payload: payload})
	if err == nil {
		t.Error("expected error when refresher is nil")
	}
}

// TestHandler_RefreshTitle_MissingSessionID verifies error handling when
// session_id is not provided.
func TestHandler_RefreshTitle_MissingSessionID(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	refresher := NewSessionRefresher(nil, slog.Default())
	handler := NewHandler(store, nil, slog.Default())
	handler.refresher = refresher

	params := RefreshRequest{
		Topic: "debugging", // missing session_id
	}
	payload, _ := json.Marshal(params)

	_, err := handler.handleRefreshTitle(&models.BusMessage{Payload: payload})
	if err == nil {
		t.Error("expected error when session_id is missing")
	}
}

// TestHandler_RefreshTitle_InvalidJSON verifies error handling for malformed JSON.
func TestHandler_RefreshTitle_InvalidJSON(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	// Invalid JSON payload
	payload := []byte(`{invalid json}`)

	_, err := handler.handleRefreshTitle(&models.BusMessage{Payload: payload})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestSessionRefresher_Refresh_NoLLMIntegration verifies that SessionRefresher
// produces valid fallback results when LLM client is nil.
func TestSessionRefresher_Refresh_NoLLMIntegration(t *testing.T) {
	refresher := NewSessionRefresher(nil, slog.Default())

	result, err := refresher.Refresh(t.Context(), RefreshRequest{
		SessionID: "test-123",
		TurnCount: 5,
	})
	if err != nil {
		t.Fatalf("Refresh should not fail: %v", err)
	}

	if result.Name == "" {
		t.Error("Name should not be empty")
	}
	if result.Description == "" {
		t.Error("Description should not be empty")
	}
}

// TestHandler_RefreshTitle_PreservesUserRename pins bughunt 2026-11-19
// wave 2 (finding A): the periodic title refresher must never overwrite a
// user-renamed description. The auto NAME still refreshes, and the
// published session.title_updated event carries the USER's description so
// live GUI/TUI titles never visually revert the rename.
func TestHandler_RefreshTitle_PreservesUserRename(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	msgBus := bus.New(bus.DefaultConfig(), slog.Default())
	defer msgBus.Close()
	handler := NewHandler(store, msgBus, slog.Default())
	handler.refresher = NewSessionRefresher(nil, slog.Default())

	sess, err := store.Create("refresh-rename")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.UpdateDescription(sess.ID, "my renamed session"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	sub := msgBus.Subscribe("refresh-title-pin", "session.title_updated")
	defer bus.Unsubscribe(msgBus, sub)

	payload, err := json.Marshal(RefreshRequest{SessionID: sess.ID, TurnCount: 5, Topic: "debugging"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := handler.handleRefreshTitle(&models.BusMessage{Payload: payload}); err != nil {
		t.Fatalf("handleRefreshTitle: %v", err)
	}

	got := store.Get(sess.ID)
	if got == nil || got.Description != "my renamed session" || !got.DescriptionUserSet {
		t.Fatalf("refresh overwrote the user rename: %+v", got)
	}
	if got.Name == "" || got.Name == "refresh-rename" {
		t.Fatalf("auto name did not refresh on a renamed session: %q", got.Name)
	}

	select {
	case evt := <-sub.Channel:
		var p struct {
			SessionID   string `json:"session_id"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			t.Fatalf("event decode: %v", err)
		}
		if p.Description != "my renamed session" {
			t.Fatalf("title_updated event reverted the rename: %q", p.Description)
		}
	default:
		t.Fatal("no session.title_updated event published")
	}
}

// TestHandler_RefreshTitle_UnrenamedStillAutoNames pins the other half:
// sessions the user never renamed keep refreshing description AND name.
func TestHandler_RefreshTitle_UnrenamedStillAutoNames(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	msgBus := bus.New(bus.DefaultConfig(), slog.Default())
	defer msgBus.Close()
	handler := NewHandler(store, msgBus, slog.Default())
	handler.refresher = NewSessionRefresher(nil, slog.Default())

	sess, err := store.Create("refresh-auto")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	payload, err := json.Marshal(RefreshRequest{SessionID: sess.ID, TurnCount: 7, Topic: "coding"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := handler.handleRefreshTitle(&models.BusMessage{Payload: payload}); err != nil {
		t.Fatalf("handleRefreshTitle: %v", err)
	}
	got := store.Get(sess.ID)
	if got == nil || got.Description == "" || got.DescriptionUserSet {
		t.Fatalf("un-renamed session must auto-name without stamping the marker: %+v", got)
	}
}

// TestHandler_GenerateDescription_PreservesUserRename pins the first-
// exchange auto-namer RPC: when a rename landed while generation was in
// flight, the stored description survives AND the RPC response reports the
// user's text (clients paint the response directly).
func TestHandler_GenerateDescription_PreservesUserRename(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())
	// No summarizer: the simple-extraction fallback still writes via the
	// auto lane, which is exactly the path under test.

	sess, err := store.Create("generate-rename")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.UpdateDescription(sess.ID, "user picked this"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	payload, err := json.Marshal(map[string]string{
		"session_id":    sess.ID,
		"first_message": "help me debug the build pipeline please",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw, err := handler.handleGenerateDescription(&models.BusMessage{Payload: payload})
	if err != nil {
		t.Fatalf("handleGenerateDescription: %v", err)
	}
	resp, ok := raw.(map[string]string)
	if !ok {
		t.Fatalf("unexpected response type %T", raw)
	}
	if resp["description"] != "user picked this" {
		t.Fatalf("response must report the user's description, got %q", resp["description"])
	}
	got := store.Get(sess.ID)
	if got == nil || got.Description != "user picked this" || !got.DescriptionUserSet {
		t.Fatalf("generate overwrote the user rename: %+v", got)
	}
}

// TestHandler_GenerateDescription_UnrenamedStillWorks pins legacy behavior
// for sessions without a rename: the generated description lands and the
// marker stays false so later refreshes keep working.
func TestHandler_GenerateDescription_UnrenamedStillWorks(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	sess, err := store.Create("generate-auto")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	payload, err := json.Marshal(map[string]string{
		"session_id":    sess.ID,
		"first_message": "help me debug the build pipeline please",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw, err := handler.handleGenerateDescription(&models.BusMessage{Payload: payload})
	if err != nil {
		t.Fatalf("handleGenerateDescription: %v", err)
	}
	resp, ok := raw.(map[string]string)
	if !ok {
		t.Fatalf("unexpected response type %T", raw)
	}
	got := store.Get(sess.ID)
	if got == nil || got.Description == "" || got.Description != resp["description"] {
		t.Fatalf("generated description did not land: store=%+v resp=%v", got, resp)
	}
	if got.DescriptionUserSet {
		t.Fatal("auto generation must not stamp the user-set marker")
	}
}

// TestMemoryStore_UserRenameDurability is the MemoryStore parity pin for
// the SQLite durability test above.
func TestMemoryStore_UserRenameDurability(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	sess, err := store.Create("memory-rename")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	written, err := store.UpdateDescriptionAuto(sess.ID, "auto first")
	if err != nil || !written {
		t.Fatalf("auto write on fresh session: written=%v err=%v", written, err)
	}
	if err := store.UpdateDescription(sess.ID, "user rename"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	written, err = store.UpdateDescriptionAuto(sess.ID, "auto again")
	if err != nil {
		t.Fatalf("auto write after rename: %v", err)
	}
	if written {
		t.Fatal("auto write overwrote a user-set description")
	}
	got := store.Get(sess.ID)
	if got == nil || got.Description != "user rename" || !got.DescriptionUserSet {
		t.Fatalf("rename lost: %+v", got)
	}
	if err := store.UpdateName(sess.ID, "auto-name"); err != nil {
		t.Fatalf("UpdateName: %v", err)
	}
	if got := store.Get(sess.ID); got.Name != "auto-name" || got.Description != "user rename" {
		t.Fatalf("name lane disturbed rename: %+v", got)
	}
	if _, err := store.UpdateDescriptionAuto("session-gone", "x"); err == nil {
		t.Error("missing session must error on the auto lane")
	}
}
