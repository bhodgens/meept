package debug

// SessionMode describes how a debug session was started.
type SessionMode string

const (
	// SessionModeLaunch indicates the session was created via a DAP launch request.
	SessionModeLaunch SessionMode = "launch"
	// SessionModeAttach indicates the session was created via a DAP attach request.
	SessionModeAttach SessionMode = "attach"
	// SessionModeCore indicates the session was created by loading a core dump.
	SessionModeCore SessionMode = "core"
)
