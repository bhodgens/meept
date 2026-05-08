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
