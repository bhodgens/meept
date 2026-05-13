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
