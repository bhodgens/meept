package builtin

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"

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

// snapshotTagLen is the length of snapshot tag strings in characters.
const snapshotTagLen = 4

// GenerateSnapshotTag returns a random 4-character hexadecimal snapshot tag.
// Tags are minted per read/search call and stored in ReadCache.
func GenerateSnapshotTag() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		// fall back to a deterministic tag on entropy failure
		return "0000"
	}
	return fmt.Sprintf("%02x%02x", b[0], b[1])
}

// FormatSnapshotHashLine formats a single line as "LINE_NUMBER:TAG:HASH|CONTENT".
// This is the enhanced hashline format that includes a session snapshot tag.
func FormatSnapshotHashLine(lineNum int, snapshotTag string, line string) string {
	return fmt.Sprintf("%d:%s:%s|%s", lineNum, snapshotTag, ComputeLineHash(line), line)
}

// FormatSnapshotHashLines formats multiple lines with snapshot-tagged hashlines.
// startLine is 1-based. snapshotTag is a 4-hex-character tag from GenerateSnapshotTag.
func FormatSnapshotHashLines(lines []string, startLine int, snapshotTag string) string {
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(FormatSnapshotHashLine(startLine+i, snapshotTag, line))
	}
	return sb.String()
}

// ParseSnapshotAnchor parses a snapshot-tagged anchor string and returns the
// line number, snapshot tag, and hash. it accepts either "LINE:TAG:HASH"
// (new format) or "LINE:HASH" (legacy format).
// Returns (0, "", "", error) on invalid format.
func ParseSnapshotAnchor(anchor string) (lineNum int, snapshotTag string, hash string, err error) {
	if anchor == "BOF" || anchor == "EOF" {
		return 0, "", anchor, nil
	}

	parts := strings.SplitN(anchor, ":", 3)
	switch len(parts) {
	case 2:
		// legacy "LINE:HASH" format
		var n int
		if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
			return 0, "", "", fmt.Errorf("invalid line number in anchor %q: %w", anchor, err)
		}
		if len(parts[1]) != 2 {
			return 0, "", "", fmt.Errorf("invalid hash in anchor %q: expected 2-char bigram", anchor)
		}
		return n, "", parts[1], nil
	case 3:
		// new "LINE:TAG:HASH" format
		var n int
		if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
			return 0, "", "", fmt.Errorf("invalid line number in anchor %q: %w", anchor, err)
		}
		if len(parts[1]) != snapshotTagLen {
			return 0, "", "", fmt.Errorf("invalid snapshot tag in anchor %q: expected %d-hex tag", anchor, snapshotTagLen)
		}
		if len(parts[2]) != 2 {
			return 0, "", "", fmt.Errorf("invalid hash in anchor %q: expected 2-char bigram", anchor)
		}
		return n, parts[1], parts[2], nil
	default:
		return 0, "", "", fmt.Errorf("invalid anchor format %q: expected LINE:HASH or LINE:TAG:HASH", anchor)
	}
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

// SnapshotEntry records a single historical snapshot for session chain recovery.
type SnapshotEntry struct {
	Lines     []string
	Tag       string
	Timestamp time.Time
}

// ReadCache is a thread-safe LRU cache for file snapshots used by the edit tool
// for stale-anchor recovery.
type ReadCache struct {
	mu          sync.RWMutex
	entries     map[string]*cacheEntry
	order       []string // LRU order (front = oldest)
	maxItems    int
	editHistory map[string][]SnapshotEntry
}

type cacheEntry struct {
	lines       []string // 0-indexed lines
	snapshotTag string   // snapshot tag for this cached version
}

// NewReadCache creates a ReadCache with the given maximum entries.
func NewReadCache(maxItems int) *ReadCache {
	if maxItems <= 0 {
		maxItems = 30
	}
	return &ReadCache{
		entries:     make(map[string]*cacheEntry),
		order:       make([]string, 0, maxItems),
		maxItems:    maxItems,
		editHistory: make(map[string][]SnapshotEntry),
	}
}

// Store records a file snapshot in the cache with an optional snapshot tag.
func (c *ReadCache) Store(path string, lines []string) {
	c.StoreWithTag(path, lines, "")
}

// StoreWithTag records a file snapshot in the cache with a snapshot tag.
// The tag allows editors to reference the exact version they read.
// It also appends the snapshot to editHistory (capped at 10 per path).
func (c *ReadCache) StoreWithTag(path string, lines []string, snapshotTag string) {
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
	c.entries[path] = &cacheEntry{lines: copied, snapshotTag: snapshotTag}
	c.order = append(c.order, path)

	// Append to edit history, capped at 10 per path.
	const maxHistoryPerPath = 10
	hist := c.editHistory[path]
	entry := SnapshotEntry{
		Lines:     copied,
		Tag:       snapshotTag,
		Timestamp: time.Now(),
	}
	hist = append(hist, entry)
	if len(hist) > maxHistoryPerPath {
		hist = hist[len(hist)-maxHistoryPerPath:]
	}
	c.editHistory[path] = hist
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

// GetTagged retrieves a file snapshot and its tag from the cache.
// Returns (nil, "") if not found.
func (c *ReadCache) GetTagged(path string) ([]string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, ok := c.entries[path]; ok {
		copied := make([]string, len(entry.lines))
		copy(copied, entry.lines)
		return copied, entry.snapshotTag
	}
	return nil, ""
}

// GetByTag looks up a cached snapshot by its snapshot tag.
// Returns nil if no snapshot with that tag is found.
func (c *ReadCache) GetByTag(tag string) []string {
	if tag == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, entry := range c.entries {
		if entry.snapshotTag == tag {
			copied := make([]string, len(entry.lines))
			copy(copied, entry.lines)
			return copied
		}
	}
	return nil
}

// GetHistory returns the edit history for a given path, ordered newest-first.
// Each entry contains a copy of the lines at that snapshot.
func (c *ReadCache) GetHistory(path string) []SnapshotEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hist := c.editHistory[path]
	if len(hist) == 0 {
		return nil
	}

	// Return newest-first with copies of the lines to avoid aliasing.
	result := make([]SnapshotEntry, len(hist))
	for i, entry := range hist {
		result[i] = SnapshotEntry{
			Tag:       entry.Tag,
			Timestamp: entry.Timestamp,
		}
		if entry.Lines != nil {
			result[i].Lines = make([]string, len(entry.Lines))
			copy(result[i].Lines, entry.Lines)
		}
	}
	// Reverse so newest is first.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
