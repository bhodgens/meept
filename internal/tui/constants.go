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

// Color constants for TUI rendering were removed by unified theming (L4):
// colors now come from the shared palette — see internal/tui/palette.go
// and theme/tokens.json5.

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

// Command name constants.
const (
	CmdTasks = "tasks"
)

// Task state constants.
const (
	StatePlanning  = "planning"
	StateReady     = "ready"
	StateReviewing = "reviewing"
	StateApproved  = "approved"
	StateRejected  = "rejected"
)
