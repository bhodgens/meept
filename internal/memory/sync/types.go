// Package sync provides distributed memory synchronization between local SQLite
// and shared memvid storage. It implements a 2-tier architecture where:
// - Tier 1 (Local): SQLite + KnowledgeGraph for fast local queries
// - Tier 2 (Shared): Memvid for cross-agent knowledge sharing
package sync

import (
	"time"
)

// EdgeRef represents a graph edge reference for serialization in memvid metadata.
// Edges are serialized as part of memory metadata to survive the sync process.
type EdgeRef struct {
	ID       string  `json:"id"`
	TargetID string  `json:"target_id,omitempty"` // For outgoing edges
	SourceID string  `json:"source_id,omitempty"` // For incoming edges
	Type     string  `json:"type"`
	Weight   float64 `json:"weight"`
}

// DistilledMemory represents a memory that has been distilled for promotion to shared storage.
type DistilledMemory struct {
	// ID is the local memory ID
	ID string `json:"id"`
	// Content is the memory content
	Content string `json:"content"`
	// Type is the memory type (episodic, task, etc.)
	Type string `json:"type"`
	// Category is the finer-grained label
	Category string `json:"category"`
	// Metadata is the original metadata plus sync metadata
	Metadata map[string]any `json:"metadata"`
	// PageRank is the importance score from the knowledge graph
	PageRank float64 `json:"page_rank"`
	// PromotionReason describes why this memory was selected
	PromotionReason string `json:"promotion_reason"`
	// EdgesOut are outgoing edges from this memory
	EdgesOut []EdgeRef `json:"edges_out,omitempty"`
	// EdgesIn are incoming edges to this memory
	EdgesIn []EdgeRef `json:"edges_in,omitempty"`
	// AgentID identifies which agent created this memory
	AgentID string `json:"agent_id,omitempty"`
	// TaskID is the task this memory was created during
	TaskID string `json:"task_id,omitempty"`
	// CreatedAt is when the memory was originally created
	CreatedAt time.Time `json:"created_at"`
	// DistilledAt is when the memory was distilled
	DistilledAt time.Time `json:"distilled_at"`
}

// HydrationRequest specifies what memories to hydrate from shared storage.
type HydrationRequest struct {
	// JobID is the job being processed
	JobID string `json:"job_id"`
	// TaskID is the associated task (optional)
	TaskID string `json:"task_id,omitempty"`
	// ContextQuery is a query to find relevant memories
	ContextQuery string `json:"context_query,omitempty"`
	// MemoryRefs are explicit memory IDs to fetch
	MemoryRefs []string `json:"memory_refs,omitempty"`
	// Limit is the maximum number of memories to hydrate
	Limit int `json:"limit"`
}

// HydrationResult contains the results of a hydration operation.
type HydrationResult struct {
	// MemoriesHydrated is the number of memories copied to local storage
	MemoriesHydrated int `json:"memories_hydrated"`
	// EdgesRestored is the number of graph edges reconstructed
	EdgesRestored int `json:"edges_restored"`
	// Duration is how long the hydration took
	Duration time.Duration `json:"duration"`
	// FromCache indicates if results came from cache
	FromCache bool `json:"from_cache"`
	// Error is any error that occurred (partial success possible)
	Error string `json:"error,omitempty"`
}

// DistillationResult contains the results of a distillation operation.
type DistillationResult struct {
	// MemoriesEvaluated is the number of memories considered
	MemoriesEvaluated int `json:"memories_evaluated"`
	// MemoriesPromoted is the number of memories promoted to shared storage
	MemoriesPromoted int `json:"memories_promoted"`
	// EdgesPreserved is the number of edges serialized in metadata
	EdgesPreserved int `json:"edges_preserved"`
	// Duration is how long the distillation took
	Duration time.Duration `json:"duration"`
	// Error is any error that occurred (partial success possible)
	Error string `json:"error,omitempty"`
	// Failures contains IDs of memories that failed to promote
	Failures []string `json:"failures,omitempty"`
}

// SyncStatus represents the current state of the sync system.
type SyncStatus struct {
	// Enabled indicates if distributed sync is enabled
	Enabled bool `json:"enabled"`
	// Mode is the current mode ("local" or "distributed")
	Mode string `json:"mode"`
	// MemvidAvailable indicates if memvid service is reachable
	MemvidAvailable bool `json:"memvid_available"`
	// LastHydration is the timestamp of the last hydration
	LastHydration *time.Time `json:"last_hydration,omitempty"`
	// LastDistillation is the timestamp of the last distillation
	LastDistillation *time.Time `json:"last_distillation,omitempty"`
	// PendingRetries is the number of failed operations awaiting retry
	PendingRetries int `json:"pending_retries"`
	// TotalHydrations is the count of successful hydrations
	TotalHydrations int64 `json:"total_hydrations"`
	// TotalDistillations is the count of successful distillations
	TotalDistillations int64 `json:"total_distillations"`
}

// RetryItem represents a failed operation queued for retry.
type RetryItem struct {
	// ID is a unique identifier for this retry item
	ID string `json:"id"`
	// Operation is the type of operation ("hydrate" or "distill")
	Operation string `json:"operation"`
	// MemoryID is the memory being operated on (for distill single memory)
	MemoryID string `json:"memory_id,omitempty"`
	// TaskID is the task associated with the operation (for distill replay)
	TaskID string `json:"task_id,omitempty"`
	// AgentID is the agent associated with the operation (for distill replay)
	AgentID string `json:"agent_id,omitempty"`
	// Request is the original request (for hydrate)
	Request *HydrationRequest `json:"request,omitempty"`
	// Memory is the distilled memory (for distill single memory)
	Memory *DistilledMemory `json:"memory,omitempty"`
	// Attempts is the number of retry attempts
	Attempts int `json:"attempts"`
	// LastAttempt is when the last attempt was made
	LastAttempt time.Time `json:"last_attempt"`
	// NextAttempt is when the next attempt should be made
	NextAttempt time.Time `json:"next_attempt"`
	// Error is the last error encountered
	Error string `json:"error"`
}

// PromotionCandidate represents a memory being considered for promotion.
type PromotionCandidate struct {
	// MemoryID is the local memory ID
	MemoryID string `json:"memory_id"`
	// PageRank is the importance score
	PageRank float64 `json:"page_rank"`
	// InDegree is the number of incoming edges
	InDegree int `json:"in_degree"`
	// OutDegree is the number of outgoing edges
	OutDegree int `json:"out_degree"`
	// TaskID is the associated task
	TaskID string `json:"task_id,omitempty"`
	// AgentID is the creating agent
	AgentID string `json:"agent_id,omitempty"`
	// Score is the computed promotion score
	Score float64 `json:"score"`
	// Reason is why this memory is a candidate
	Reason string `json:"reason"`
}
