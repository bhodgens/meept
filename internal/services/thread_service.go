package services

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
)

// ThreadService handles Thread CRUD operations.
type ThreadService struct {
	store session.Store
}

// NewThreadService creates a ThreadService.
func NewThreadService(s session.Store) *ThreadService {
	return &ThreadService{store: s}
}

// CreateThreadRequest contains parameters for creating a new thread.
type CreateThreadRequest struct {
	SessionID      string `json:"session_id"`
	TopicLabel     string `json:"topic_label"`
	ConversationID string `json:"conversation_id,omitempty"`
	Summary        string `json:"summary,omitempty"`
	IsActive       bool   `json:"is_active,omitempty"`
}

// CreateThread creates a new thread for the given session.
func (t *ThreadService) CreateThread(ctx context.Context, req CreateThreadRequest) (*session.Thread, error) {
	if t.store == nil {
		return nil, wrapError("thread", "CreateThread", ErrUnavailable)
	}
	if req.SessionID == "" {
		return nil, wrapError("thread", "CreateThread", ErrInvalidInput)
	}
	if req.TopicLabel == "" {
		return nil, wrapError("thread", "CreateThread", ErrInvalidInput)
	}

	now := time.Now().UTC()
	thread := &session.Thread{
		ID:             "thread-" + req.TopicLabel + "-" + id.Generate(""),
		SessionID:      req.SessionID,
		TopicLabel:     req.TopicLabel,
		ConversationID: req.ConversationID,
		Summary:        req.Summary,
		CreatedAt:      now,
		LastActivityAt: now,
		IsActive:       req.IsActive,
	}

	if err := t.store.CreateThread(ctx, thread); err != nil {
		return nil, wrapError("thread", "CreateThread", err)
	}
	return thread, nil
}

// GetThreadRequest contains parameters for getting a thread.
type GetThreadRequest struct {
	ThreadID string `json:"thread_id"`
}

// GetThread retrieves a thread by ID.
func (t *ThreadService) GetThread(ctx context.Context, req GetThreadRequest) (*session.Thread, error) {
	if t.store == nil {
		return nil, wrapError("thread", "GetThread", ErrUnavailable)
	}
	if req.ThreadID == "" {
		return nil, wrapError("thread", "GetThread", ErrInvalidInput)
	}
	thread, err := t.store.GetThread(ctx, req.ThreadID)
	if err != nil {
		return nil, wrapError("thread", "GetThread", err)
	}
	return thread, nil
}

// ListThreadsRequest contains parameters for listing threads.
type ListThreadsRequest struct {
	SessionID string `json:"session_id"`
}

// ListThreads returns all threads for a session.
func (t *ThreadService) ListThreads(ctx context.Context, req ListThreadsRequest) ([]*session.Thread, error) {
	if t.store == nil {
		return nil, wrapError("thread", "ListThreads", ErrUnavailable)
	}
	if req.SessionID == "" {
		return nil, wrapError("thread", "ListThreads", ErrInvalidInput)
	}
	threads, err := t.store.ListThreadsBySession(ctx, req.SessionID)
	if err != nil {
		return nil, wrapError("thread", "ListThreads", err)
	}
	return threads, nil
}

// UpdateThreadRequest contains parameters for updating a thread.
type UpdateThreadRequest struct {
	ThreadID       string `json:"thread_id"`
	TopicLabel     string `json:"topic_label,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Summary        string `json:"summary,omitempty"`
	IsActive       *bool  `json:"is_active,omitempty"` // pointer to distinguish unset vs false
}

// UpdateThread updates an existing thread.
func (t *ThreadService) UpdateThread(ctx context.Context, req UpdateThreadRequest) (*session.Thread, error) {
	if t.store == nil {
		return nil, wrapError("thread", "UpdateThread", ErrUnavailable)
	}
	if req.ThreadID == "" {
		return nil, wrapError("thread", "UpdateThread", ErrInvalidInput)
	}

	thread, err := t.store.GetThread(ctx, req.ThreadID)
	if err != nil {
		return nil, wrapError("thread", "UpdateThread", err)
	}

	if req.TopicLabel != "" {
		thread.TopicLabel = req.TopicLabel
	}
	if req.ConversationID != "" {
		thread.ConversationID = req.ConversationID
	}
	if req.Summary != "" {
		thread.Summary = req.Summary
	}
	if req.IsActive != nil {
		thread.IsActive = *req.IsActive
	}

	if err := t.store.UpdateThread(ctx, thread); err != nil {
		return nil, wrapError("thread", "UpdateThread", err)
	}
	return thread, nil
}

// DeleteThreadRequest contains parameters for deleting a thread.
type DeleteThreadRequest struct {
	ThreadID string `json:"thread_id"`
}

// DeleteThread removes a thread.
func (t *ThreadService) DeleteThread(ctx context.Context, req DeleteThreadRequest) error {
	if t.store == nil {
		return wrapError("thread", "DeleteThread", ErrUnavailable)
	}
	if req.ThreadID == "" {
		return wrapError("thread", "DeleteThread", ErrInvalidInput)
	}
	if err := t.store.DeleteThread(ctx, req.ThreadID); err != nil {
		return wrapError("thread", "DeleteThread", err)
	}
	return nil
}

// GetActiveThreadRequest contains parameters for getting the active thread.
type GetActiveThreadRequest struct {
	SessionID string `json:"session_id"`
}

// GetActiveThread retrieves the active thread for a session.
func (t *ThreadService) GetActiveThread(ctx context.Context, req GetActiveThreadRequest) (*session.Thread, error) {
	if t.store == nil {
		return nil, wrapError("thread", "GetActiveThread", ErrUnavailable)
	}
	if req.SessionID == "" {
		return nil, wrapError("thread", "GetActiveThread", ErrInvalidInput)
	}
	thread, err := t.store.GetActiveThread(ctx, req.SessionID)
	if err != nil {
		return nil, wrapError("thread", "GetActiveThread", err)
	}
	if thread == nil {
		return nil, wrapError("thread", "GetActiveThread", ErrNotFound)
	}
	return thread, nil
}

// SetActiveThreadRequest contains parameters for setting the active thread.
type SetActiveThreadRequest struct {
	SessionID string `json:"session_id"`
	ThreadID  string `json:"thread_id"`
}

// SetActiveThread sets the active thread for a session.
func (t *ThreadService) SetActiveThread(ctx context.Context, req SetActiveThreadRequest) (*session.Thread, error) {
	if t.store == nil {
		return nil, wrapError("thread", "SetActiveThread", ErrUnavailable)
	}
	if req.SessionID == "" {
		return nil, wrapError("thread", "SetActiveThread", ErrInvalidInput)
	}
	if req.ThreadID == "" {
		return nil, wrapError("thread", "SetActiveThread", ErrInvalidInput)
	}
	if err := t.store.SetActiveThread(ctx, req.SessionID, req.ThreadID); err != nil {
		return nil, wrapError("thread", "SetActiveThread", err)
	}
	// Return the newly activated thread
	thread, err := t.store.GetThread(ctx, req.ThreadID)
	if err != nil {
		return nil, wrapError("thread", "SetActiveThread", err)
	}
	return thread, nil
}
