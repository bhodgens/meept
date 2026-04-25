package telegram

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/session"
)

func TestAgentHandler_CreatesSession(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	sid := handler.getOrCreateSession(12345)
	if sid == "" {
		t.Fatal("expected non-empty session ID")
	}

	if handler.GetSessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", handler.GetSessionCount())
	}
}

func TestAgentHandler_SameSessionForSameChat(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	sid1 := handler.getOrCreateSession(123)
	sid2 := handler.getOrCreateSession(123)

	if sid1 != sid2 {
		t.Errorf("expected same session ID for same chat, got %q and %q", sid1, sid2)
	}

	if handler.GetSessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", handler.GetSessionCount())
	}
}

func TestAgentHandler_DifferentSessionsForDifferentChats(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	sid1 := handler.getOrCreateSession(100)
	sid2 := handler.getOrCreateSession(200)

	if sid1 == sid2 {
		t.Error("expected different session IDs for different chats")
	}

	if handler.GetSessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", handler.GetSessionCount())
	}
}

func TestAgentHandler_ResetSession(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	_ = handler.getOrCreateSession(99999)
	if handler.GetSessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", handler.GetSessionCount())
	}

	handler.ResetSession(99999)

	if handler.GetSessionCount() != 0 {
		t.Errorf("expected 0 sessions after reset, got %d", handler.GetSessionCount())
	}
}

func TestAgentHandler_ResetNonexistentSession(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	// Should not panic
	handler.ResetSession(42)

	if handler.GetSessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", handler.GetSessionCount())
	}
}

func TestAgentHandler_NewCreatesEmptySessions(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	if handler.GetSessionCount() != 0 {
		t.Errorf("expected 0 sessions on new handler, got %d", handler.GetSessionCount())
	}
}

func TestAgentHandler_HandleReturnsErrorWithoutLLM(t *testing.T) {
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop() // no LLM configured
	handler := NewAgentHandler(store, loop, t.TempDir(), nil)

	msg := &Message{
		Chat: Chat{ID: 12345},
		Text: "hello",
	}

	// RunOnce will fail because no LLM client is configured, but
	// Handle wraps the error and returns a user-visible string.
	response, err := handler.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("Handle should not return error directly: %v", err)
	}
	if response == "" {
		t.Error("expected non-empty response (error message)")
	}
}

func TestAgentHandler_SessionPersistence(t *testing.T) {
	dir := t.TempDir()
	store := session.NewMemoryStore(nil)
	loop := agent.NewAgentLoop()

	handler := NewAgentHandler(store, loop, dir, nil)
	_ = handler.getOrCreateSession(42)
	handler.saveSessions()

	// Create a new handler loading from the same dir
	handler2 := NewAgentHandler(store, loop, dir, nil)
	if handler2.GetSessionCount() != 1 {
		t.Errorf("expected 1 loaded session, got %d", handler2.GetSessionCount())
	}

	// The session ID should match
	sid := handler2.getOrCreateSession(42)
	if sid == "" {
		t.Error("expected non-empty session ID after reload")
	}
}
