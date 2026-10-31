package compress

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sync"
)

// HashLength is the number of hex characters in a CCR hash.
// 24 chars = 12 bytes = 96 bits of collision resistance.
// This is sufficient for ~1 billion entries with negligible collision risk.
const HashLength = 24

// hashPool provides reusable hash instances to reduce allocations.
var hashPool = sync.Pool{
	New: func() any {
		return sha256.New()
	},
}

// ContentHash computes a content-addressed hash for the given content.
// Returns a hex-encoded string of HashLength characters.
//
// The hash is:
// - Deterministic: same content always produces same hash
// - Collision-resistant: different content almost never produces same hash
// - Truncated: 24 chars (96 bits) for brevity while maintaining safety
func ContentHash(content string) string {
	// Get a reusable hash instance
	h := hashPool.Get().(hash.Hash)
	h.Reset()
	defer hashPool.Put(h)

	// Hash the content
	h.Write([]byte(content))
	sum := h.Sum(nil)

	// Truncate to 12 bytes (24 hex chars)
	return hex.EncodeToString(sum[:12])
}

// MarkerFormat returns the standard CCR marker string for a hash.
// Format: <<ccr:HASH>>
func MarkerFormat(hash string) string {
	return fmt.Sprintf("<<ccr:%s>>", hash)
}

// ParseMarker extracts the hash from a CCR marker.
// Returns empty string if the marker is invalid.
func ParseMarker(marker string) string {
	// Expected format: <<ccr:HASH>>
	if len(marker) < 10 {
		return ""
	}
	if marker[:6] != "<<ccr:" || marker[len(marker)-2:] != ">>" {
		return ""
	}
	hash := marker[6 : len(marker)-2]
	if len(hash) != HashLength {
		return ""
	}
	return hash
}

// VerboseMarkerFormat returns a verbose marker with metadata.
// Format: [N items compressed to X tokens, hash=HASH]
func VerboseMarkerFormat(itemCount, tokenCount int, hash string) string {
	return fmt.Sprintf("[%d items compressed to %d tokens, hash=%s]", itemCount, tokenCount, hash)
}




// findSubstring finds the index of substr in s, or -1 if not found.
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
