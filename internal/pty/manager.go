package pty

import (
	"fmt"
	"sync"
)

// Manager handles PTY session lifecycle.
type Manager struct {
	mu          sync.RWMutex
	sessions    map[string]Session
	maxSessions int
}

// ManagerConfig holds manager configuration.
type ManagerConfig struct {
	// MaxSessions is the maximum concurrent sessions (0 = unlimited).
	MaxSessions int
}

// NewManager creates a new PTY session manager.
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		sessions:    make(map[string]Session),
		maxSessions: 10, // Default limit
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithMaxSessions sets the maximum number of concurrent sessions.
func WithMaxSessions(n int) ManagerOption {
	return func(m *Manager) {
		if n > 0 {
			m.maxSessions = n
		}
	}
}

// CreateSession creates a new session with the given ID and configuration.
func (m *Manager) CreateSession(id string, cfg SessionConfig) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check ID collision
	if _, exists := m.sessions[id]; exists {
		return nil, fmt.Errorf("session ID already exists: %w", ErrSessionExists)
	}

	// Check limit
	if m.maxSessions > 0 && len(m.sessions) >= m.maxSessions {
		return nil, fmt.Errorf("%w (%d)", ErrSessionLimit, m.maxSessions)
	}

	// Create session
	sess, err := NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	m.sessions[id] = sess
	return sess, nil
}

// CreateAutoSession creates a session with an auto-generated ID.
func (m *Manager) CreateAutoSession(cfg SessionConfig) (string, Session, error) {
	id := fmt.Sprintf("pty-%d", managerCounter.add())

	sess, err := m.CreateSession(id, cfg)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create auto session: %w", err)
	}

	return id, sess, nil
}

var managerCounter = &counter{}

type counter struct {
	mu    sync.Mutex
	value int64
}

func (c *counter) add() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
	return c.value
}

// GetSession retrieves a session by ID.
func (m *Manager) GetSession(id string) Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// DestroySession closes and removes a session.
func (m *Manager) DestroySession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.destroySessionLocked(id)
}

func (m *Manager) destroySessionLocked(id string) error {
	sess, exists := m.sessions[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}

	if err := sess.Close(); err != nil {
		return err
	}

	delete(m.sessions, id)
	return nil
}

// ListSessions returns all active session IDs.
func (m *Manager) ListSessions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Close shuts down all sessions.
func (m *Manager) Close() error {
	// Snapshot session IDs under the lock, then release before doing
	// per-session I/O (sess.Close) to avoid holding the manager write lock
	// during blocking operations.
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.DestroySession(id)
	}

	return nil
}

// SessionCount returns the number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// SessionIDs returns all active session IDs as a slice (non-mutable).
func (m *Manager) SessionIDs() []string {
	return m.ListSessions()
}
