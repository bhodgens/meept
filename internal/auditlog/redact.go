package auditlog

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/caimlas/meept/internal/security"
)

// secretKeyFragments is a case-insensitive substring blocklist applied to
// payload KEYS (master.md C4).
var secretKeyFragments = []string{
	"token", "secret", "password", "api_key", "apikey",
	"authorization", "credential", "private_key", "bearer", "cookie",
}

// maxPayloadStringBytes caps any single string VALUE in a record payload.
const maxPayloadStringBytes = 4096

// redactMonitor is the shared output monitor used for value-level
// credential redaction (internal/security/sanitizer.go DetectAndRedact).
// The monitor is stateless for our purposes (Scan+redact per call).
var redactMonitor = security.NewOutputMonitor()

// OutputSHA256 is the only channel gate command output may take into a
// record (master.md C4: never raw command output).
func OutputSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SanitizePayload deep-copies p and redacts: (1) secret-fragment keys →
// "[redacted]"; (2) remaining string values scrubbed via
// security.OutputMonitor.DetectAndRedact; (3) string values > 4096 bytes
// truncated with a "...[truncated]" suffix. The input map is never mutated.
func SanitizePayload(p map[string]any) map[string]any {
	out := make(map[string]any, len(p))
	for k, v := range p {
		out[k] = sanitizeValue(k, v)
	}
	return out
}

func isSecretKey(k string) bool {
	lk := strings.ToLower(k)
	for _, frag := range secretKeyFragments {
		if strings.Contains(lk, frag) {
			return true
		}
	}
	return false
}

func sanitizeValue(key string, v any) any {
	if isSecretKey(key) {
		return "[redacted]"
	}
	switch x := v.(type) {
	case map[string]any:
		return SanitizePayload(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			// Array elements carry no key; only value-level rules apply.
			out[i] = sanitizeElement(e)
		}
		return out
	case string:
		s := x
		if redacted, changed := redactMonitor.DetectAndRedact(s); changed {
			s = redacted
		}
		if len(s) > maxPayloadStringBytes {
			s = s[:maxPayloadStringBytes] + "...[truncated]"
		}
		return s
	default:
		return v
	}
}

func sanitizeElement(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return SanitizePayload(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = sanitizeElement(e)
		}
		return out
	case string:
		s := x
		if redacted, changed := redactMonitor.DetectAndRedact(s); changed {
			s = redacted
		}
		if len(s) > maxPayloadStringBytes {
			s = s[:maxPayloadStringBytes] + "...[truncated]"
		}
		return s
	default:
		return v
	}
}
