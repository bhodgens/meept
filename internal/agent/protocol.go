package agent

import (
	"encoding/json"
	"time"
)

// MessageType defines the type of inter-agent message.
type MessageType string

const (
	// MessageTypeTask is a task assignment message.
	MessageTypeTask MessageType = "task"
	// MessageTypeResult is a task result message.
	MessageTypeResult MessageType = "result"
	// MessageTypeError is an error notification.
	MessageTypeError MessageType = "error"
	// MessageTypeStatus is a status update.
	MessageTypeStatus MessageType = "status"
)

// ActionType defines the action requested in a task message.
type ActionType string

const (
	// ActionExecute requests task execution.
	ActionExecute ActionType = "execute"
	// ActionDelegate requests delegation to another agent.
	ActionDelegate ActionType = "delegate"
	// ActionReview requests review of work.
	ActionReview ActionType = "review"
	// ActionCancel requests task cancellation.
	ActionCancel ActionType = "cancel"
)

// TaskPriority defines task priority levels.
type TaskPriority string

const (
	PriorityLow    TaskPriority = "low"
	PriorityNormal TaskPriority = "normal"
	PriorityHigh   TaskPriority = "high"
	PriorityUrgent TaskPriority = "urgent"
)

// TaskMessage is the standardized inter-agent communication format.
// This follows JSONL format for streaming and logging.
type TaskMessage struct {
	// Type identifies the message type.
	Type MessageType `json:"type"`

	// ID is a unique message identifier.
	ID string `json:"id"`

	// TaskID references the parent task (for results/errors).
	TaskID string `json:"task_id,omitempty"`

	// From is the source agent ID.
	From string `json:"from"`

	// To is the target agent ID.
	To string `json:"to"`

	// Action specifies what action is requested.
	Action ActionType `json:"action"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`

	// Payload contains action-specific data.
	Payload json.RawMessage `json:"payload"`
}

// NewTaskMessage creates a new task message.
func NewTaskMessage(from, to string, action ActionType, payload any) (*TaskMessage, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &TaskMessage{
		Type:      MessageTypeTask,
		ID:        generateMessageID(),
		From:      from,
		To:        to,
		Action:    action,
		Timestamp: time.Now().UTC(),
		Payload:   payloadJSON,
	}, nil
}

// NewResultMessage creates a result message for a task.
func NewResultMessage(taskID, from string, result *ResultPayload) (*TaskMessage, error) {
	payloadJSON, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	return &TaskMessage{
		Type:      MessageTypeResult,
		ID:        generateMessageID(),
		TaskID:    taskID,
		From:      from,
		To:        "", // Results go back to originator
		Action:    "",
		Timestamp: time.Now().UTC(),
		Payload:   payloadJSON,
	}, nil
}

// TaskPayload is the payload for "execute" and "delegate" actions.
type TaskPayload struct {
	// Description is what needs to be done.
	Description string `json:"description"`

	// MemoryRefs are explicit memory IDs for context.
	MemoryRefs []string `json:"memory_refs,omitempty"`

	// ContextQuery is an auto-search query for additional context.
	ContextQuery string `json:"context_query,omitempty"`

	// InheritedFrom is the parent task ID for subtasks.
	InheritedFrom string `json:"inherited_from,omitempty"`

	// Priority is the task priority.
	Priority TaskPriority `json:"priority,omitempty"`

	// ErrorContext provides error details for debugger handoffs.
	ErrorContext string `json:"error_context,omitempty"`

	// Constraints are agent-specific execution constraints.
	Constraints map[string]any `json:"constraints,omitempty"`

	// MemvidZone specifies the memory zone for this task.
	MemvidZone string `json:"memvid_zone,omitempty"`
}

// ResultPayload is the payload for "result" type messages.
type ResultPayload struct {
	// Summary is a brief description of the outcome.
	Summary string `json:"summary"`

	// Status is the completion status.
	Status string `json:"status"` // "completed", "partial", "failed"

	// CreatedMemories are memory IDs created during execution.
	CreatedMemories []string `json:"created_memories,omitempty"`

	// Artifacts are file paths, URLs, or other outputs.
	Artifacts []string `json:"artifacts,omitempty"`

	// NextSteps are suggested follow-up actions.
	NextSteps []string `json:"next_steps,omitempty"`

	// Error contains error details if status is "failed".
	Error string `json:"error,omitempty"`
}

// ErrorPayload is the payload for "error" type messages.
type ErrorPayload struct {
	// Code is an error code.
	Code string `json:"code"`

	// Message is a human-readable error message.
	Message string `json:"message"`

	// Details contains additional error context.
	Details map[string]any `json:"details,omitempty"`

	// Recoverable indicates if the error can be retried.
	Recoverable bool `json:"recoverable"`
}

// StatusPayload is the payload for "status" type messages.
type StatusPayload struct {
	// Progress is completion percentage (0-100).
	Progress float64 `json:"progress"`

	// Phase is the current execution phase.
	Phase string `json:"phase"`

	// Message is a status message.
	Message string `json:"message"`
}

// GetTaskPayload extracts TaskPayload from a message.
func (m *TaskMessage) GetTaskPayload() (*TaskPayload, error) {
	var payload TaskPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetResultPayload extracts ResultPayload from a message.
func (m *TaskMessage) GetResultPayload() (*ResultPayload, error) {
	var payload ResultPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// GetErrorPayload extracts ErrorPayload from a message.
func (m *TaskMessage) GetErrorPayload() (*ErrorPayload, error) {
	var payload ErrorPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ToJSONL converts the message to a single JSONL line.
func (m *TaskMessage) ToJSONL() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseTaskMessage parses a JSONL line into a TaskMessage.
func ParseTaskMessage(line string) (*TaskMessage, error) {
	var msg TaskMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ChatDelegation is a simple delegation from the chat agent.
// This is NOT in JSONL format - it's a simpler struct for chat-to-dispatcher handoff.
type ChatDelegation struct {
	// Intent is what the user wants to accomplish.
	Intent string `json:"intent"`

	// Urgency is the conversational assessment of urgency.
	Urgency string `json:"urgency"`

	// Context is relevant conversation context.
	Context string `json:"context"`

	// OriginalMessage is the user's original request.
	OriginalMessage string `json:"original_message"`
}

// ToTaskPayload converts a chat delegation to a task payload.
func (d *ChatDelegation) ToTaskPayload() *TaskPayload {
	return &TaskPayload{
		Description:  d.Intent,
		ContextQuery: d.OriginalMessage,
		Priority:     d.urgencyToPriority(),
	}
}

func (d *ChatDelegation) urgencyToPriority() TaskPriority {
	switch d.Urgency {
	case "urgent":
		return PriorityUrgent
	case "high":
		return PriorityHigh
	case "low":
		return PriorityLow
	default:
		return PriorityNormal
	}
}
