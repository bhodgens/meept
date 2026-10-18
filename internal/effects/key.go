package effects

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// EffectKey derives a stable, deterministic key: sha256 hex over the UTF-8
// bytes of strings.Join(normalized(parts), "\x1f") where normalized(p) =
// strings.TrimSpace(p). 64 lowercase hex chars. The \x1f (unit separator)
// join makes ("ab","c") != ("a","bc"). Order matters: callers pass parts in
// a fixed documented order per tool:
//
//	EffectKey(toolName, sessionID, stepOrRoundKey, argsHashOrIDs...)
//
// Empty parts are allowed and simply contribute an empty field. The function
// is pure: it never allocates randomness and never reads the clock, so the
// same parts produce the same key across processes and restarts.
func EffectKey(parts ...string) string {
	normalized := make([]string, len(parts))
	for i, p := range parts {
		normalized[i] = strings.TrimSpace(p)
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// GitPushKey constructs the effect key for a git push operation.
// It takes the remote path and the commit SHA, omitting sessionID since
// the same-SHA push is idempotent. This preserves the exact key composition
// used by internal/backup/git_ops.go:270.
func GitPushKey(remotePath, headSHA string) string {
	return EffectKey("backup.git_push", remotePath, headSHA)
}

// PushNotifyKey constructs the effect key for a push notification.
// It takes the marshaled session IDs JSON, source, type, and content,
// preserving the exact key composition used by internal/services/push_service.go:168.
func PushNotifyKey(sessionsJSON, source, typ, content string) string {
	return EffectKey("push.notify", sessionsJSON, source, typ, content)
}

// New effect types must add a named constructor here to enforce the
// documented key convention and ensure existing ledgers keep matching.