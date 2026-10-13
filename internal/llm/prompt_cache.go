package llm

import "strings"

// PromptCacheBoundary is a sentinel marker inserted into system prompt section
// lists to delineate static (cacheable across sessions) content from dynamic
// (session-specific) content. Sections appearing before the boundary are
// classified as static; sections after it are classified as dynamic.
const PromptCacheBoundary = "__MEEPT_PROMPT_CACHE_BOUNDARY__"

// CacheScope indicates the caching lifetime of a prompt block.
type CacheScope int

const (
	// CacheScopeNone indicates the block should not be cached.
	CacheScopeNone CacheScope = iota
	// CacheScopeStatic indicates the block is stable across sessions and can
	// be cached indefinitely (until the prompt template changes).
	CacheScopeStatic
	// CacheScopeSession indicates the block is specific to the current session
	// and should be cached only for the session lifetime.
	CacheScopeSession
)

// SystemPromptBlock is a contiguous chunk of system prompt text tagged with
// its cache scope.
type SystemPromptBlock struct {
	Text       string
	CacheScope CacheScope
}

// ClassifyPromptSections splits a slice of prompt sections into static and
// dynamic groups based on the PromptCacheBoundary marker. Sections before the
// boundary are static; sections after it are dynamic. Empty strings and the
// boundary marker itself are excluded from both groups. If no boundary is
// present, all non-empty sections are classified as static.
func ClassifyPromptSections(sections []string) (static []string, dynamic []string) {
	boundaryFound := false
	for _, s := range sections {
		if s == PromptCacheBoundary {
			boundaryFound = true
			continue
		}
		if s == "" {
			continue
		}
		if !boundaryFound {
			static = append(static, s)
		} else {
			dynamic = append(dynamic, s)
		}
	}
	return static, dynamic
}

// BuildSystemPromptBlocks groups classified prompt sections into
// SystemPromptBlock values suitable for prefix-aware caching. Static sections
// are joined into a single CacheScopeStatic block; dynamic sections are joined
// into a single CacheScopeSession block. Empty groups produce no block.
func BuildSystemPromptBlocks(sections []string) []SystemPromptBlock {
	static, dynamic := ClassifyPromptSections(sections)
	var blocks []SystemPromptBlock
	if len(static) > 0 {
		blocks = append(blocks, SystemPromptBlock{
			Text:       strings.Join(static, "\n\n"),
			CacheScope: CacheScopeStatic,
		})
	}
	if len(dynamic) > 0 {
		blocks = append(blocks, SystemPromptBlock{
			Text:       strings.Join(dynamic, "\n\n"),
			CacheScope: CacheScopeSession,
		})
	}
	return blocks
}

// SplitSystemPromptOnBoundary splits a flat system prompt on the
// PromptCacheBoundary sentinel ONLY — never on blank lines — returning
// sections in the shape ClassifyPromptSections/BuildSystemPromptBlocks expect:
// the static text before the boundary, the boundary itself, and the dynamic
// text after it. The separator newlines around the sentinel are consumed so
// block text matches the pre-L03 byte-for-byte output for well-formed
// prompts. A prompt without the boundary returns as a single-element slice
// (all-static classification).
func SplitSystemPromptOnBoundary(s string) []string {
	idx := strings.Index(s, PromptCacheBoundary)
	if idx < 0 {
		return []string{s}
	}
	before := strings.TrimSuffix(s[:idx], "\n\n")
	after := strings.TrimPrefix(s[idx+len(PromptCacheBoundary):], "\n\n")
	return []string{before, PromptCacheBoundary, after}
}

// StripPromptCacheBoundary removes the PromptCacheBoundary sentinel and any
// surrounding blank-line separators from a system prompt string. This must be
// called before sending the prompt to any provider so the internal marker is
// never leaked to the API.
func StripPromptCacheBoundary(s string) string {
	s = strings.ReplaceAll(s, PromptCacheBoundary+"\n\n", "")
	s = strings.ReplaceAll(s, "\n\n"+PromptCacheBoundary, "")
	s = strings.ReplaceAll(s, PromptCacheBoundary, "")
	return s
}
