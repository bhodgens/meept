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
