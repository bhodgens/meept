package transport

import (
	"strings"
)

// secretEnvPlaceholderPrefix is the placeholder prefix children receive for
// declared secrets. Must stay identical to internal/secrets.PlaceholderPrefix
// and internal/runtime.SecretPlaceholderPrefix ("MEEPT_SECRET:") so that
// BuildChildEnv's placeholder passthrough recognizes these values. Declared
// literally here to keep this wiring dependency-free.
const secretEnvPlaceholderPrefix = "MEEPT_SECRET:"

// secretEnvPattern marks a configured env value as a declared-secret
// reference: "${secret:<name>}".
const secretEnvPattern = "${secret:"

// substituteSecretEnvValue maps one configured MCP env value to what actually
// enters the subprocess environment:
//
//	"${secret:name}" -> "MEEPT_SECRET:name"   (placeholder, never plaintext)
//	anything else    -> unchanged
//
// Only the exact ${secret:...} form is rewritten; other ${VAR} references are
// passed through untouched (they are resolved by the child's own shell or
// inherited environment, matching existing catalog behavior).
func substituteSecretEnvValue(value string) string {
	if !strings.HasPrefix(value, secretEnvPattern) || !strings.HasSuffix(value, "}") {
		return value
	}

	name := value[len(secretEnvPattern) : len(value)-1]
	if name == "" {
		return value // "${secret:}" is malformed; passthrough
	}
	return secretEnvPlaceholderPrefix + name
}
