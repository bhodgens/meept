package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
)

// CacheKeyBuilder constructs CacheKey values from prompts, model IDs, and
// message histories. When file-aware caching is enabled it extracts file
// paths referenced in the prompt text, reads their contents from disk, and
// includes SHA256 content hashes in the resulting key so that a cached
// response is automatically invalidated when any referenced file changes.
type CacheKeyBuilder struct {
	// FileAware controls whether file references are extracted and hashed.
	FileAware bool
	// Logger is used for diagnostic output. Defaults to slog.Default() when nil.
	Logger *slog.Logger
}

// NewCacheKeyBuilder returns a builder initialised with the given file-aware flag.
func NewCacheKeyBuilder(fileAware bool) *CacheKeyBuilder {
	return &CacheKeyBuilder{
		FileAware: fileAware,
		Logger:    slog.Default().With("component", "cache_key_builder"),
	}
}

// Regular expressions for extracting file references from prompt text.
// Compiled once at package init for reuse.
var (
	// file: /abs/or/rel/path.go
	reFilePrefix = regexp.MustCompile(`(?i)\bfile:\s*([^\s,;)}\]]+)`)

	// @path/to/file.go
	reAtPath = regexp.MustCompile(`@([\w./\-]+\.[\w]+)`)

	// path/to/file.go:42 (colon + digits at end -- line reference)
	rePathWithLine = regexp.MustCompile(`(?:^|[\s(=\["'])((?:[\w.\-]+/)+[\w.\-]+\.[\w]+):(\d+)(?:$|[\s,;)}\]])`)

	// Bare absolute path: /Users/name/project/file.go
	reAbsolutePath = regexp.MustCompile(`(?:^|[\s(=\["'])(/(?:[\w.\-]+/)+[\w.\-]+\.[\w]+)(?:$|[\s,;)}\]])`)
)

// knownNonFileExtensions is a set of extensions that commonly appear in
// prompts but are very unlikely to be local file references. Extracted paths
// ending with these extensions are excluded.
var knownNonFileExtensions = map[string]bool{
	".com": true,
	".org": true,
	".net": true,
	".io":  true,
	".dev": true,
	".ai":  true,
	".co":  true,
	".uk":  true,
	".gov": true,
	".edu": true,
	".mil": true,
	".us":  true,
	".eu":  true,
	".tv":  true,
	".me":  true,
	".app": true,
	".txt": true, // ambiguous, but commonly prose not paths
}

// ExtractFileReferences parses the prompt text and returns a deduplicated,
// sorted slice of file paths referenced in it. The following patterns are
// recognised:
//
//   - Absolute paths: /Users/name/project/file.go
//   - "file:" prefix:  file: /path/to/file.go
//   - "@" notation:    @src/main.go
//   - Line references: path/to/file.go:42
func (b *CacheKeyBuilder) ExtractFileReferences(prompt string) []string {
	if !b.FileAware || prompt == "" {
		return nil
	}

	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		p = cleanExtractedPath(p)
		if p == "" || seen[p] {
			return
		}
		if isKnownNonFile(p) {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}

	// Pattern 1: file: /path/to/file.go
	for _, match := range reFilePrefix.FindAllStringSubmatch(prompt, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}

	// Pattern 2: @path/to/file.go
	for _, match := range reAtPath.FindAllStringSubmatch(prompt, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}

	// Pattern 3: path/to/file.go:42
	for _, match := range rePathWithLine.FindAllStringSubmatch(prompt, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}

	// Pattern 4: /absolute/path/file.go
	for _, match := range reAbsolutePath.FindAllStringSubmatch(prompt, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}

	sort.Strings(paths)
	return paths
}

// ComputeFileHashes reads each file path and returns a map from path to
// hex-encoded SHA256 hash of its contents. Paths that cannot be read are
// silently skipped (a warning is logged). The returned map is never nil.
func (b *CacheKeyBuilder) ComputeFileHashes(paths []string) map[string]string {
	hashes := make(map[string]string, len(paths))

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			b.Logger.Warn("cannot read file for cache key hash, skipping",
				"path", p, "error", err)
			continue
		}
		h := sha256.Sum256(data)
		hashes[p] = hex.EncodeToString(h[:])
	}

	return hashes
}

// ComputePromptHash returns the hex-encoded SHA256 hash of the serialised
// message list. The hash is computed over a deterministic concatenation of
// role, content, name, tool_call_id, and serialised tool calls for every
// message.
func (b *CacheKeyBuilder) ComputePromptHash(messages []ChatMessage) string {
	h := sha256.New()
	for _, m := range messages {
		// Role
		h.Write([]byte(m.Role))
		h.Write([]byte{0}) // separator
		// Content
		h.Write([]byte(m.Content))
		h.Write([]byte{0})
		// Name
		h.Write([]byte(m.Name))
		h.Write([]byte{0})
		// Tool call ID
		h.Write([]byte(m.ToolCallID))
		h.Write([]byte{0})
		// Tool calls -- deterministic ordering guaranteed by slice order
		for _, tc := range m.ToolCalls {
			h.Write([]byte(tc.ID))
			h.Write([]byte{0})
			h.Write([]byte(tc.Type))
			h.Write([]byte{0})
			h.Write([]byte(tc.Function.Name))
			h.Write([]byte{0})
			h.Write([]byte(tc.Function.Arguments))
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Build constructs a complete CacheKey from the given model ID, messages, and
// (optionally) a standalone prompt string. If FileAware is enabled, file
// references are extracted from the prompt and all message contents, their
// contents are hashed, and the hashes are included in the key.
func (b *CacheKeyBuilder) Build(prompt string, modelID string, messages []ChatMessage) CacheKey {
	key := CacheKey{
		ModelID: modelID,
	}

	// Compute the prompt hash from messages
	key.PromptHash = b.ComputePromptHash(messages)

	// File-aware: extract file references and hash them
	if b.FileAware {
		// Collect text from the standalone prompt plus all message contents
		var allText strings.Builder
		if prompt != "" {
			allText.WriteString(prompt)
			allText.WriteByte('\n')
		}
		for _, m := range messages {
			allText.WriteString(m.Content)
			allText.WriteByte('\n')
		}

		paths := b.ExtractFileReferences(allText.String())
		if len(paths) > 0 {
			key.FileHashes = b.ComputeFileHashes(paths)
		}
	}

	return key
}

// cleanExtractedPath normalises a raw extracted path string: trims surrounding
// punctuation and whitespace, strips trailing dots, and returns empty if the
// result is too short to be a plausible file path.
func cleanExtractedPath(p string) string {
	p = strings.TrimRight(p, ".,;:!?'\"")
	p = strings.TrimSpace(p)
	// Strip trailing colon+digits that may be a line reference not caught by regex groups
	if idx := strings.LastIndex(p, ":"); idx > 0 {
		suffix := p[idx+1:]
		if isAllDigits(suffix) {
			p = p[:idx]
		}
	}
	if len(p) < 3 { // shortest plausible: a.go
		return ""
	}
	return p
}

// isAllDigits returns true if every rune in s is an ASCII digit.
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// isKnownNonFile rejects paths whose extension maps to a common TLD or other
// non-file token (e.g. ".com", ".org").
func isKnownNonFile(p string) bool {
	dot := strings.LastIndex(p, ".")
	if dot < 0 {
		return false
	}
	ext := strings.ToLower(p[dot:])
	return knownNonFileExtensions[ext]
}
