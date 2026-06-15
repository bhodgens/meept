package id

import (
	"crypto/rand"
	"encoding/hex"
)

// Generate creates a random hex ID with the given prefix.
// Uses crypto/rand to ensure uniqueness and unpredictability.
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
