package session

import (
	"encoding/json"
	"log/slog"
	"testing"

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
