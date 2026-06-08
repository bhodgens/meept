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
	EntryType  string    `json:"entry_type"`            // "message", "branch_point", "compaction", "summary"
	BranchID   string    `json:"branch_id"`             // "main" or branch identifier
	Model      string    `json:"model,omitempty"`
	Name       string    `json:"name,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation associated with a message.
type ToolCall struct {
	ID         int64  `json:"id"`
	MessageID  int64  `json:"message_id"`
	ToolName   string `json:"tool_name"`
	ToolCallID string `json:"tool_call_id"`
	Arguments  string `json:"arguments"`
	Result     string `json:"result"`
	Seq        int    `json:"seq"`
}

// Branch represents a named branch in a conversation tree.
type Branch struct {
	ID           string `json:"id"`
	LeafID       int64  `json:"leaf_id"`
	MessageCount int    `json:"message_count"`
	Summary      string `json:"summary,omitempty"`
}

// TreeNode represents a single node in the conversation tree.
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
	Create(name string) (*Session, error)
	Get(id string) *Session
	GetByConversationID(conversationID string) *Session
	GetMostRecent() *Session
	List() ([]*Session, error)
	Delete(id string) bool
	Attach(sessionID, clientID string) error
	Detach(sessionID, clientID string) error
	UpdateActivity(sessionID string) error
	AddWorker(sessionID, workerID string) error
	RemoveWorker(sessionID, workerID string) error
	SaveMessages(sessionID string, messages []Message) error
	GetMessages(sessionID string, offset, limit int) ([]Message, error)
	GetMessageCount(sessionID string) (int, error)
	UpdateDescription(sessionID, description string) error
	UpdateName(sessionID, name string) error
	HasResponses(sessionID string) (bool, error)
	Close() error

	// Tree operations
	GetLeafMessageID(sessionID string) (int64, error)
	SetLeafMessageID(sessionID string, messageID int64) error
	GetMessagePath(sessionID string, leafID int64) ([]Message, error)
	GetMessageBranches(sessionID string) ([]Branch, error)
	NavigateToBranch(sessionID string, targetMessageID int64) (oldLeaf int64, err error)
	GetTree(sessionID string) ([]TreeNode, error)
	ForkSession(sourceSessionID string, fromMessageID int64, newName string) (*Session, error)

	// Tool call operations
	SaveToolCalls(messageID int64, toolCalls []ToolCall) error
	GetToolCalls(messageID int64) ([]ToolCall, error)
	GetToolCallsForMessages(messageIDs []int64) (map[int64][]ToolCall, error)

	// Project operations
	SetProject(sessionID, projectID, projectPath string) error
}
