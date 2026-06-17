package id

import (
	"crypto/rand"
	"encoding/hex"
)

// Generate creates a random hex ID with the given prefix.
// Uses crypto/rand to ensure uniqueness and unpredictability.
//
// Fallback behavior: if crypto/rand fails (which indicates catastrophic
// system failure — entropy exhaustion, broken /dev/urandom, etc.), Generate
// returns prefix + "0000000000000000" rather than panicking. This fallback
// IS predictable and not unique, but triggering it means the host is
// already in an unrecoverable state where panic would be worse than a
// degraded ID. Callers that need hard uniqueness guarantees should treat a
// zero-suffixed ID as a fatal signal.
func Generate(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback should never happen on supported platforms.
		// If it does, the prefix + "0000000000000000" still works as a key,
		// just not guaranteed unique.
		return prefix + "0000000000000000"
	}
	return prefix + hex.EncodeToString(b)
}
