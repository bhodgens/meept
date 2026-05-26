package task

import (
	"encoding/json"
	"fmt"
	"time"
)

// AmendmentType represents the type of amendment.
type AmendmentType string

const (
	AmendmentInjectContext AmendmentType = "inject_context" // Add context/message to agent
	AmendmentReprioritize  AmendmentType = "reprioritize"   // Change step priorities
	AmendmentSkipStep      AmendmentType = "skip_step"      // Skip a step
	AmendmentAddStep       AmendmentType = "add_step"       // Insert new step
	AmendmentChangeAgent   AmendmentType = "change_agent"   // Reassign step to different agent
	AmendmentCancelTask    AmendmentType = "cancel_task"    // Cancel entire task
)

func (t AmendmentType) String() string {
	return string(t)
}

// AmendmentRequest represents a user's amendment request.
type AmendmentRequest struct {
	ID          string          `json:"id"`
	TaskID      string          `json:"task_id"`
	Type        AmendmentType   `json:"type"`
	StepID      string          `json:"step_id,omitempty"`  // For step-specific amendments
	Content     string          `json:"content"`            // The amendment content
	Metadata    json.RawMessage `json:"metadata,omitempty"` // Type-specific metadata
	Status      AmendmentStatus `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	ProcessedAt time.Time       `json:"processed_at,omitempty,omitzero"` //nolint:modernize // omitzero already applied
	Result      string          `json:"result,omitempty"`                // Result message after processing
}

// AmendmentStatus represents the status of an amendment.
type AmendmentStatus string

const (
	AmendmentPending  AmendmentStatus = "pending"
	AmendmentApplied  AmendmentStatus = "applied"
	AmendmentRejected AmendmentStatus = "rejected"
	AmendmentIgnored  AmendmentStatus = "ignored" // For amendments no longer relevant
)

func (s AmendmentStatus) String() string {
	return string(s)
}

// AmendmentReply is the response to an amendment request.
type AmendmentReply struct {
	RequestID string          `json:"request_id"`
	Success   bool            `json:"success"`
	Message   string          `json:"message,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// NewAmendmentRequest creates a new amendment request.
func NewAmendmentRequest(taskID string, typ AmendmentType, content string) *AmendmentRequest {
	return &AmendmentRequest{
		ID:        fmt.Sprintf("amend-%s-%d", taskID, time.Now().UnixNano()),
		TaskID:    taskID,
		Type:      typ,
		Content:   content,
		Status:    AmendmentPending,
		CreatedAt: time.Now().UTC(),
	}
}
