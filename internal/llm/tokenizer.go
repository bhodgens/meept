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
	if text == "" {
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
//
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
	if text == "" {
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
	// Qwen family (qwen2.5-coder, etc.) - uses cl100k_base compatible BPE
	if isQwenFamily(modelID) {
		return NewTiktokenTokenizer("cl100k_base")
	}
	// GLM family (glm-4.7, glm-4.5-air) - uses cl100k_base compatible BPE
	if isGLMFamily(modelID) {
		return NewTiktokenTokenizer("cl100k_base")
	}
	// Mistral family - uses p50k_base (similar to GPT-3)
	if isMistralFamily(modelID) {
		return NewTiktokenTokenizer("p50k_base")
	}
	// Llama family (llama3, llama3.1, llama3.2) - uses tiktoken llama3 encoding
	if isLlamaFamily(modelID) {
		// Try llama3 encoding first, fall back to cl100k_base if unavailable
		t := NewTiktokenTokenizer("llama3")
		if t.tke != nil {
			return t
		}
		// Fall back to cl100k_base if llama3 encoding not available
		return NewTiktokenTokenizer("cl100k_base")
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

// isQwenFamily checks if model is Qwen family
func isQwenFamily(modelID string) bool {
	return containsIgnoreCase(modelID, "qwen")
}

// isGLMFamily checks if model is GLM family (Z.Ai models)
func isGLMFamily(modelID string) bool {
	return containsIgnoreCase(modelID, "glm")
}

// isMistralFamily checks if model is Mistral family
func isMistralFamily(modelID string) bool {
	return containsIgnoreCase(modelID, "mistral")
}

// isLlamaFamily checks if model is Llama family
func isLlamaFamily(modelID string) bool {
	return containsIgnoreCase(modelID, "llama")
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// EstimateTokenCountHeuristic estimates tokens using the 3 chars/token heuristic.
// This is exported for use by other packages that need a quick estimate.
func EstimateTokenCountHeuristic(content string) int {
	if content == "" {
		return 0
	}
	return int(math.Ceil(float64(len(content)) / 3.0))
}
