package session

import (
	"log/slog"
	"path/filepath"
	"testing"
)

// testLoggerOrNil discards store logs in tests.
func testLoggerOrNil() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestPlannerInterviewMessageRoundTrip pins the leaf-04 persistence
// shape end-to-end over the real SQLite store: an assistant message
// carrying metadata {planner_interview: task-id} survives save + get,
// and legacy messages without metadata read back byte-identical (nil
// map).
func TestPlannerInterviewMessageRoundTrip(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), testLoggerOrNil())
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	const convID = "conv-interview-roundtrip"
	if _, err := s.Ensure(convID, "interview roundtrip"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	msg := Message{
		SessionID: convID,
		Role:      "assistant",
		Content:   "I need a few answers before I can plan this task:\n\n1. which storage engine?\n\nReply to continue with your answers.",
		Metadata:  map[string]string{"planner_interview": "task-123"},
	}
	if err := s.SaveMessages(convID, []Message{msg}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.GetMessages(convID, 0, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if got[0].Metadata == nil || got[0].Metadata["planner_interview"] != "task-123" {
		t.Errorf("metadata = %v, want planner_interview=task-123", got[0].Metadata)
	}

	// Legacy row: no metadata -> nil map, not an empty non-nil one.
	legacy := Message{SessionID: convID, Role: "user", Content: "hello"}
	if err := s.SaveMessages(convID, []Message{legacy}); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	got2, err := s.GetMessages(convID, 0, 10)
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if len(got2) != 2 {
		t.Fatalf("got %d messages, want 2", len(got2))
	}
	if got2[1].Metadata != nil {
		t.Errorf("legacy metadata = %v, want nil", got2[1].Metadata)
	}
}
