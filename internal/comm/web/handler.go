package web

import (
	"context"
	"time"
)

// SessionInfo represents a conversation session for API responses.
type SessionInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CreatedAt    string    `json:"created_at"`
	LastActivity string    `json:"last_activity"`
	RequestCount int       `json:"request_count"`
}

// AgentEntry represents an agent for listing via the API.
type AgentEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// DelegateRequest is the payload for delegating a task to an agent.
type DelegateRequest struct {
	Message string `json:"message"`
}

// DelegateResult is the result of a delegation.
type DelegateResult struct {
	AgentID  string `json:"agent_id"`
	Status   string `json:"status"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ToolEntry represents a tool for listing via the API.
type ToolEntry struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// MemoryStoreRequest is the payload for storing a memory.
type MemoryStoreRequest struct {
	Content  string         `json:"content"`
	Type     string         `json:"type"`
	Category string         `json:"category,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MemoryStoreResult is the result of storing a memory.
type MemoryStoreResult struct {
	ID string `json:"id"`
}

// SkillExecuteRequest is the payload for executing a skill.
type SkillExecuteRequest struct {
	Input string `json:"input"`
}

// SkillExecuteResult is the result of executing a skill.
type SkillExecuteResult struct {
	Content          string `json:"content"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

// ChatStreamer provides streaming chat functionality.
type ChatStreamer interface {
	ChatStream(ctx context.Context, message string, chunks chan<- string) error
}

// SessionManager provides session CRUD operations.
type SessionManager interface {
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	CreateSession(ctx context.Context, name string) (SessionInfo, error)
	GetSession(ctx context.Context, id string) (SessionInfo, error)
	DeleteSession(ctx context.Context, id string) error
}

// AgentLister provides agent listing and delegation.
type AgentLister interface {
	ListAgents(ctx context.Context) ([]AgentEntry, error)
	DelegateTask(ctx context.Context, agentID string, message string) (DelegateResult, error)
}

// ToolLister provides tool listing.
type ToolLister interface {
	ListTools(ctx context.Context) ([]ToolEntry, error)
}

// MemoryStore provides memory storage.
type MemoryStore interface {
	StoreMemory(ctx context.Context, req MemoryStoreRequest) (MemoryStoreResult, error)
}

// SkillExecutor provides skill execution.
type SkillExecutor interface {
	ExecuteSkill(ctx context.Context, name string, input string) (SkillExecuteResult, error)
}

// JobScheduler provides job scheduling operations.
type JobScheduler interface {
	CreateJob(ctx context.Context, cfg map[string]any) (string, error)
	GetJob(ctx context.Context, id string) (map[string]any, error)
	CancelJob(ctx context.Context, id string) error
}

// Timestamp helpers

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func ptrTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
