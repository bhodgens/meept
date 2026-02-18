// Package types provides shared types for the TUI package.
package types

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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
	ID             string                 `json:"id"`
	Content        string                 `json:"content"`
	MemoryType     string                 `json:"memory_type"`
	Type           string                 `json:"type"`
	Category       string                 `json:"category"`
	RelevanceScore float64                `json:"relevance_score"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
	Metadata       map[string]interface{} `json:"metadata"`
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
	ConversationID  string   `json:"conversation_id"`
	CreatedAt       string   `json:"created_at"`
	LastActivity    string   `json:"last_activity"`
	AttachedClients []string `json:"attached_clients"`
	WorkerIDs       []string `json:"worker_ids,omitempty"`
}

// SessionListResponse represents the session list RPC response.
type SessionListResponse struct {
	Sessions []Session `json:"sessions"`
}
