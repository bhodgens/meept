package liteclient

import (
	"context"

	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
)

// SessionManager handles session operations for meept-lite.
type SessionManager struct {
	client        transport.Client
	currentSession *types.Session
	defaultName   string
}

// NewSessionManager creates a new session manager.
func NewSessionManager(client transport.Client, defaultName string) *SessionManager {
	return &SessionManager{
		client:      client,
		defaultName: defaultName,
	}
}

// GetCurrentSession returns the current session.
func (s *SessionManager) GetCurrentSession() *types.Session {
	return s.currentSession
}

// GetSessionName returns the current session name or default.
func (s *SessionManager) GetSessionName() string {
	if s.currentSession != nil {
		if s.currentSession.Description != "" {
			return s.currentSession.Description
		}
		if s.currentSession.Name != "" {
			return s.currentSession.Name
		}
	}
	return s.defaultName
}

// SetSession sets the current session.
func (s *SessionManager) SetSession(session *types.Session) {
	s.currentSession = session
}

// LoadOrCreateSession loads the most recent session or creates a new one.
func (s *SessionManager) LoadOrCreateSession(ctx context.Context, sessionName string) error {
	if sessionName != "" {
		// Try to find or create the named session
		return s.switchSession(ctx, sessionName)
	}

	// Load most recent session
	resp, err := s.client.GetMostRecentSession()
	if err != nil {
		// No sessions exist, create a new one
		session, err := s.client.CreateSession(s.defaultName)
		if err != nil {
			return err
		}
		s.currentSession = session
		return nil
	}

	s.currentSession = resp
	return nil
}

// ListSessions returns all sessions.
func (s *SessionManager) ListSessions(ctx context.Context) ([]types.Session, error) {
	resp, err := s.client.ListSessions()
	if err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

// CreateSession creates a new session with the given name.
func (s *SessionManager) CreateSession(ctx context.Context, name string) error {
	session, err := s.client.CreateSession(name)
	if err != nil {
		return err
	}
	s.currentSession = session
	return nil
}

// SwitchSession switches to an existing session by name or ID.
func (s *SessionManager) SwitchSession(ctx context.Context, identifier string) error {
	return s.switchSession(ctx, identifier)
}

// switchSession finds a session by name or ID and switches to it.
func (s *SessionManager) switchSession(ctx context.Context, identifier string) error {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return err
	}

	// Find session by name or ID
	for _, sess := range sessions {
		if sess.ID == identifier || sess.Name == identifier || sess.Description == identifier {
			s.currentSession = &sess
			return nil
		}
	}

	// Not found, create a new session with this name
	session, err := s.client.CreateSession(identifier)
	if err != nil {
		return err
	}
	s.currentSession = session
	return nil
}

// DeleteSession deletes a session by ID.
func (s *SessionManager) DeleteSession(ctx context.Context, sessionID string) error {
	return s.client.DeleteSession(sessionID)
}

// UpdateSessionDescription updates the current session's description.
func (s *SessionManager) UpdateSessionDescription(ctx context.Context, description string) error {
	if s.currentSession == nil {
		return nil
	}
	return s.client.UpdateSessionDescription(s.currentSession.ID, description)
}
