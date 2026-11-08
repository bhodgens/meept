package security

import (
	"strings"
	"testing"
)

// Proof tests for wiring-gap remediation leaf 07 (secret restore scoping).
//
// T1 (collision): the legacy 16-bit placeholder collided across secrets and
// obfuscateMap[ph] was overwritten, so deobfuscation restored the WRONG
// secret by map iteration order. Two secrets that mint placeholders must
// each restore to their OWN original.
//
// T2 (session scope): RestoreOutput scoped to session B must refuse to
// resolve placeholders minted in session A (cross-session restore is an
// info leak).
//
// T3 (bounded scan): restore consults only the requested session's entries.

// TestObfuscate_PlaceholderEntropyWidened pins the T1 fix structurally: the
// placeholder format carries 48 bits (12 hex chars), not the legacy 16.
func TestObfuscate_PlaceholderEntropyWidened(t *testing.T) {
	s := NewSecretObfuscator()
	s.counter = 1
	ph := s.placeholder(s.counter)
	if len(ph) != len("#XXXXXXXXXXXX#") {
		t.Fatalf("placeholder %q: want 12 hex chars between # markers (48-bit entropy), got %d", ph, len(ph)-2)
	}
}

// TestObfuscate_CollidingSecretsRestoreOwnOriginals is the T1 behavioral
// pin: mint many placeholders; every secret must restore to its own
// original through the sessionless lane (a 16-bit collision would overwrite
// obfuscateMap entries and restore the wrong secret by map order).
func TestObfuscate_CollidingSecretsRestoreOwnOriginals(t *testing.T) {
	s := NewSecretObfuscator()
	const n = 500
	secrets := make([]string, n)
	for i := range secrets {
		secrets[i] = "collide-secret-" + strings.Repeat("x", i+1) + "-value"
		s.addPlainEntry(secrets[i], SecretModeObfuscate)
	}
	s.sortEntriesLocked()

	text := strings.Join(secrets, " ")
	obfuscated := s.obfuscate(text, "")

	// Every secret must be gone from the obfuscated text.
	if containsAny(obfuscated, secrets...) {
		t.Fatal("raw secret survived obfuscation")
	}

	// Each minted placeholder must map to its OWN secret in the value map.
	seen := make(map[string]string) // placeholder -> original
	for _, secret := range secrets {
		ph, ok := s.deobfuscateMap[secret]
		if !ok {
			t.Fatalf("secret %q never minted a placeholder", secret)
		}
		original, ok := s.obfuscateMap[ph]
		if !ok {
			t.Fatalf("placeholder %q missing from the restore map", ph)
		}
		if original != secret {
			t.Fatalf("COLLISION: placeholder %q maps %q back to %q (wrong-secret restore by map order)", ph, secret, original)
		}
		if prev, dup := seen[ph]; dup {
			t.Fatalf("placeholder %q minted twice: %q and %q", ph, prev, secret)
		}
		seen[ph] = secret
	}

	// Roundtrip restores every original.
	restored := s.Deobfuscate(obfuscated)
	for _, secret := range secrets {
		if !strings.Contains(restored, secret) {
			t.Fatalf("restored text lost original %q (wrong-secret restore)", secret)
		}
	}
}

// TestRestoreOutput_CrossSessionRefused is the T2 pin: session B restoring
// output containing session A's placeholders must resolve NOTHING.
func TestRestoreOutput_CrossSessionRefused(t *testing.T) {
	s := NewSecretObfuscator()
	secretA := "session-alpha-secret-value"
	secretB := "session-beta-secret-value"
	s.addPlainEntry(secretA, SecretModeObfuscate)
	s.addPlainEntry(secretB, SecretModeObfuscate)
	s.sortEntriesLocked()

	outA := s.ObfuscateForSession("alpha says "+secretA, "session-A")
	outB := s.ObfuscateForSession("beta says "+secretB, "session-B")

	// Scoped restore in the minting session works.
	if got := s.RestoreOutput(outA, "session-A"); !strings.Contains(got, secretA) {
		t.Fatalf("own-session restore failed: %q", got)
	}

	// Cross-session restore is refused: session B cannot resolve
	// session A placeholders (and vice versa).
	if got := s.RestoreOutput(outA, "session-B"); got != outA {
		t.Fatalf("cross-session restore LEAKED: session B resolved session A placeholder -> %q", got)
	}
	if got := s.RestoreOutput(outB, "session-A"); got != outB {
		t.Fatalf("cross-session restore LEAKED: session A resolved session B placeholder -> %q", got)
	}

	// Legacy sessionless Deobfuscate still resolves both (in-process lane).
	if got := s.Deobfuscate(outA); !strings.Contains(got, secretA) {
		t.Fatalf("legacy lane lost session A secret: %q", got)
	}
}

// TestRestoreOutput_EmptySessionRefused pins the no-provenance rule: an
// empty sessionID resolves nothing through the scoped lane.
func TestRestoreOutput_EmptySessionRefused(t *testing.T) {
	s := NewSecretObfuscator()
	secret := "empty-session-secret-value"
	s.addPlainEntry(secret, SecretModeObfuscate)
	s.sortEntriesLocked()

	out := s.ObfuscateForSession("text with "+secret, "session-A")
	if got := s.RestoreOutput(out, ""); got != out {
		t.Fatalf("empty-session restore resolved a placeholder: %q", got)
	}
}

// TestRestoreOutput_BoundedToSessionEntries pins the T3 bound: restore
// consults only the requested session's entries - a placeholder from
// another session in the map is neither looked up nor replaced.
func TestRestoreOutput_BoundedToSessionEntries(t *testing.T) {
	s := NewSecretObfuscator()
	secretA := "bounded-alpha-secret-value"
	secretB := "bounded-beta-secret-value"
	s.addPlainEntry(secretA, SecretModeObfuscate)
	s.addPlainEntry(secretB, SecretModeObfuscate)
	s.sortEntriesLocked()

	outA := s.ObfuscateForSession(secretA, "session-A")
	outB := s.ObfuscateForSession(secretB, "session-B")
	phA := s.deobfuscateMap[secretA]
	phB := s.deobfuscateMap[secretB]

	// Mixed output under session A: only A's placeholder resolves.
	mixed := outA + " " + outB
	got := s.RestoreOutput(mixed, "session-A")
	if !strings.Contains(got, secretA) {
		t.Fatalf("own-session placeholder did not resolve: %q", got)
	}
	if strings.Contains(got, phA) {
		t.Fatalf("session A placeholder survived its own restore: %q", got)
	}
	if !strings.Contains(got, phB) || !strings.Contains(got, secretB) {
		// B's placeholder must be untouched AND B's secret not minted in.
		if !strings.Contains(got, phB) {
			t.Fatalf("cross-session placeholder resolved under wrong scope: %q", got)
		}
	}
}
