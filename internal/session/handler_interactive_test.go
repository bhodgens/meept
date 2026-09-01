package session

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// TestHandler_SetForeground verifies the session.set_foreground RPC closure:
// happy path persists the flag, a missing session_id errors, and an unknown
// session errors instead of silently succeeding.
func TestHandler_SetForeground(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	s, err := store.Create("rpc-target")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	set := func(t *testing.T, payload string) (any, error) {
		t.Helper()
		return handler.handleSetForeground(&models.BusMessage{Payload: []byte(payload)})
	}

	// Happy path.
	resp, err := set(t, `{"session_id":"`+s.ID+`","foreground":true}`)
	if err != nil {
		t.Fatalf("handleSetForeground: %v", err)
	}
	m, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("response type %T, want map[string]any", resp)
	}
	if fg, _ := m["foreground"].(bool); !fg {
		t.Fatalf("response foreground = %v, want true", m["foreground"])
	}
	got := store.Get(s.ID)
	if got == nil || !got.Foreground {
		t.Fatal("foreground flag not persisted by RPC handler")
	}

	// Missing session_id.
	if _, err := set(t, `{"foreground":true}`); err == nil {
		t.Fatal("expected error for missing session_id")
	}

	// Unknown session.
	if _, err := set(t, `{"session_id":"nope","foreground":true}`); err == nil {
		t.Fatal("expected error for unknown session")
	}

	// Clearing the flag round-trips through the same RPC.
	if _, err := set(t, `{"session_id":"`+s.ID+`","foreground":false}`); err != nil {
		t.Fatalf("clear foreground: %v", err)
	}
	if got := store.Get(s.ID); got == nil || got.Foreground {
		t.Fatal("foreground flag not cleared by RPC handler")
	}
}

// TestHandler_SetForeground_InvalidJSON verifies malformed payloads error.
func TestHandler_SetForeground_InvalidJSON(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	if _, err := handler.handleSetForeground(&models.BusMessage{Payload: []byte("{invalid")}); err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

// TestHandler_SetLastUserMessage verifies the chat-path writer: the handler
// records the user-message timestamp on the session so IsInteractive has a
// message-proximate recency source (D11 — Session.LastActivity is not
// written by the message path).
func TestHandler_SetLastUserMessage(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	s, err := store.Create("msg-target")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(map[string]any{"session_id": s.ID, "at": at.Format(time.RFC3339)})
	resp, err := handler.handleSetLastUserMessage(&models.BusMessage{Payload: payload})
	if err != nil {
		t.Fatalf("handleSetLastUserMessage: %v", err)
	}
	if _, ok := resp.(map[string]any); !ok {
		t.Fatalf("response type %T, want map[string]any", resp)
	}

	got := store.Get(s.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !got.LastUserMessageAt.Equal(at) {
		t.Fatalf("LastUserMessageAt = %v, want %v", got.LastUserMessageAt, at)
	}

	if !IsInteractive(got, at.Add(4*time.Minute), 5*time.Minute) {
		t.Fatal("session with fresh user message should be interactive")
	}
	if IsInteractive(got, at.Add(6*time.Minute), 5*time.Minute) {
		t.Fatal("session outside window should not be interactive")
	}

	// Missing session_id errors.
	if _, err := handler.handleSetLastUserMessage(&models.BusMessage{Payload: []byte(`{"at":"` + at.Format(time.RFC3339) + `"}`)}); err == nil {
		t.Fatal("expected error for missing session_id")
	}
}
