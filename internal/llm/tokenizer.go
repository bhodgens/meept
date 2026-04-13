package llm

import (
	"math"
	"strings"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer provides token counting for text content.
// Implementations can use actual tokenizers (tiktoken) or heuristics.
type Tokenizer interface {
	// CountTokens returns the number of tokens in the given text.
	CountTokens(text string) int
}

// HeuristicTokenizer uses a simple character-based heuristic (3 chars/token).
type HeuristicTokenizer struct{}

// CountTokens estimates tokens using 3 characters per token heuristic.
func (h *HeuristicTokenizer) CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return int(math.Ceil(float64(len(text)) / 3.0))
}

// Ensure HeuristicTokenizer implements Tokenizer
var _ Tokenizer = (*HeuristicTokenizer)(nil)

// TiktokenTokenizer wraps tiktoken for accurate token counting.
// Uses github.com/pkoukk/tiktoken-go for Go bindings.
type TiktokenTokenizer struct {
	encoding string
	tke      *tiktoken.Tiktoken
	mu       sync.RWMutex
}

// NewTiktokenTokenizer creates a new tiktoken-based tokenizer.
// encoding should be a tiktoken encoding name like:
//   - "cl100k_base" (GPT-4, GPT-3.5-turbo)
//   - "p50k_base" (GPT-3, Codex)
//   - "r50k_base" (GPT-2, GPT-3 text-davinci)
//   - "o200k_base" (GPT-4o)
// If tiktoken fails to load the encoding, falls back to heuristic.
func NewTiktokenTokenizer(encoding string) *TiktokenTokenizer {
	t := &TiktokenTokenizer{
		encoding: encoding,
	}
	// Try to load the encoding
	tke, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		// Encoding not found, will fall back to heuristic
		t.tke = nil
	} else {
		t.tke = tke
	}
	return t
}

// CountTokens returns accurate token count using tiktoken.
// Falls back to heuristic if tiktoken encoding was not loaded.
func (t *TiktokenTokenizer) CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}

	t.mu.RLock()
	tke := t.tke
	t.mu.RUnlock()

	if tke == nil {
		// Fall back to heuristic
		return int(math.Ceil(float64(len(text)) / 3.0))
	}

	// Use tiktoken EncodeOrdinary for standard tokenization (no special tokens)
	tokens := tke.EncodeOrdinary(text)
	return len(tokens)
}

// Ensure TiktokenTokenizer implements Tokenizer
var _ Tokenizer = (*TiktokenTokenizer)(nil)

// TokenCache provides caching for token counts to avoid recomputation.
type TokenCache struct {
	tokenizer Tokenizer
	cache     sync.Map // map[string]int
}

// NewTokenCache creates a new token cache wrapping a tokenizer.
func NewTokenCache(tokenizer Tokenizer) *TokenCache {
	return &TokenCache{
		tokenizer: tokenizer,
	}
}

// CountTokens returns cached token count or computes and caches it.
func (c *TokenCache) CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}

	// Try cache first
	if cached, ok := c.cache.Load(text); ok {
		return cached.(int)
	}

	// Compute and cache
	count := c.tokenizer.CountTokens(text)
	c.cache.Store(text, count)

	return count
}

// Ensure TokenCache implements Tokenizer
var _ Tokenizer = (*TokenCache)(nil)

// NewTokenizerForModel creates an appropriate tokenizer based on model/provider.
// Returns a tiktoken tokenizer for known models, or heuristic as fallback.
func NewTokenizerForModel(modelID string) Tokenizer {
	// Map model IDs to tiktoken encodings
	// GPT-4, GPT-3.5-turbo use cl100k_base
	if isGPT4Family(modelID) || isGPT35Family(modelID) {
		return NewTiktokenTokenizer("cl100k_base")
	}
	// GPT-4o uses o200k_base
	if isGPT4oFamily(modelID) {
		return NewTiktokenTokenizer("o200k_base")
	}
	// GPT-3 uses p50k_base
	if isGPT3Family(modelID) {
		return NewTiktokenTokenizer("p50k_base")
	}
	// Default to cl100k_base (most common for modern models)
	// Many providers use compatible tokenizers
	return NewTiktokenTokenizer("cl100k_base")
}

// isGPT4Family checks if model is GPT-4 family
func isGPT4Family(modelID string) bool {
	return containsIgnoreCase(modelID, "gpt-4") && !containsIgnoreCase(modelID, "gpt-4o")
}

// isGPT4oFamily checks if model is GPT-4o family
func isGPT4oFamily(modelID string) bool {
	return containsIgnoreCase(modelID, "gpt-4o")
}

// isGPT35Family checks if model is GPT-3.5-turbo family
func isGPT35Family(modelID string) bool {
	return containsIgnoreCase(modelID, "gpt-3.5-turbo")
}

// isGPT3Family checks if model is GPT-3 family
func isGPT3Family(modelID string) bool {
	return containsIgnoreCase(modelID, "text-davinci") || containsIgnoreCase(modelID, "ada-001")
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// EstimateTokenCountHeuristic estimates tokens using the 3 chars/token heuristic.
// This is exported for use by other packages that need a quick estimate.
func EstimateTokenCountHeuristic(content string) int {
	if len(content) == 0 {
		return 0
	}
	return int(math.Ceil(float64(len(content)) / 3.0))
}
