package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// newThreadTestStore returns a MemoryStore pre-populated with a single
// session (id returned) that tests can use as a parent for threads.
func newThreadTestStore(t *testing.T) (*session.MemoryStore, string) {
	t.Helper()
	store := session.NewMemoryStore(slog.Default())
	sess, err := store.Create("test-session")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	return store, sess.ID
}

// addThread is a helper that creates a thread directly via the store so tests
// have a known fixture before exercising the service.
func addThread(t *testing.T, store *session.MemoryStore, sessionID, threadID, topic string) *session.Thread {
	t.Helper()
	thread := &session.Thread{
		ID:             threadID,
		SessionID:      sessionID,
		TopicLabel:     topic,
		ConversationID: "conv-" + topic,
	}
	if err := store.CreateThread(context.Background(), thread); err != nil {
		t.Fatalf("failed to seed thread: %v", err)
	}
	return thread
}

func TestNewThreadService(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)
	if s == nil {
		t.Fatal("expected non-nil ThreadService")
	}
}

func TestNewThreadService_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	if s == nil {
		t.Fatal("expected non-nil ThreadService even with nil store")
	}
}

func TestCreateThread_Success(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	thread, err := s.CreateThread(context.Background(), CreateThreadRequest{
		SessionID:      sessID,
		TopicLabel:     "work",
		ConversationID: "conv-work-1",
		Summary:        "initial summary",
		IsActive:       true,
	})
	if err != nil {
		t.Fatalf("CreateThread() unexpected error: %v", err)
	}
	if thread == nil {
		t.Fatal("expected non-nil thread")
	}
	if thread.SessionID != sessID {
		t.Errorf("expected SessionID %q, got %q", sessID, thread.SessionID)
	}
	if thread.TopicLabel != "work" {
		t.Errorf("expected TopicLabel %q, got %q", "work", thread.TopicLabel)
	}
	if thread.ConversationID != "conv-work-1" {
		t.Errorf("expected ConversationID %q, got %q", "conv-work-1", thread.ConversationID)
	}
	if thread.Summary != "initial summary" {
		t.Errorf("expected Summary %q, got %q", "initial summary", thread.Summary)
	}
	if !thread.IsActive {
		t.Error("expected IsActive true")
	}
	if !strings.HasPrefix(thread.ID, "thread-work-") {
		t.Errorf("expected thread ID prefix %q, got %q", "thread-work-", thread.ID)
	}
	if thread.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if thread.LastActivityAt.IsZero() {
		t.Error("expected non-zero LastActivityAt")
	}
}

func TestCreateThread_EmptySessionID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.CreateThread(context.Background(), CreateThreadRequest{
		TopicLabel: "work",
	})
	if err == nil {
		t.Fatal("expected error for empty SessionID")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCreateThread_EmptyTopicLabel(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.CreateThread(context.Background(), CreateThreadRequest{
		SessionID: sessID,
	})
	if err == nil {
		t.Fatal("expected error for empty TopicLabel")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCreateThread_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	_, err := s.CreateThread(context.Background(), CreateThreadRequest{
		SessionID:  "sess",
		TopicLabel: "work",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestCreateThread_StoreError(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	// Reference a non-existent session.
	_, err := s.CreateThread(context.Background(), CreateThreadRequest{
		SessionID:  "session-does-not-exist",
		TopicLabel: "work",
	})
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
	// The underlying store returns a plain error (not ErrNotFound) so just
	// verify it's wrapped in a ServiceError.
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Errorf("expected *ServiceError, got %T: %v", err, err)
	}
}

func TestGetThread_Success(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	addThread(t, store, sessID, "thread-work-1", "work")
	s := NewThreadService(store)

	thread, err := s.GetThread(context.Background(), GetThreadRequest{
		ThreadID: "thread-work-1",
	})
	if err != nil {
		t.Fatalf("GetThread() unexpected error: %v", err)
	}
	if thread == nil {
		t.Fatal("expected non-nil thread")
	}
	if thread.ID != "thread-work-1" {
		t.Errorf("expected ID %q, got %q", "thread-work-1", thread.ID)
	}
	if thread.SessionID != sessID {
		t.Errorf("expected SessionID %q, got %q", sessID, thread.SessionID)
	}
}

func TestGetThread_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.GetThread(context.Background(), GetThreadRequest{
		ThreadID: "thread-missing",
	})
	if err == nil {
		t.Fatal("expected error for missing thread")
	}
}

func TestGetThread_EmptyThreadID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.GetThread(context.Background(), GetThreadRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestGetThread_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	_, err := s.GetThread(context.Background(), GetThreadRequest{ThreadID: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestListThreads_Success(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	addThread(t, store, sessID, "thread-a", "alpha")
	addThread(t, store, sessID, "thread-b", "beta")
	s := NewThreadService(store)

	threads, err := s.ListThreads(context.Background(), ListThreadsRequest{
		SessionID: sessID,
	})
	if err != nil {
		t.Fatalf("ListThreads() unexpected error: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
	// Verify the returned IDs are the ones we created (order undefined).
	seen := map[string]bool{}
	for _, th := range threads {
		seen[th.ID] = true
	}
	if !seen["thread-a"] || !seen["thread-b"] {
		t.Errorf("expected thread-a and thread-b, seen=%v", seen)
	}
}

func TestListThreads_EmptySession(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	threads, err := s.ListThreads(context.Background(), ListThreadsRequest{
		SessionID: sessID,
	})
	if err != nil {
		t.Fatalf("ListThreads() unexpected error: %v", err)
	}
	if len(threads) != 0 {
		t.Errorf("expected 0 threads for session with none, got %d", len(threads))
	}
}

func TestListThreads_EmptySessionID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.ListThreads(context.Background(), ListThreadsRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestListThreads_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	_, err := s.ListThreads(context.Background(), ListThreadsRequest{SessionID: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestUpdateThread_PartialUpdate(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	orig := addThread(t, store, sessID, "thread-pu", "initial")
	_ = orig
	s := NewThreadService(store)

	newLabel := "updated"
	updated, err := s.UpdateThread(context.Background(), UpdateThreadRequest{
		ThreadID:   "thread-pu",
		TopicLabel: newLabel,
	})
	if err != nil {
		t.Fatalf("UpdateThread() unexpected error: %v", err)
	}
	if updated.TopicLabel != newLabel {
		t.Errorf("expected TopicLabel %q, got %q", newLabel, updated.TopicLabel)
	}
	// ConversationID should remain unchanged (omitted from request).
	if updated.ConversationID != "conv-initial" {
		t.Errorf("expected ConversationID unchanged %q, got %q", "conv-initial", updated.ConversationID)
	}
	if updated.Summary != "" {
		t.Errorf("expected empty Summary, got %q", updated.Summary)
	}
}

func TestUpdateThread_AllFields(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	addThread(t, store, sessID, "thread-af", "init")
	s := NewThreadService(store)

	active := true
	updated, err := s.UpdateThread(context.Background(), UpdateThreadRequest{
		ThreadID:       "thread-af",
		TopicLabel:     "newtopic",
		ConversationID: "conv-new",
		Summary:        "new summary",
		IsActive:       &active,
	})
	if err != nil {
		t.Fatalf("UpdateThread() unexpected error: %v", err)
	}
	if updated.TopicLabel != "newtopic" {
		t.Errorf("expected TopicLabel %q, got %q", "newtopic", updated.TopicLabel)
	}
	if updated.ConversationID != "conv-new" {
		t.Errorf("expected ConversationID %q, got %q", "conv-new", updated.ConversationID)
	}
	if updated.Summary != "new summary" {
		t.Errorf("expected Summary %q, got %q", "new summary", updated.Summary)
	}
	if !updated.IsActive {
		t.Error("expected IsActive true")
	}
}

func TestUpdateThread_Deactivate(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	addThread(t, store, sessID, "thread-d", "topic")
	s := NewThreadService(store)

	inactive := false
	updated, err := s.UpdateThread(context.Background(), UpdateThreadRequest{
		ThreadID: "thread-d",
		IsActive: &inactive,
	})
	if err != nil {
		t.Fatalf("UpdateThread() unexpected error: %v", err)
	}
	if updated.IsActive {
		t.Error("expected IsActive false after deactivation")
	}
}

func TestUpdateThread_EmptyThreadID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.UpdateThread(context.Background(), UpdateThreadRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestUpdateThread_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.UpdateThread(context.Background(), UpdateThreadRequest{
		ThreadID:   "thread-missing",
		TopicLabel: "x",
	})
	if err == nil {
		t.Fatal("expected error for missing thread")
	}
}

func TestUpdateThread_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	_, err := s.UpdateThread(context.Background(), UpdateThreadRequest{ThreadID: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestDeleteThread_Success(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	addThread(t, store, sessID, "thread-del", "topic")
	s := NewThreadService(store)

	if err := s.DeleteThread(context.Background(), DeleteThreadRequest{
		ThreadID: "thread-del",
	}); err != nil {
		t.Fatalf("DeleteThread() unexpected error: %v", err)
	}

	// Verify thread is gone from store.
	if _, err := store.GetThread(context.Background(), "thread-del"); err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteThread_EmptyThreadID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	err := s.DeleteThread(context.Background(), DeleteThreadRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestDeleteThread_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	err := s.DeleteThread(context.Background(), DeleteThreadRequest{
		ThreadID: "thread-missing",
	})
	if err == nil {
		t.Fatal("expected error for missing thread")
	}
}

func TestDeleteThread_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	err := s.DeleteThread(context.Background(), DeleteThreadRequest{ThreadID: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestGetActiveThread_Success(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	thread := addThread(t, store, sessID, "thread-act", "active")
	thread.IsActive = true
	// Manually mark active by setting store-level state via SetActiveThread.
	if err := store.SetActiveThread(context.Background(), sessID, "thread-act"); err != nil {
		t.Fatalf("failed to set active thread: %v", err)
	}
	s := NewThreadService(store)

	got, err := s.GetActiveThread(context.Background(), GetActiveThreadRequest{
		SessionID: sessID,
	})
	if err != nil {
		t.Fatalf("GetActiveThread() unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil thread")
	}
	if got.ID != "thread-act" {
		t.Errorf("expected ID %q, got %q", "thread-act", got.ID)
	}
	if !got.IsActive {
		t.Error("expected IsActive true")
	}
}

func TestGetActiveThread_None(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.GetActiveThread(context.Background(), GetActiveThreadRequest{
		SessionID: sessID,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound when no active thread exists, got %v", err)
	}
}

func TestGetActiveThread_EmptySessionID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.GetActiveThread(context.Background(), GetActiveThreadRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestGetActiveThread_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	_, err := s.GetActiveThread(context.Background(), GetActiveThreadRequest{SessionID: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestSetActiveThread_Success(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	addThread(t, store, sessID, "thread-a", "alpha")
	addThread(t, store, sessID, "thread-b", "beta")
	s := NewThreadService(store)

	// Set thread-b as active.
	thread, err := s.SetActiveThread(context.Background(), SetActiveThreadRequest{
		SessionID: sessID,
		ThreadID:  "thread-b",
	})
	if err != nil {
		t.Fatalf("SetActiveThread() unexpected error: %v", err)
	}
	if thread == nil {
		t.Fatal("expected non-nil thread")
	}
	if thread.ID != "thread-b" {
		t.Errorf("expected ID %q, got %q", "thread-b", thread.ID)
	}
	if !thread.IsActive {
		t.Error("expected IsActive true on returned thread")
	}

	// Verify the store-level state: GetActiveThread returns the new thread.
	active, err := s.GetActiveThread(context.Background(), GetActiveThreadRequest{
		SessionID: sessID,
	})
	if err != nil {
		t.Fatalf("GetActiveThread() error after set: %v", err)
	}
	if active.ID != "thread-b" {
		t.Errorf("expected active thread ID %q, got %q", "thread-b", active.ID)
	}

	// Verify other thread was deactivated: fetch thread-a directly from store.
	other, err := store.GetThread(context.Background(), "thread-a")
	if err != nil {
		t.Fatalf("GetThread(a) error: %v", err)
	}
	if other.IsActive {
		t.Error("expected thread-a to be deactivated after setting thread-b active")
	}
}

func TestSetActiveThread_EmptySessionID(t *testing.T) {
	t.Parallel()
	store, _ := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.SetActiveThread(context.Background(), SetActiveThreadRequest{
		ThreadID: "x",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSetActiveThread_EmptyThreadID(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.SetActiveThread(context.Background(), SetActiveThreadRequest{
		SessionID: sessID,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSetActiveThread_NotFound(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	_, err := s.SetActiveThread(context.Background(), SetActiveThreadRequest{
		SessionID: sessID,
		ThreadID:  "thread-missing",
	})
	if err == nil {
		t.Fatal("expected error for missing thread")
	}
}

func TestSetActiveThread_NilStore(t *testing.T) {
	t.Parallel()
	s := NewThreadService(nil)
	_, err := s.SetActiveThread(context.Background(), SetActiveThreadRequest{
		SessionID: "x",
		ThreadID:  "y",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestCreateThread_GeneratesUniqueIDs(t *testing.T) {
	t.Parallel()
	store, sessID := newThreadTestStore(t)
	s := NewThreadService(store)

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		thread, err := s.CreateThread(context.Background(), CreateThreadRequest{
			SessionID:  sessID,
			TopicLabel: "dup",
		})
		if err != nil {
			t.Fatalf("CreateThread() error on iteration %d: %v", i, err)
		}
		if ids[thread.ID] {
			t.Fatalf("duplicate ID generated: %s", thread.ID)
		}
		ids[thread.ID] = true
	}
}
