package session

import (
	"testing"
	"time"
)

func TestThread_Struct(t *testing.T) {
	now := time.Now().UTC()
	thread := &Thread{
		ID:             "thread-001",
		SessionID:      "session-abc",
		TopicLabel:     "work",
		ConversationID: "conv-work-xyz",
		CreatedAt:      now,
		LastActivityAt: now,
		Summary:        "Discussion about Go feature implementation",
		IsActive:       true,
	}

	if thread.ID != "thread-001" {
		t.Errorf("expected ID 'thread-001', got %q", thread.ID)
	}
	if thread.TopicLabel != "work" {
		t.Errorf("expected TopicLabel 'work', got %q", thread.TopicLabel)
	}
	if !thread.IsActive {
		t.Error("expected thread to be active")
	}
}

func TestThread_Touch(t *testing.T) {
	thread := &Thread{
		ID:             "thread-001",
		SessionID:      "session-abc",
		TopicLabel:     "work",
		ConversationID: "conv-work-xyz",
		CreatedAt:      time.Now().UTC(),
		LastActivityAt: time.Now().UTC().Add(-1 * time.Hour),
		IsActive:       true,
	}

	before := thread.LastActivityAt
	thread.Touch()
	after := thread.LastActivityAt

	if !after.After(before) {
		t.Error("expected LastActivityAt to be updated")
	}
}

// Note: ThreadRouter-specific tests (TestThreadRouter_Detect,
// TestThreadRouter_CustomKeywords, TestThreadRouter_GenerateThreadID,
// TestThreadRouter_GetSetActiveThread) were relocated to
// internal/agent/thread_router_session_test.go to resolve an import
// cycle (the session package cannot import the agent package).

func TestSession_GetActiveThread(t *testing.T) {
	session := &Session{
		ID:             "session-test",
		ConversationID: "conv-test",
		Threads: map[string]*Thread{
			"thread-work": {ID: "thread-work", TopicLabel: "work", IsActive: false},
			"thread-food": {ID: "thread-food", TopicLabel: "food", IsActive: true},
		},
		ActiveThreadID: "thread-food",
	}

	active := session.GetActiveThread()
	if active == nil {
		t.Fatal("expected active thread")
	}
	if active.TopicLabel != "food" {
		t.Errorf("expected 'food' thread, got %q", active.TopicLabel)
	}
}

func TestSession_GetOrCreateThread(t *testing.T) {
	session := &Session{
		ID:             "session-test",
		ConversationID: "conv-test",
		Threads:        make(map[string]*Thread),
	}

	// Create new thread
	thread := session.GetOrCreateThread("thread-work", "work")
	if thread == nil {
		t.Fatal("expected thread to be created")
	}
	if thread.TopicLabel != "work" {
		t.Errorf("expected topic 'work', got %q", thread.TopicLabel)
	}
	if !thread.IsActive {
		t.Error("expected new thread to be active")
	}
	if session.ActiveThreadID != "thread-work" {
		t.Errorf("expected ActiveThreadID 'thread-work', got %q", session.ActiveThreadID)
	}

	// Get existing thread
	thread2 := session.GetOrCreateThread("thread-work", "work")
	if thread2 != thread {
		t.Error("expected same thread instance")
	}
}

func TestThread_Active(t *testing.T) {
	t.Run("active thread", func(t *testing.T) {
		thread := &Thread{ID: "t1", IsActive: true}
		if !thread.Active() {
			t.Error("expected true for active thread")
		}
	})
	t.Run("inactive thread", func(t *testing.T) {
		thread := &Thread{ID: "t2", IsActive: false}
		if thread.Active() {
			t.Error("expected false for inactive thread")
		}
	})
	t.Run("nil thread", func(t *testing.T) {
		var thread *Thread
		if thread.Active() {
			t.Error("expected false for nil thread")
		}
	})
}

func TestThreadConfig_Defaults(t *testing.T) {
	cfg := DefaultThreadConfig()
	if cfg.EnableTopicDetection {
		t.Error("expected EnableTopicDetection to be false by default")
	}
	if cfg.MinMessagesForSummary != 20 {
		t.Errorf("expected MinMessagesForSummary 20, got %d", cfg.MinMessagesForSummary)
	}
}

func TestSession_GetOrCreateThread_DeactivatesOthers(t *testing.T) {
	session := &Session{
		ID:             "session-test",
		ConversationID: "conv-test",
		Threads:        make(map[string]*Thread),
	}

	// Create first thread
	session.GetOrCreateThread("thread-work", "work")

	// Create second thread
	session.GetOrCreateThread("thread-food", "food")

	// Verify only the new thread is active
	for _, thread := range session.Threads {
		if thread.TopicLabel == "food" && !thread.IsActive {
			t.Error("expected food thread to be active")
		}
		if thread.TopicLabel == "work" && thread.IsActive {
			t.Error("expected work thread to be inactive")
		}
	}
}
