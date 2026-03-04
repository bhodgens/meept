package session

import "time"

// Message represents a chat message persisted in a session.
type Message struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Store defines the interface for session persistence.
type Store interface {
	// Create creates a new session with the given name.
	Create(name string) *Session

	// Get retrieves a session by ID.
	Get(id string) *Session

	// GetByConversationID retrieves a session by its conversation ID.
	GetByConversationID(conversationID string) *Session

	// GetMostRecent returns the most recently active session.
	GetMostRecent() *Session

	// List returns all sessions.
	List() []*Session

	// Delete removes a session by ID.
	Delete(id string) bool

	// Attach adds a client to a session.
	Attach(sessionID, clientID string) error

	// Detach removes a client from a session.
	Detach(sessionID, clientID string) error

	// UpdateActivity updates the last activity timestamp for a session.
	UpdateActivity(sessionID string)

	// AddWorker adds a worker ID to a session.
	AddWorker(sessionID, workerID string) error

	// RemoveWorker removes a worker ID from a session.
	RemoveWorker(sessionID, workerID string) error

	// SaveMessages batch-inserts messages for a session.
	SaveMessages(sessionID string, messages []Message) error

	// GetMessages retrieves messages for a session with pagination.
	GetMessages(sessionID string, offset, limit int) ([]Message, error)

	// GetMessageCount returns the number of messages in a session.
	GetMessageCount(sessionID string) (int, error)

	// UpdateDescription updates a session's description.
	UpdateDescription(sessionID, description string) error

	// UpdateName updates a session's name.
	UpdateName(sessionID, name string) error

	// HasResponses checks if a session has any assistant messages.
	HasResponses(sessionID string) (bool, error)

	// Close closes the store and releases resources.
	Close() error
}
