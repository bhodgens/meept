package effects

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestEffectKey(t *testing.T) {
	expected := func(parts ...string) string {
		sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
		return hex.EncodeToString(sum[:])
	}

	t.Run("stable across repeated calls", func(t *testing.T) {
		k1 := EffectKey("backup.git_push", "sess-1", "step-2", "abc123")
		k2 := EffectKey("backup.git_push", "sess-1", "step-2", "abc123")
		if k1 != k2 {
			t.Fatalf("keys differ for identical parts: %q vs %q", k1, k2)
		}
		if k1 != expected("backup.git_push", "sess-1", "step-2", "abc123") {
			t.Fatalf("key does not match sha256 of \\x1f-joined parts: %q", k1)
		}
	})

	t.Run("separator prevents part-boundary collision", func(t *testing.T) {
		k1 := EffectKey("ab", "c")
		k2 := EffectKey("a", "bc")
		if k1 == k2 {
			t.Fatalf(`EffectKey("ab","c") == EffectKey("a","bc"); \x1f join not applied`)
		}
		if k1 != expected("ab", "c") {
			t.Fatalf("key for (ab,c) mismatch: %q", k1)
		}
	})

	t.Run("trim normalization", func(t *testing.T) {
		k1 := EffectKey(" tool ", "sess")
		k2 := EffectKey("tool", "sess")
		if k1 != k2 {
			t.Fatalf("untrimmed and trimmed parts produced different keys: %q vs %q", k1, k2)
		}
		if k1 != expected("tool", "sess") {
			t.Fatalf("trimmed key mismatch: %q", k1)
		}
	})

	t.Run("empty input is valid sha256 of empty string", func(t *testing.T) {
		k := EffectKey()
		if k != expected() {
			t.Fatalf("empty parts key mismatch: %q", k)
		}
	})

	t.Run("output shape: 64 lowercase hex chars", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			parts []string
		}{
			{"single", []string{"backup.git_push"}},
			{"many", []string{"a", "b", "c", "d", "e"}},
			{"empty", nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				k := EffectKey(tc.parts...)
				if len(k) != 64 {
					t.Fatalf("key length = %d, want 64", len(k))
				}
				if k != strings.ToLower(k) {
					t.Fatalf("key contains uppercase: %q", k)
				}
				if _, err := hex.DecodeString(k); err != nil {
					t.Fatalf("key is not valid hex: %v", err)
				}
			})
		}
	})
}
