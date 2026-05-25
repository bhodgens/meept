package builtin

import (
	"fmt"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
)

const (
	// numBigrams is the total number of two-letter lowercase combinations (26*26).
	numBigrams = 676

	// hashLineSep separates hash from content in hashline format.
	hashLineSep = "|"
)

// bpeBigrams is all 676 two-letter lowercase combinations, ordered alphabetically.
var bpeBigrams [numBigrams]string

func init() {
	idx := 0
	for b1 := byte('a'); b1 <= 'z'; b1++ {
		for b2 := byte('a'); b2 <= 'z'; b2++ {
			bpeBigrams[idx] = string([]byte{b1, b2})
			idx++
		}
	}
}

// ComputeLineHash returns a 2-character lowercase bigram hash for the given line.
// Uses xxHash64 on the trimmed line content (trailing whitespace removed).
// Line index is intentionally NOT used so anchors stay stable across sibling edits.
// Identical blank lines intentionally collide; line number disambiguates.
func ComputeLineHash(line string) string {
	trimmed := strings.TrimRight(line, "\r\n \t")
	h := xxhash.Sum64String(trimmed)
	return bpeBigrams[h%uint64(numBigrams)]
}

// FormatHashLine formats a single line as "LINE_NUMBER:HASH|CONTENT".
func FormatHashLine(lineNum int, line string) string {
	return fmt.Sprintf("%d:%s|%s", lineNum, ComputeLineHash(line), line)
}

// FormatHashLines formats multiple lines with hashline tags.
// startLine is 1-based.
func FormatHashLines(lines []string, startLine int) string {
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(FormatHashLine(startLine+i, line))
	}
	return sb.String()
}

// ParseAnchor parses a "LINE_NUMBER:HASH" anchor string and returns the line number and hash.
// Returns (0, "", error) on invalid format.
func ParseAnchor(anchor string) (lineNum int, hash string, err error) {
	parts := strings.SplitN(anchor, ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid anchor format %q: expected LINE:HASH", anchor)
	}

	var n int
	if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
		return 0, "", fmt.Errorf("invalid line number in anchor %q: %w", anchor, err)
	}

	if len(parts[1]) != 2 {
		return 0, "", fmt.Errorf("invalid hash in anchor %q: expected 2-char bigram", anchor)
	}

	return n, parts[1], nil
}

// ValidateAnchor checks if a hashline anchor matches the actual content at the given line.
// lines is 0-indexed (lines[0] is line 1). lineNum is 1-based.
func ValidateAnchor(lines []string, lineNum int, expectedHash string) bool {
	idx := lineNum - 1
	if idx < 0 || idx >= len(lines) {
		return false
	}
	return ComputeLineHash(lines[idx]) == expectedHash
}

// ReadCache is a thread-safe LRU cache for file snapshots used by the edit tool
// for stale-anchor recovery.
type ReadCache struct {
	mu       sync.RWMutex
	entries  map[string]*cacheEntry
	order    []string // LRU order (front = oldest)
	maxItems int
}

type cacheEntry struct {
	lines []string // 0-indexed lines
}

// NewReadCache creates a ReadCache with the given maximum entries.
func NewReadCache(maxItems int) *ReadCache {
	if maxItems <= 0 {
		maxItems = 30
	}
	return &ReadCache{
		entries:  make(map[string]*cacheEntry),
		order:    make([]string, 0, maxItems),
		maxItems: maxItems,
	}
}

// Store records a file snapshot in the cache.
func (c *ReadCache) Store(path string, lines []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove old entry if exists
	if _, ok := c.entries[path]; ok {
		for i, p := range c.order {
			if p == path {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}

	// Evict oldest if at capacity
	if len(c.order) >= c.maxItems {
		oldest := c.order[0]
		delete(c.entries, oldest)
		c.order = c.order[1:]
	}

	// Store a copy to avoid aliasing
	copied := make([]string, len(lines))
	copy(copied, lines)
	c.entries[path] = &cacheEntry{lines: copied}
	c.order = append(c.order, path)
}

// Get retrieves a file snapshot from the cache.
// Returns nil if not found.
func (c *ReadCache) Get(path string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, ok := c.entries[path]; ok {
		copied := make([]string, len(entry.lines))
		copy(copied, entry.lines)
		return copied
	}
	return nil
}
