// Package builtin: session context key shared by file_edit.go and
// filesystem.go staging paths.
package builtin

import "context"

// sessionIDContextKey is a dedicated context-key type for the staging paths'
// session ID. It satisfies staticcheck SA1029 (no builtin string as ctx key).
type sessionIDContextKeyType struct{}

var sessionIDContextKey = sessionIDContextKeyType{}

// ContextWithSessionID stores a session ID under the dedicated typed key.
// Tests use this helper instead of raw WithValue calls. NOTE: production
// readers in file_edit.go:394 / filesystem.go still look up the raw string
// "session_id"; migrate those readers to this typed key together, otherwise
// staged writes lose their session attribution.
func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey, sessionID)
}
