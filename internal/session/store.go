package session

import "time"

// Message represents a chat message persisted in a session.
type Message struct {
	ID         int64     `json:"id"`
	SessionID  string    `json:"session_id"`
	ParentID   *int64    `json:"parent_id,omitempty"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Timestamp  time.Time `json:"timestamp"`
	EntryType  string    `json:"entry_type"`
	BranchID   string    `json:"branch_id"`
	Model      string    `json:"model,omitempty"`
	Name       string    `json:"name,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
}

// ToolCall represents a normalized tool call associated with a message.
type ToolCall struct {
	ID         int64  `json:"id"`
	MessageID  int64  `json:"message_id"`
	ToolName   string `json:"tool_name"`
	ToolCallID string `json:"tool_call_id"`
	Arguments  string `json:"arguments"`
	Result     string `json:"result,omitempty"`
	Seq        int    `json:"seq"`
}

// Branch represents a conversation branch within a session.
type Branch struct {
	ID           string `json:"id"`
	LeafID       int64  `json:"leaf_id"`
	MessageCount int    `json:"message_count"`
	Summary      string `json:"summary,omitempty"`
}

// CompactionEntry represents a compaction entry that summarizes a range of
// compressed messages. The Content field holds the raw JSON and CompressedIDs
// is parsed from that JSON for convenience.
type CompactionEntry struct {
	ID            int64   `json:"id"`
	SessionID     string  `json:"session_id"`
	ParentID      *int64  `json:"parent_id,omitempty"`
	Content       string  `json:"content"`
	Timestamp     time.Time `json:"timestamp"`
	CompressedIDs []int64 `json:"compressed_ids"`
}

// TreeNode represents a single node in the conversation tree for visualization.
type TreeNode struct {
	ID        int64  `json:"id"`
	ParentID  int64  `json:"parent_id"`
	Role      string `json:"role"`
	EntryType string `json:"entry_type"`
	BranchID  string `json:"branch_id"`
	Content   string `json:"content,omitempty"`
	Timestamp string `json:"timestamp"`
	IsLeaf    bool   `json:"is_leaf"`
}

// Store defines the interface for session persistence.
type Store interface {
	// Create creates a new session with the given name.
	Create(name string) (*Session, error)

	// Get retrieves a session by ID.
	Get(id string) *Session

	// GetByConversationID retrieves a session by its conversation ID.
	GetByConversationID(conversationID string) *Session

	// GetMostRecent returns the most recently active session.
	GetMostRecent() *Session

	// List returns all sessions.
	List() ([]*Session, error)

	// Delete removes a session by ID.
	Delete(id string) bool

	// Attach adds a client to a session.
	Attach(sessionID, clientID string) error

	// Detach removes a client from a session.
	Detach(sessionID, clientID string) error

	// UpdateActivity updates the last activity timestamp for a session.
	UpdateActivity(sessionID string) error

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

	// GetLeafMessageID returns the current leaf message ID for a session.
	// Returns 0 if no leaf is set.
	GetLeafMessageID(sessionID string) (int64, error)

	// SetLeafMessageID updates the leaf message ID for a session.
	SetLeafMessageID(sessionID string, messageID int64) error

	// GetMessagePath returns the path from root to the given leaf message ID,
	// ordered from root to leaf.
	GetMessagePath(sessionID string, leafID int64) ([]Message, error)

	// GetMessageBranches returns all branches in a session.
	GetMessageBranches(sessionID string) ([]Branch, error)

	// GetTree returns all nodes in the session tree for visualization.
	GetTree(sessionID string) ([]TreeNode, error)

	// NavigateToBranch moves the session leaf to a target message, returning
	// the old leaf ID. This enables branching from a prior point.
	NavigateToBranch(sessionID string, targetMessageID int64) (oldLeaf int64, err error)

	// ForkSession creates a new session by copying messages from root to
	// fromMessageID from the source session.
	ForkSession(sourceSessionID string, fromMessageID int64, newName string) (*Session, error)

	// InsertCompaction inserts a compaction entry that replaces the given
	// compressed message IDs with a summary. Returns the new compaction entry ID.
	InsertCompaction(sessionID string, parentID int64, summary string, compressedIDs []int64) (int64, error)

	// ReparentAfterCompaction re-parents all messages whose current parent is
	// afterID (or that are children of messages in the compacted range) to point
	// to compactionID instead. This ensures the tree path walks through the
	// compaction entry, skipping the compacted messages.
	ReparentAfterCompaction(sessionID string, afterID int64, compactionID int64) error

	// GetCompactionEntries retrieves all compaction entries for a session,
	// ordered by ID. Returns an empty slice if no compaction entries exist.
	GetCompactionEntries(sessionID string) ([]CompactionEntry, error)

	// SaveToolCalls persists tool calls associated with a message.
	SaveToolCalls(messageID int64, toolCalls []ToolCall) error

	// GetToolCalls retrieves all tool calls for a single message.
	GetToolCalls(messageID int64) ([]ToolCall, error)

	// GetToolCallsForMessages batch-retrieves tool calls for multiple messages.
	// Returns a map from message ID to the slice of tool calls for that message.
	GetToolCallsForMessages(messageIDs []int64) (map[int64][]ToolCall, error)
}
