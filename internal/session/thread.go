package session

import "time"

// Thread represents a topic-based partition of a session's conversation.
// Threads allow a single session to maintain multiple independent
// conversation contexts (e.g. "work", "food", "general"), each with its
// own conversation ID, summary, and activity tracking.
//
// Thread-based context partitioning is the user's WIP feature
// (docs/superpowers/plans/2026-06-20-thread-based-context-partitioning.md).
// This file declares the Thread type referenced by:
//   - session.go (Session.Threads map, GetActiveThread, GetOrCreateThread)
//   - thread_store.go (SQLite persistence)
//   - thread_summary.go (summary generation)
//   - thread_migration.go (initial thread seeding)
//   - thread_test.go (unit tests)
//   - services/thread_service.go (RPC service layer)
//   - agent/thread_router.go (dispatcher integration)
//   - tui/thread_indicator.go (TUI display)
//   - daemon/thread_rpc.go (RPC handlers)
type Thread struct {
	// ID is the unique identifier for the thread (e.g. "thread-work-abc123").
	ID string `json:"id"`

	// SessionID is the parent session ID.
	SessionID string `json:"session_id"`

	// TopicLabel is the human-readable topic label (e.g. "work", "food").
	TopicLabel string `json:"topic_label"`

	// ConversationID is the unique conversation ID for this thread's
	// messages. Formed by appending the thread ID to the session's
	// base conversation ID (see Session.GetOrCreateThread).
	ConversationID string `json:"conversation_id"`

	// CreatedAt is when the thread was first created (RFC3339).
	CreatedAt time.Time `json:"created_at"`

	// LastActivityAt is the timestamp of the most recent message in
	// this thread (RFC3339). Updated whenever a message is appended.
	LastActivityAt time.Time `json:"last_activity_at"`

	// Summary holds an optional LLM-generated summary of the thread's
	// conversation context. Empty when no summary has been generated.
	Summary string `json:"summary,omitempty"`

	// IsActive indicates whether this is the currently selected thread
	// for its parent session. Exactly one thread per session is active
	// at a time (enforced by Session.GetOrCreateThread).
	IsActive bool `json:"is_active"`
}

// Touch updates LastActivityAt to the current time. Called by the
// ThreadRouter whenever a thread is accessed to keep activity tracking
// fresh for ordering and pruning decisions.
func (t *Thread) Touch() {
	if t == nil {
		return
	}
	t.LastActivityAt = time.Now().UTC()
}

// Active returns true if this thread is the currently active thread
// for its parent session.
func (t *Thread) Active() bool {
	if t == nil {
		return false
	}
	return t.IsActive
}

// ThreadConfig holds configuration options for thread-based context
// partitioning.
type ThreadConfig struct {
	// EnableTopicDetection enables automatic keyword-based topic
	// detection for new incoming messages. When false, threads must be
	// created explicitly.
	EnableTopicDetection bool

	// MinMessagesForSummary is the number of messages a thread must have
	// before the system will attempt to generate an LLM summary of the
	// thread's conversation context.
	MinMessagesForSummary int
}

// DefaultThreadConfig returns a ThreadConfig with sensible defaults.
func DefaultThreadConfig() ThreadConfig {
	return ThreadConfig{
		EnableTopicDetection:  false,
		MinMessagesForSummary: 20,
	}
}
