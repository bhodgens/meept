package effects

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestNamedConstructors_IndependentHashPin pins the exact key composition of
// GitPushKey / PushNotifyKey against an independently computed sha256 of the
// unit-separator-joined trimmed parts (bughunt 2026-10-15 A7). If either
// constructor's inputs or part order change, existing effect-ledger rows stop
// matching and this test fails. Unlike a constructor-vs-EffectKey comparison,
// the expected value here is computed from the raw parts, so it catches a
// changed separator or a reordered argument.
func TestNamedConstructors_IndependentHashPin(t *testing.T) {
	expected := func(parts ...string) string {
		trimmed := make([]string, len(parts))
		for i, p := range parts {
			trimmed[i] = strings.TrimSpace(p)
		}
		sum := sha256.Sum256([]byte(strings.Join(trimmed, "\x1f")))
		return hex.EncodeToString(sum[:])
	}

	if got, want := GitPushKey("/repo", "abc123"), expected("backup.git_push", "/repo", "abc123"); got != want {
		t.Errorf("GitPushKey composition drift: got %s want %s", got, want)
	}

	sessionsJSON := `["s1","s2"]`
	if got, want := PushNotifyKey(sessionsJSON, "src", "toast", "body"),
		expected("push.notify", sessionsJSON, "src", "toast", "body"); got != want {
		t.Errorf("PushNotifyKey composition drift: got %s want %s", got, want)
	}
}
