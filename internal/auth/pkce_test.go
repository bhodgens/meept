package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"
)

var urlSafeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestGeneratePKCEVerifier(t *testing.T) {
	v1, _ := GeneratePKCE()
	v2, _ := GeneratePKCE()

	if len(v1) != 43 {
		t.Errorf("verifier length = %d, want 43", len(v1))
	}
	if !urlSafeRe.MatchString(v1) {
		t.Errorf("verifier %q contains non-URL-safe characters", v1)
	}
	if v1 == v2 {
		t.Errorf("verifier repeated across calls: %q", v1)
	}
}

func TestGeneratePKCEChallenge(t *testing.T) {
	verifier, challenge := GeneratePKCE()

	if len(challenge) != 43 {
		t.Fatalf("challenge length = %d, want 43", len(challenge))
	}
	if !urlSafeRe.MatchString(challenge) {
		t.Errorf("challenge %q contains non-URL-safe characters", challenge)
	}

	// Recompute the challenge independently: base64url(SHA-256(verifier)).
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Errorf("challenge = %q, want independently recomputed %q", challenge, want)
	}
}

func TestGenerateState(t *testing.T) {
	s1 := GenerateState()
	s2 := GenerateState()

	if len(s1) != 32 {
		t.Errorf("state length = %d, want 32", len(s1))
	}
	if !urlSafeRe.MatchString(s1) {
		t.Errorf("state %q contains non-URL-safe characters", s1)
	}
	if s1 == s2 {
		t.Errorf("state repeated across calls: %q", s1)
	}
}
