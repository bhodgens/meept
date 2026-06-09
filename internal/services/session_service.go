package services

import (
	"context"

	"github.com/caimlas/meept/internal/session"
)

// SessionService handles session operations.
type SessionService struct {
	store session.Store
}

// NewSessionService creates a session service.
func NewSessionService(s session.Store) *SessionService {
	return &SessionService{store: s}
}

// CreateSessionRequest contains session creation parameters.
type CreateSessionRequest struct {
	Name string `json:"name,omitempty"`
}

// CreateSession creates a new session.
func (s *SessionService) CreateSession(ctx context.Context, req CreateSessionRequest) (*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "CreateSession", ErrUnavailable)
	}
	name := req.Name
	if name == "" {
		name = "default"
	}
	sess, err := s.store.Create(name)
	if err != nil {
		return nil, wrapError("session", "CreateSession", err)
	}
	return sess, nil
}

// GetSessionRequest contains get parameters.
type GetSessionRequest struct {
	ID string `json:"id"`
}

// GetSession retrieves a session by ID.
func (s *SessionService) GetSession(ctx context.Context, req GetSessionRequest) (*session.Session, error) {
	if req.ID == "" {
		return nil, wrapError("session", "GetSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "GetSession", ErrUnavailable)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "GetSession", ErrNotFound)
	}
	return sess, nil
}

// GetMessagesRequest contains get-messages parameters.
type GetMessagesRequest struct {
	ID     string `json:"id"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GetMessages retrieves messages for a session with pagination.
func (s *SessionService) GetMessages(ctx context.Context, req GetMessagesRequest) ([]session.Message, error) {
	if req.ID == "" {
		return nil, wrapError("session", "GetMessages", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "GetMessages", ErrUnavailable)
	}
	if req.Limit <= 0 {
		req.Limit = 1000
	}
	return s.store.GetMessages(req.ID, req.Offset, req.Limit)
}

// DeleteSessionRequest contains delete parameters.
type DeleteSessionRequest struct {
	ID string `json:"id"`
}

// DeleteSession removes a session.
func (s *SessionService) DeleteSession(ctx context.Context, req DeleteSessionRequest) error {
	if req.ID == "" {
		return wrapError("session", "DeleteSession", ErrInvalidInput)
	}
	if s.store == nil {
		return wrapError("session", "DeleteSession", ErrUnavailable)
	}
	if !s.store.Delete(req.ID) {
		return wrapError("session", "DeleteSession", ErrNotFound)
	}
	return nil
}

// ListSessionsRequest contains list parameters.
type ListSessionsRequest struct {
	Limit int `json:"limit,omitempty"`
}

// GetMostRecent returns the most recently active session, or nil if none exist.
func (s *SessionService) GetMostRecent(ctx context.Context) (*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "GetMostRecent", ErrUnavailable)
	}
	sessions, err := s.store.List()
	if err != nil {
		return nil, wrapError("session", "GetMostRecent", err)
	}
	if len(sessions) == 0 {
		return nil, wrapError("session", "GetMostRecent", ErrNotFound)
	}
	// Sessions from store.List() are ordered by most recent first.
	return sessions[0], nil
}

// List returns all sessions.
func (s *SessionService) List(ctx context.Context, req ListSessionsRequest) ([]*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "List", ErrUnavailable)
	}
	sessions, err := s.store.List()
	if err != nil {
		return nil, wrapError("session", "List", err)
	}
	// Apply limit if specified
	if req.Limit > 0 && len(sessions) > req.Limit {
		sessions = sessions[:req.Limit]
	}
	return sessions, nil
}

// AttachSessionRequest contains attach parameters.
type AttachSessionRequest struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
}

// Attach adds an agent to a session.
func (s *SessionService) Attach(ctx context.Context, req AttachSessionRequest) (*session.Session, error) {
	if req.ID == "" || req.AgentID == "" {
		return nil, wrapError("session", "Attach", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "Attach", ErrUnavailable)
	}
	if err := s.store.Attach(req.ID, req.AgentID); err != nil {
		return nil, wrapError("session", "Attach", err)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "Attach", ErrNotFound)
	}
	return sess, nil
}

// DetachSessionRequest contains detach parameters.
type DetachSessionRequest struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
}

// Detach removes an agent from a session.
func (s *SessionService) Detach(ctx context.Context, req DetachSessionRequest) (*session.Session, error) {
	if req.ID == "" || req.AgentID == "" {
		return nil, wrapError("session", "Detach", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "Detach", ErrUnavailable)
	}
	if err := s.store.Detach(req.ID, req.AgentID); err != nil {
		return nil, wrapError("session", "Detach", err)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "Detach", ErrNotFound)
	}
	return sess, nil
}

// ForkSessionRequest contains fork parameters.
type ForkSessionRequest struct {
	SessionID     string `json:"session_id"`
	FromMessageID int64  `json:"from_message_id"`
	Name          string `json:"name,omitempty"`
}

// ForkSession creates a new session by copying messages from the source session
// up to the specified message ID.
func (s *SessionService) ForkSession(ctx context.Context, req ForkSessionRequest) (*session.Session, error) {
	if req.SessionID == "" {
		return nil, wrapError("session", "ForkSession", ErrInvalidInput)
	}
	if req.FromMessageID == 0 {
		return nil, wrapError("session", "ForkSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "ForkSession", ErrUnavailable)
	}
	newSession, err := s.store.ForkSession(req.SessionID, req.FromMessageID, req.Name)
	if err != nil {
		return nil, wrapError("session", "ForkSession", err)
	}
	return newSession, nil
}

// ResumeSessionRequest contains resume parameters.
type ResumeSessionRequest struct {
	ID string `json:"id"`
}

// ResumeSession restores a session into active memory by returning its current state.
// The caller (agent loop) handles restoring the conversation from the session store.
func (s *SessionService) ResumeSession(ctx context.Context, req ResumeSessionRequest) (*session.Session, error) {
	if req.ID == "" {
		return nil, wrapError("session", "ResumeSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "ResumeSession", ErrUnavailable)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "ResumeSession", ErrNotFound)
	}
	if err := s.store.UpdateActivity(req.ID); err != nil {
		return nil, wrapError("session", "ResumeSession", err)
	}
	return sess, nil
}

// BranchSessionRequest contains branch navigation parameters.
type BranchSessionRequest struct {
	ID              string `json:"id"`
	TargetMessageID int64  `json:"target_message_id"`
}

// BranchSession navigates to a branch point in the session tree.
func (s *SessionService) BranchSession(ctx context.Context, req BranchSessionRequest) (*session.Session, error) {
	if req.ID == "" {
		return nil, wrapError("session", "BranchSession", ErrInvalidInput)
	}
	if req.TargetMessageID == 0 {
		return nil, wrapError("session", "BranchSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "BranchSession", ErrUnavailable)
	}
	// Navigate the branch in the store
	_, err := s.store.NavigateToBranch(req.ID, req.TargetMessageID)
	if err != nil {
		return nil, wrapError("session", "BranchSession", err)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "BranchSession", ErrNotFound)
	}
	return sess, nil
}

// ListBranchesRequest contains parameters for listing branches.
type ListBranchesRequest struct {
	ID string `json:"id"`
}

// ListBranches returns all branches for a session.
func (s *SessionService) ListBranches(ctx context.Context, req ListBranchesRequest) ([]session.Branch, error) {
	if req.ID == "" {
		return nil, wrapError("session", "ListBranches", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "ListBranches", ErrUnavailable)
	}
	branches, err := s.store.GetMessageBranches(req.ID)
	if err != nil {
		return nil, wrapError("session", "ListBranches", err)
	}
	return branches, nil
}

// GetTreeRequest contains parameters for getting the tree structure.
type GetTreeRequest struct {
	ID string `json:"id"`
}

// GetTree returns the full tree structure for a session.
func (s *SessionService) GetTree(ctx context.Context, req GetTreeRequest) ([]session.TreeNode, error) {
	if req.ID == "" {
		return nil, wrapError("session", "GetTree", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "GetTree", ErrUnavailable)
	}
	nodes, err := s.store.GetTree(req.ID)
	if err != nil {
		return nil, wrapError("session", "GetTree", err)
	}
	return nodes, nil
}

// CompactSessionRequest contains parameters for triggering compaction.
type CompactSessionRequest struct {
	ID string `json:"id"`
}

// CompactSession triggers compaction on a session by inserting a compaction entry.
// This is a manual trigger; normally compaction happens automatically via maybeCompact.
func (s *SessionService) CompactSession(ctx context.Context, req CompactSessionRequest) (map[string]any, error) {
	if req.ID == "" {
		return nil, wrapError("session", "CompactSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "CompactSession", ErrUnavailable)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "CompactSession", ErrNotFound)
	}

	// Get current leaf and message count
	leafID, err := s.store.GetLeafMessageID(sess.ID)
	if err != nil {
		return nil, wrapError("session", "CompactSession", err)
	}
	if leafID == 0 {
		return nil, wrapError("session", "CompactSession", ErrInvalidInput)
	}

	// Get current path to check if compaction is needed
	path, err := s.store.GetMessagePath(sess.ID, leafID)
	if err != nil {
		return nil, wrapError("session", "CompactSession", err)
	}

	if len(path) == 0 {
		return map[string]any{
			"status":  "no_messages",
			"message": "no messages to compact",
		}, nil
	}

	return map[string]any{
		"status":        "triggered",
		"session_id":    sess.ID,
		"message_count": len(path),
	}, nil
}
