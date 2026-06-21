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
	New: func() interface{} {
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

// ParseVerboseMarker extracts hash and metadata from a verbose marker.
// Returns empty string if the marker is invalid.
func ParseVerboseMarker(marker string) (hash string, itemCount, tokenCount int, ok bool) {
	// Expected format: [N items compressed to X tokens, hash=HASH]
	if len(marker) < 30 {
		return "", 0, 0, false
	}
	if marker[0] != '[' || marker[len(marker)-1] != ']' {
		return "", 0, 0, false
	}

	// Find hash= prefix
	hashPrefix := "hash="
	hashIdx := findSubstring(marker, hashPrefix)
	if hashIdx < 0 {
		return "", 0, 0, false
	}

	// Extract hash (everything after "hash=" until "]")
	hash = marker[hashIdx+len(hashPrefix) : len(marker)-1]
	if len(hash) != HashLength {
		return "", 0, 0, false
	}

	// Extract item count (first number after "[")
	itemCount = extractNumber(marker[1:hashIdx], "items")
	tokenCount = extractNumber(marker[1:hashIdx], "tokens")

	return hash, itemCount, tokenCount, true
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

// extractNumber finds a number before a keyword in text like "42 items" or "100 tokens".
func extractNumber(text, keyword string) int {
	idx := findSubstring(text, keyword)
	if idx < 0 {
		return 0
	}

	// Work backwards from the keyword to find the number
	start := idx - 1
	for start >= 0 && (text[start] == ' ' || text[start] == '\t') {
		start--
	}
	end := start
	for end >= 0 && text[end] >= '0' && text[end] <= '9' {
		end--
	}

	if end >= start {
		return 0
	}

	var n int
	for i := end + 1; i <= start; i++ {
		n = n*10 + int(text[i]-'0')
	}
	return n
}
