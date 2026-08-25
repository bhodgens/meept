package models

// Key name constants used for keyboard event matching.
const (
	KeyTab   = "tab"
	KeyEsc   = "esc"
	KeyEnter = "enter"
	KeyDown  = "down"
	KeyUp    = "up"
	KeyLeft  = "left"
	KeyRight = "right"
)

// UI state constants shared across TUI models.
const (
	StateCompleted  = "completed"
	StateFailed     = "failed"
	StatePending    = "pending"
	StateProcessing = "processing"
	StateExecuting  = "executing"
	StateRunning    = "running"
	StateNormal     = "normal"
	RoleUser        = "user"
	RoleAssistant   = "assistant"
	RoleSystem      = "system"
	RoleParticipant = "participant"
	RolePending     = "pending"
)

// Color constants for TUI rendering were removed by unified theming (L4):
// colors now come from the shared palette via models' palette_bridge.go
// (paletteColor/paletteHex) and theme/tokens.json5.

// Status text constants.
const (
	StatusNA = "n/a"
)

// Table column title constants.
const (
	ColState = "state"
)

// Task step state constants.
const (
	StateReady     = "ready"
	StateReviewing = "reviewing"
	StateApproved  = "approved"
	StateRejected  = "rejected"
)
