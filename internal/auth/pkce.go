package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// randomURLSafe returns n random bytes encoded as unpadded base64 raw URL
// encoding, using crypto/rand only.
func randomURLSafe(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failure means the OS entropy source is broken;
		// continuing with predictable values would be a security hole.
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// GeneratePKCE returns a random 43-char URL-safe verifier (32 bytes,
// RawURLEncoding) and its S256 challenge (base64url SHA-256 of the
// verifier bytes, unpadded).
func GeneratePKCE() (verifier, challenge string) {
	verifier = randomURLSafe(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// GenerateState returns a 32-char random URL-safe state string.
func GenerateState() string {
	return randomURLSafe(24)
}
