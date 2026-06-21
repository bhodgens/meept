package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// These tests were relocated from internal/session/thread_test.go to
// resolve an import cycle (the session package cannot import agent).
// They exercise ThreadRouter behavior using the same-package API
// (private fields/methods are accessible here).

func TestThreadRouter_Detect_FromSession(t *testing.T) {
	router := NewThreadRouter()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"work task", "I need to build a Go feature for the API", "work"},
		{"code debug", "I need to debug this panic error", "code"},
		{"lunch food", "What should I have for lunch today?", "food"},
		{"restaurant", "Recommend a good Italian restaurant", "food"},
		{"weekend plans", "What are my weekend plans?", "personal"},
		{"random", "Hello there!", "general"},
		{"shopping list", "I need to buy groceries", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.detectTopic(tt.input)
			if got != tt.expected {
				t.Errorf("Detect(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestThreadRouter_CustomKeywords_FromSession(t *testing.T) {
	router := NewThreadRouter()
	// Add custom keywords for gaming topic
	router.detector.keywords["gaming"] = []string{"game", "play", "steam", "xbox", "playstation"}

	got := router.detectTopic("I'm going to play some steam games")
	if got != "gaming" && got != "work" {
		t.Errorf("expected 'gaming' or 'work' (steam is also a work keyword), got %q", got)
	}
}

func TestThreadRouter_GenerateThreadID_FromSession(t *testing.T) {
	router := NewThreadRouter()

	tests := []struct {
		sessionID string
		topic     string
		want      string
	}{
		// GenerateThreadID uses the last 4 chars of sessionID as the suffix.
		{"session-abc123", "work", "thread-work-c123"},
		{"session-xyz", "food", "thread-food--xyz"}, // last 4 = "-xyz"
		{"short", "general", "thread-general-hort"}, // len>4, so last 4 = "hort"
	}

	for _, tt := range tests {
		got := router.generateThreadID(tt.sessionID, tt.topic)
		if got != tt.want {
			t.Errorf("GenerateThreadID(%q, %q) = %q, want %q", tt.sessionID, tt.topic, got, tt.want)
		}
	}
}

func TestThreadRouter_GetSetActiveThread_FromSession(t *testing.T) {
	store := newMockThreadStore()
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	sessionID := "session-test"
	threadID := "thread-work-test"

	sess := &session.Session{
		ID:             sessionID,
		ConversationID: "conv-test",
		Threads: map[string]*session.Thread{
			threadID: {
				ID:             threadID,
				SessionID:      sessionID,
				TopicLabel:     "work",
				ConversationID: "conv-test",
				IsActive:       false,
			},
		},
	}
	store.addSession(sess)

	// Initially no active thread.
	if a := sess.GetActiveThread(); a != nil {
		t.Error("expected no active thread initially")
	}

	// Set active thread.
	if err := router.SetActiveThread(sessionID, threadID); err != nil {
		t.Fatalf("SetActiveThread failed: %v", err)
	}

	// Get active thread via router.
	got, err := router.GetActiveThread(sessionID)
	if err != nil {
		t.Fatalf("GetActiveThread failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected active thread to exist")
	}
	if got.ID != threadID {
		t.Errorf("expected threadID %q, got %q", threadID, got.ID)
	}
}
