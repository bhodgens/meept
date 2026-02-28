package models

// ProgressEvent represents a progress update event
type ProgressEvent struct {
	// Source identification
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	WorkerID       string `json:"worker_id,omitempty"`

	// Progress tracking
	Stage   string  `json:"stage"`   // "thinking", "tool_use", etc.
	Percent float64 `json:"percent"` // 0-100
	Detail  string  `json:"detail,omitempty"`

	// Token tracking
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`

	// Tool execution
	CurrentTool string `json:"current_tool,omitempty"`
	ToolStep    int    `json:"tool_step,omitempty"`
	ToolTotal   int    `json:"tool_total,omitempty"`

	// Conversation state
	ContextResets int `json:"context_resets,omitempty"`
	MessageCount  int `json:"message_count,omitempty"`

	// Timestamp
	Timestamp string `json:"timestamp"`
}

// TokenUsageEvent represents token usage event
type TokenUsageEvent struct {
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	FinishReason     string `json:"finish_reason,omitempty"`
	Timestamp        string `json:"timestamp"`
}

// ContextResetEvent represents a conversation context reset
type ContextResetEvent struct {
	AgentID           string `json:"agent_id"`
	ConversationID    string `json:"conversation_id,omitempty"`
	MessagesRemoved   int    `json:"messages_removed"`
	MessagesRemaining int    `json:"messages_remaining"`
	Reason            string `json:"reason"` // "max_messages", "token_limit", etc.
	Timestamp         string `json:"timestamp"`
}

// WorkerStateEvent represents worker state changes
type WorkerStateEvent struct {
	WorkerID       string `json:"worker_id"`
	OldState       string `json:"old_state"`
	NewState       string `json:"new_state"` // "processing", "executing_tool", etc.
	CurrentTool    string `json:"current_tool,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Timestamp      string `json:"timestamp"`
}
