package tui

// Key name constants used for keyboard event matching.
const (
	KeyEsc   = "esc"
	KeyEnter = "enter"
	KeyDown  = "down"
	KeyUp    = "up"
	KeyLeft  = "left"
	KeyRight = "right"
	KeyTab   = "tab"
)

// UI state constants shared across TUI models.
const (
	StateCompleted  = "completed"
	StateFailed     = "failed"
	StatePending    = "pending"
	StateRunning    = "running"
	StateProcessing = "processing"
	StateExecuting  = "executing"
	StateNormal     = "normal"
	RoleUser        = "user"
	RoleAssistant   = "assistant"
)

// Color constants for TUI rendering.
const (
	ColorAmber = "#F59E0B"
	ColorGreen = "#10B981"
	ColorRed   = "#EF4444"
	ColorGray  = "#6B7280"
)

// Map key constants used in RPC/API parameter maps.
const (
	ParamSessionID      = "session_id"
	ParamTaskID         = "task_id"
	ParamConversationID = "conversation_id"
	ParamLimit          = "limit"
	ParamName           = "name"
	ParamMessage        = "message"
	ParamState          = "state"
	ParamDescription    = "description"
	ParamClientID       = "client_id"
	ParamCount          = "count"
)

// Status text constants.
const (
	StatusNA = "n/a"
)

// Not connected error message.
const (
	ErrNotConnected = "not connected to daemon"
)

// Event name constants.
const (
	EventTaskFailed = "task.failed"
)
