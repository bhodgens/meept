// Package memory provides memory storage and retrieval for meept.
package memory

import (
	"encoding/json"
	"time"
)

// MemoryType classifies memory storage subsystems.
type MemoryType string

const (
	// MemoryTypeEpisodic is for conversation and interaction history.
	MemoryTypeEpisodic MemoryType = "episodic"
	// MemoryTypeTask is for domain-specific technical knowledge.
	MemoryTypeTask MemoryType = "task"
	// MemoryTypePersonality is for personality and preference tracking.
	MemoryTypePersonality MemoryType = "personality"
)

// Memory represents a stored memory item.
type Memory struct {
	// ID is the unique identifier (UUID hex string).
	ID string `json:"id"`
	// Content is the textual content of the memory.
	Content string `json:"content"`
	// Type is which subsystem owns this memory.
	Type MemoryType `json:"type"`
	// Category is a finer-grained label (e.g., "conversation", "code").
	Category string `json:"category"`
	// Metadata is arbitrary key/value data attached to the memory.
	Metadata map[string]any `json:"metadata,omitempty"`
	// CreatedAt is when the memory was first stored.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the memory was last modified.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// LastAccessedAt is when the memory was last accessed.
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`

	// Agent attribution fields (for multi-agent orchestration)
	// AgentID identifies which agent created this memory.
	AgentID string `json:"agent_id,omitempty"`
	// SessionID is the conversation session this memory belongs to.
	SessionID string `json:"session_id,omitempty"`
	// TaskID is the task this memory was created during.
	TaskID string `json:"task_id,omitempty"`
}

// MemoryResult is a memory item returned from a search with relevance info.
type MemoryResult struct {
	// Memory is the underlying memory entry.
	Memory Memory `json:"memory"`
	// RelevanceScore is [0.0, 1.0] indicating match quality.
	RelevanceScore float64 `json:"relevance_score"`
	// Source is a human-readable label for the subsystem (e.g., "episodic", "task:code").
	Source string `json:"source"`
}

// MemoryQuery describes a search request against the memory system.
type MemoryQuery struct {
	// Query is the free-text search string.
	Query string `json:"query"`
	// Type restricts results to a single subsystem, or empty for all.
	Type MemoryType `json:"type,omitempty"`
	// Category restricts results to a single category, or empty for all.
	Category string `json:"category,omitempty"`
	// Domain restricts task memories to a specific domain.
	Domain string `json:"domain,omitempty"`
	// Limit is the maximum number of results to return.
	Limit int `json:"limit"`
	// MinRelevance discards results below this threshold.
	MinRelevance float64 `json:"min_relevance,omitempty"`
}

// MemoryStats holds aggregate statistics about stored memories.
type MemoryStats struct {
	// TotalCount is the total number of memory items across all subsystems.
	TotalCount int `json:"total_count"`
	// EpisodicCount is the number of episodic memories.
	EpisodicCount int `json:"episodic_count"`
	// TaskCount is the number of task memories.
	TaskCount int `json:"task_count"`
	// Oldest is the timestamp of the oldest memory.
	Oldest *time.Time `json:"oldest,omitempty"`
	// Newest is the timestamp of the newest memory.
	Newest *time.Time `json:"newest,omitempty"`
}

// ConsolidationReport summarizes a consolidation run.
type ConsolidationReport struct {
	// EpisodicArchived is the number of episodic memories archived.
	EpisodicArchived int `json:"episodic_archived"`
	// SummariesCreated is the number of summary memories created.
	SummariesCreated int `json:"summaries_created"`
	// DuplicatesRemoved is the number of duplicate task memories removed.
	DuplicatesRemoved int `json:"duplicates_removed"`
	// Expired is the number of memories expired due to access-based expiration.
	Expired int `json:"expired"`
	// Duration is how long consolidation took.
	Duration time.Duration `json:"duration"`
	// Error is any error that occurred.
	Error string `json:"error,omitempty"`
}

// MetadataJSON converts metadata to a JSON string.
func (m *Memory) MetadataJSON() string {
	if m.Metadata == nil {
		return "{}"
	}
	data, err := json.Marshal(m.Metadata)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ParseMetadata parses a JSON string into metadata.
func ParseMetadata(jsonStr string) map[string]any {
	if jsonStr == "" || jsonStr == "{}" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		return nil
	}
	return meta
}

// Backend is the interface that memory storage backends must implement.
type Backend interface {
	// Initialize sets up the backend.
	Initialize() error
	// Store persists a memory and returns its ID.
	Store(content string, category string, metadata map[string]any) (string, error)
	// Search finds memories matching the query.
	Search(query string, limit int) ([]MemoryResult, error)
	// GetRecent retrieves the most recent memories.
	GetRecent(limit int) ([]MemoryResult, error)
	// Delete removes a memory by ID.
	Delete(id string) error
	// DeleteByIDs removes multiple memories by ID.
	DeleteByIDs(ids []string) (int, error)
	// Count returns the total number of memories.
	Count() (int, error)
	// Close releases resources.
	Close() error
}
