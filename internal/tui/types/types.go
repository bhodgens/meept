// Package types provides shared types for the TUI package.
package types

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// DaemonStatusResponse represents the status RPC response.
type DaemonStatusResponse struct {
	Status            string   `json:"status"`
	UptimeSeconds     float64  `json:"uptime_seconds"`
	Model             string   `json:"model"`
	DefaultModel      string   `json:"default_model"`
	RegisteredMethods []string `json:"registered_methods"`
	BusSubscribers    int      `json:"bus_subscribers"`
	TokensUsed        int      `json:"tokens_used"`
	TokensRemaining   int      `json:"tokens_remaining"`
	BudgetUsed        float64  `json:"budget_used"`
	BudgetRemaining   float64  `json:"budget_remaining"`
	DailyCostUsed     float64  `json:"daily_cost_used"`
	DailyCostLimit    float64  `json:"daily_cost_limit"`
	HourlyCostUsed    float64  `json:"hourly_cost_used"`
	HourlyCostLimit   float64  `json:"hourly_cost_limit"`
}

// JobListResponse represents the job list RPC response.
type JobListResponse struct {
	Jobs []Job `json:"jobs"`
}

// Job represents a scheduled job.
type Job struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	Trigger     string `json:"trigger"`
	NextRunTime string `json:"next_run_time"`
	LastResult  string `json:"last_result"`
	Paused      bool   `json:"paused"`
	Action      string `json:"action"`
}

// MemoryQueryResponse represents the memory query RPC response.
type MemoryQueryResponse struct {
	Results  []MemoryItem `json:"results"`
	Items    []MemoryItem `json:"items"`
	Memories []MemoryItem `json:"memories"`
}

// GetItems returns the memory items from whichever field is populated.
func (r *MemoryQueryResponse) GetItems() []MemoryItem {
	if len(r.Results) > 0 {
		return r.Results
	}
	if len(r.Items) > 0 {
		return r.Items
	}
	return r.Memories
}

// MemoryItem represents a single memory item.
type MemoryItem struct {
	ID             string         `json:"id"`
	Content        string         `json:"content"`
	MemoryType     string         `json:"memory_type"`
	Type           string         `json:"type"`
	Category       string         `json:"category"`
	RelevanceScore float64        `json:"relevance_score"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	Metadata       map[string]any `json:"metadata"`
}

// GetType returns the memory type from whichever field is populated.
func (m *MemoryItem) GetType() string {
	if m.MemoryType != "" {
		return m.MemoryType
	}
	return m.Type
}

// FormatUptime formats seconds into a human-readable uptime string.
func FormatUptime(seconds float64) string {
	if seconds < 0 {
		return "n/a"
	}

	total := int(seconds)
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60
	secs := total % 60

	if days > 0 {
		return lipgloss.NewStyle().Render(
			lipgloss.JoinHorizontal(lipgloss.Left,
				formatTimeUnit(days, "d"),
				formatTimeUnit(hours, "h"),
				formatTimeUnit(minutes, "m"),
				formatTimeUnit(secs, "s"),
			),
		)
	}
	if hours > 0 {
		return lipgloss.JoinHorizontal(lipgloss.Left,
			formatTimeUnit(hours, "h"),
			formatTimeUnit(minutes, "m"),
			formatTimeUnit(secs, "s"),
		)
	}
	if minutes > 0 {
		return lipgloss.JoinHorizontal(lipgloss.Left,
			formatTimeUnit(minutes, "m"),
			formatTimeUnit(secs, "s"),
		)
	}
	// Always show seconds, even if 0
	return lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%02d", secs)) + "s"
}

func formatTimeUnit(value int, unit string) string {
	if value == 0 {
		return ""
	}
	return lipgloss.NewStyle().Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			//nolint:gosec // value bounded by upstream
			lipgloss.NewStyle().Bold(true).Render(string(rune('0'+value/10))+string(rune('0'+value%10))),
			unit+" ",
		),
	)
}

// TruncateString truncates a string to the given length with ellipsis.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// WorkerListResponse represents the agent workers list response.
type WorkerListResponse struct {
	Workers []Worker `json:"workers"`
	Count   int      `json:"count"`
}

// Worker represents an active agent worker.
type Worker struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id"`
	State          string `json:"state"` // "processing", "executing_tool", "completed", "error"
	StartTime      string `json:"start_time"`
	LastActivity   string `json:"last_activity"`
	CurrentTool    string `json:"current_tool,omitempty"`
}

// Session represents a conversation session that can be shared by multiple clients.
type Session struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	ConversationID  string   `json:"conversation_id"`
	CreatedAt       string   `json:"created_at"`
	LastActivity    string   `json:"last_activity"`
	AttachedClients []string `json:"attached_clients"`
	WorkerIDs       []string `json:"worker_ids,omitempty"`
	LeafMessageID   *int64   `json:"leaf_message_id,omitempty"`
}

// GenerateDescriptionResult is the result of LLM-based session description generation.
type GenerateDescriptionResult struct {
	SessionID   string `json:"session_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// SessionMessage represents a chat message persisted on the server.
type SessionMessage struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// SessionMessagesResponse represents the response from getting session messages.
type SessionMessagesResponse struct {
	Messages []SessionMessage `json:"messages"`
	Total    int              `json:"total"`
}

// SessionListResponse represents the session list RPC response.
type SessionListResponse struct {
	Sessions []Session `json:"sessions"`
}

// Task represents a background task that can spawn multiple jobs.
type Task struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	ProjectDir     string   `json:"project_dir,omitempty"`
	WorkspaceDir   string   `json:"workspace_dir,omitempty"`
	State          string   `json:"state"` // pending, planning, executing, testing, completed, failed, cancelled
	GitRepo        string   `json:"git_repo,omitempty"`
	MemvidZone     string   `json:"memvid_zone,omitempty"`
	TotalJobs      int      `json:"total_jobs"`
	CompletedJobs  int      `json:"completed_jobs"`
	FailedJobs     int      `json:"failed_jobs"`
	LinkedSessions []string `json:"linked_sessions,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	TokenUsage     int      `json:"token_usage,omitempty"`
}

// Progress returns the task completion percentage.
func (t *Task) Progress() float64 {
	if t.TotalJobs == 0 {
		return 0
	}
	return float64(t.CompletedJobs) / float64(t.TotalJobs) * 100
}

// TaskListResponse represents the task list RPC response.
type TaskListResponse struct {
	Tasks []Task `json:"tasks"`
}

// QueueJob represents a job in the queue.
type QueueJob struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id,omitempty"`
	Type         string   `json:"type"`     // one_off, project_task
	Priority     int      `json:"priority"` // 1=low, 2=normal, 3=high, 4=urgent
	State        string   `json:"state"`    // pending, claimed, processing, completed, failed, dead
	RequiredCaps []string `json:"required_caps,omitempty"`
	MaxRetries   int      `json:"max_retries"`
	RetryCount   int      `json:"retry_count"`
	ClaimedBy    string   `json:"claimed_by,omitempty"`
	Error        string   `json:"error,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// QueueStats represents queue statistics.
type QueueStats struct {
	Pending    int `json:"pending"`
	Claimed    int `json:"claimed"`
	Processing int `json:"processing"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Dead       int `json:"dead"`
}

// QueueStatsResponse represents the queue stats RPC response.
type QueueStatsResponse struct {
	ByState    map[string]int `json:"by_state"`
	ByPriority map[string]int `json:"by_priority"`
	DeadCount  int            `json:"dead_count"`
}

// StopSessionResponse represents the response from stopping a session.
type StopSessionResponse struct {
	Status         string   `json:"status"`
	SessionID      string   `json:"session_id"`
	WorkersStopped []string `json:"workers_stopped"`
}

// QueueJobListResponse represents the queue job list RPC response.
type QueueJobListResponse struct {
	Jobs []QueueJob `json:"jobs"`
}

// PoolWorker represents a worker in the worker pool.
type PoolWorker struct {
	ID           string   `json:"id"`
	State        string   `json:"state"` // idle, claiming, processing, complete, error, stopping, stopped
	Capabilities []string `json:"capabilities"`
	StartTime    string   `json:"start_time"`
	LastActive   string   `json:"last_active"`
	JobsComplete int      `json:"jobs_complete"`
	JobsFailed   int      `json:"jobs_failed"`
	CurrentJobID string   `json:"current_job_id,omitempty"`
}

// WorkerPoolStats represents worker pool statistics.
type WorkerPoolStats struct {
	TotalWorkers int          `json:"total_workers"`
	IdleWorkers  int          `json:"idle_workers"`
	BusyWorkers  int          `json:"busy_workers"`
	ErrorWorkers int          `json:"error_workers"`
	WorkerStats  []PoolWorker `json:"worker_stats"`
}

// WorkerPoolResponse represents the worker pool RPC response.
type WorkerPoolResponse struct {
	Workers []PoolWorker    `json:"workers"`
	Stats   WorkerPoolStats `json:"stats"`
}

// AgentActivity represents real-time agent execution state.
type AgentActivity struct {
	AgentID      string     `json:"agent_id"`
	AgentName    string     `json:"agent_name"`
	Role         string     `json:"role"` // dispatcher, executor, reviewer
	Iteration    int        `json:"iteration"`
	MaxIter      int        `json:"max_iterations"`
	ToolCalls    []ToolCall `json:"tool_calls"`
	MemoryRefs   int        `json:"memory_refs"`
	Inherited    int        `json:"inherited_memories"`
	State        string     `json:"state"` // reasoning, tool_exec, waiting, completed
	TaskID       string     `json:"task_id,omitempty"`
	SessionID    string     `json:"session_id,omitempty"`
	StartedAt    string     `json:"started_at"`
	LastActivity string     `json:"last_activity"`
}

// ToolCall represents a single tool invocation.
type ToolCall struct {
	Name      string `json:"name"`
	Args      string `json:"args"`   // Truncated for display
	State     string `json:"state"`  // pending, running, done, error
	Result    string `json:"result"` // Truncated
	StartedAt string `json:"started_at,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// AgentActivityResponse represents the agent activity RPC response.
type AgentActivityResponse struct {
	Activities []AgentActivity `json:"activities"`
}

// BusEvent represents a message bus event for real-time updates.
type BusEvent struct {
	Topic     string `json:"topic"`
	Type      string `json:"type"` // event, request, response
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
	Payload   any    `json:"payload"`
}

// TaskStepView represents a step within a task for TUI display.
type TaskStepView struct {
	ID            string   `json:"id"`
	TaskID        string   `json:"task_id"`
	Description   string   `json:"description"`
	AgentID       string   `json:"agent_id,omitempty"`
	State         string   `json:"state"`
	Result        string   `json:"result,omitempty"`
	Sequence      int      `json:"sequence"`
	DependsOn     []string `json:"depends_on,omitempty"`
	JobID         string   `json:"job_id,omitempty"`
	ToolHint      string   `json:"tool_hint,omitempty"`
	RevisionCount int      `json:"revision_count,omitempty"`
}

// TaskExtended represents a task with memory context fields.
type TaskExtended struct {
	Task

	// Memory context for agent continuity
	MemoryRefs      []string `json:"memory_refs,omitempty"`
	ContextQuery    string   `json:"context_query,omitempty"`
	InheritedFrom   string   `json:"inherited_from,omitempty"`
	CreatedMemories []string `json:"created_memories,omitempty"`
	AssignedAgent   string   `json:"assigned_agent,omitempty"`

	// Steps for orchestrator tasks
	Steps []TaskStepView `json:"steps,omitempty"`

	// Stats for enhanced task detail view
	ModelsUsed []string `json:"models_used,omitempty"`
	ErrorCount int      `json:"error_count,omitempty"`
	ChildTasks []Task   `json:"child_tasks,omitempty"` // Active child tasks with progress
}

// TaskExtendedListResponse represents the extended task list response.
type TaskExtendedListResponse struct {
	Tasks []TaskExtended `json:"tasks"`
}

// TaskStepsResponse represents the response from the task.steps RPC method.
type TaskStepsResponse struct {
	Steps []TaskStepView `json:"steps"`
}

// CacheStatsResponse represents the token cache statistics RPC response.
type CacheStatsResponse struct {
	L1Entries int     `json:"l1_entries"`
	L1Hits    int     `json:"l1_hits"`
	L1Misses  int     `json:"l1_misses"`
	Evictions int     `json:"evictions"`
	L2Entries int     `json:"l2_entries"`
	L2Hits    int     `json:"l2_hits"`
	L2Misses  int     `json:"l2_misses"`
	TotalHits int     `json:"total_hits"`
	HitRate   float64 `json:"hit_rate"`
}

// CacheInspectEntry represents a single cache entry returned by cache.inspect.
type CacheInspectEntry struct {
	PromptHash string            `json:"prompt_hash"`
	ModelID    string            `json:"model_id"`
	Response   string            `json:"response"`
	CreatedAt  string            `json:"created_at"`
	ExpiresAt  string            `json:"expires_at"`
	HitCount   int               `json:"hit_count"`
	FileHashes map[string]string `json:"file_hashes"`
	Source     string            `json:"source"`
}

// CacheInspectResponse represents the response from the cache.inspect RPC method.
type CacheInspectResponse struct {
	Found   bool                `json:"found"`
	Count   int                 `json:"count"`
	Entries []CacheInspectEntry `json:"entries"`
}

// QueueStatusResponse represents the steering/follow-up queue state for a conversation.
type QueueStatusResponse struct {
	SteeringDepth int    `json:"steering_depth"`
	FollowUpDepth int    `json:"followup_depth"`
	IsActive      bool   `json:"is_active"`
	Generation    uint64 `json:"generation"`
}

// BranchInfo represents a conversation branch (shared between TUI and CLI).
type BranchInfo struct {
	ID           string `json:"id"`
	LeafID       int64  `json:"leaf_id"`
	MessageCount int    `json:"message_count"`
	Summary      string `json:"summary,omitempty"`
}

// TreeNodeInfo represents a node in the conversation tree (shared between TUI and CLI).
type TreeNodeInfo struct {
	ID        int64  `json:"id"`
	ParentID  int64  `json:"parent_id"`
	Role      string `json:"role"`
	EntryType string `json:"entry_type"`
	BranchID  string `json:"branch_id"`
	Content   string `json:"content,omitempty"`
	Timestamp string `json:"timestamp"`
	IsLeaf    bool   `json:"is_leaf"`
}

// ProjectInfo represents a registered project for TUI display.
type ProjectInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Mode      string `json:"mode"`
	Branch    string `json:"branch"`
	GitURL    string `json:"git_url,omitempty"`
	LocalPath string `json:"local_path"`
	Status    string `json:"status"`
	Dirty     bool   `json:"dirty"`
}

// ProjectListResponse represents the response from project.list RPC.
type ProjectListResponse struct {
	Projects []ProjectInfo `json:"projects"`
	Count    int           `json:"count"`
}

// ProjectStatusResponse represents the response from project.status RPC.
type ProjectStatusResponse struct {
	Branch        string `json:"branch"`
	Dirty         bool   `json:"dirty"`
	Ahead         int    `json:"ahead"`
	Behind        int    `json:"behind"`
	ModifiedFiles int    `json:"modified_files"`
}

// PlanExtended represents a plan with phase details for TUI display.
type PlanExtended struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Description    string          `json:"description,omitempty"`
	FilePath       string          `json:"file_path"`
	ProjectID      string          `json:"project_id,omitempty"`
	State          string          `json:"state"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	SourceSession  string          `json:"source_session,omitempty"`
	TaskID         string          `json:"task_id,omitempty"`
	RevisionCount  int             `json:"revision_count,omitempty"`
	Phases         []PlanPhaseView `json:"phases,omitempty"`
	TotalSteps     int             `json:"total_steps"`
	CompletedSteps int             `json:"completed_steps"`
	FailedSteps    int             `json:"failed_steps"`
}

// PlanPhaseView represents a single phase within a plan for TUI display.
type PlanPhaseView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Sequence       int    `json:"sequence"`
	TotalSteps     int    `json:"total_steps"`
	CompletedSteps int    `json:"completed_steps"`
	FailedSteps    int    `json:"failed_steps"`
	State          string `json:"state"`
}

// PlanListResponse represents the plan list RPC response.
type PlanListResponse struct {
	Plans []PlanExtended `json:"plans"`
	Err   string         `json:"err,omitempty"`
}

// PlanStateCounts holds counts of plans by state.
type PlanStateCounts struct {
	Planning        int `json:"planning"`
	Draft           int `json:"draft"`
	PendingApproval int `json:"pending_approval"`
	Approved        int `json:"approved"`
	Executing       int `json:"executing"`
	Completed       int `json:"completed"`
	Confirmed       int `json:"confirmed"`
	Failed          int `json:"failed"`
	Cancelled       int `json:"cancelled"`
}
