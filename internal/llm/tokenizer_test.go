package llm

import (
	"strings"
	"testing"
)

func TestHeuristicTokenizer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"single word", "hello", 2},          // 5 chars / 3 = 1.67 -> 2
		{"short sentence", "hello world", 4}, // 11 chars / 3 = 3.67 -> 4
		{"longer text", "The quick brown fox jumps over the lazy dog", 15}, // 44 chars / 3 = 14.67 -> 15
	}

	tokenizer := &HeuristicTokenizer{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenizer.CountTokens(tt.input)
			if got != tt.expected {
				t.Errorf("CountTokens(%q) = %d, expected %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTiktokenTokenizer(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		input    string
	}{
		{"cl100k_base", "cl100k_base", "Hello, world! This is a test."},
		{"o200k_base", "o200k_base", "Hello, world! This is a test."},
		{"p50k_base", "p50k_base", "Hello, world! This is a test."},
		{"cl100k_base code", "cl100k_base", "func main() { fmt.Println(\"Hello\") }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer := NewTiktokenTokenizer(tt.encoding)
			count := tokenizer.CountTokens(tt.input)

			if count <= 0 {
				t.Errorf("CountTokens(%q) returned %d, expected positive number", tt.input, count)
			}

			// Verify caching works
			count2 := tokenizer.CountTokens(tt.input)
			if count != count2 {
				t.Errorf("Cached count differs: first=%d, cached=%d", count, count2)
			}
		})
	}
}

func TestNewTokenizerForModel(t *testing.T) {
	tests := []struct {
		name          string
		modelID       string
		expectEncoder string // Expected tiktoken encoding
	}{
		// GPT-4 family
		{"gpt-4", "gpt-4", "cl100k_base"},
		{"gpt-4-turbo", "gpt-4-turbo", "cl100k_base"},

		// GPT-4o family
		{"gpt-4o", "gpt-4o", "o200k_base"},
		{"gpt-4o-mini", "gpt-4o-mini", "o200k_base"},

		// GPT-3.5 family
		{"gpt-3.5-turbo", "gpt-3.5-turbo", "cl100k_base"},

		// GPT-3 family
		{"text-davinci-003", "text-davinci-003", "p50k_base"},

		// Qwen family (HIGH PRIORITY)
		{"qwen2.5-coder", "qwen2.5-coder", "cl100k_base"},
		{"qwen-7b", "qwen-7b", "cl100k_base"},

		// GLM family (HIGH PRIORITY)
		{"glm-4.7", "glm-4.7", "cl100k_base"},
		{"glm-4.5-air", "glm-4.5-air", "cl100k_base"},

		// Mistral family
		{"dolphin-mistral-7b", "dolphin-mistral-7b", "p50k_base"},
		{"mistral-7b", "mistral-7b", "p50k_base"},

		// Llama family
		{"llama3.2", "llama3.2", "cl100k_base"}, // Falls back if llama3 not available
		{"llama3", "llama3", "cl100k_base"},

		// Unknown models default to cl100k_base
		{"unknown", "unknown-model", "cl100k_base"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer := NewTokenizerForModel(tt.modelID)

			if tokenizer == nil {
				t.Fatalf("NewTokenizerForModel(%q) returned nil", tt.modelID)
			}

			// Verify tokenizer works
			count := tokenizer.CountTokens("test input")
			if count <= 0 {
				t.Errorf("CountTokens returned non-positive value: %d", count)
			}
		})
	}
}

func TestTokenizerFamilyDetection(t *testing.T) {
	tests := []struct {
		name      string
		modelID   string
		isQwen    bool
		isGLM     bool
		isMistral bool
		isLlama   bool
	}{
		{"qwen detection", "qwen2.5-coder", true, false, false, false},
		{"qwen detection 2", "qwen-72b", true, false, false, false},
		{"glm detection", "glm-4.7", false, true, false, false},
		{"glm detection 2", "glm-4-air", false, true, false, false},
		{"mistral detection", "mistral-7b-v0.1", false, false, true, false},
		{"mistral detection 2", "dolphin-mistral", false, false, true, false},
		{"llama detection", "llama3.2", false, false, false, true},
		{"llama detection 2", "meta-llama-3", false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.isQwen && !isQwenFamily(tt.modelID) {
				t.Errorf("Expected %q to be detected as Qwen family", tt.modelID)
			}
			if tt.isGLM && !isGLMFamily(tt.modelID) {
				t.Errorf("Expected %q to be detected as GLM family", tt.modelID)
			}
			if tt.isMistral && !isMistralFamily(tt.modelID) {
				t.Errorf("Expected %q to be detected as Mistral family", tt.modelID)
			}
			if tt.isLlama && !isLlamaFamily(tt.modelID) {
				t.Errorf("Expected %q to be detected as Llama family", tt.modelID)
			}
		})
	}
}

func TestEstimateTokenCountHeuristic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"short", "hello", 2},
		{"medium", "Hello, world! This is a longer test string.", 15},
		{"code", "func main() { return true }", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokenCountHeuristic(tt.input)
			if got != tt.expected {
				t.Errorf("EstimateTokenCountHeuristic(%q) = %d, expected %d", tt.input, got, tt.expected)
			}
		})
	}
}

func BenchmarkTokenizer(b *testing.B) {
	sampleText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)

	b.Run("Heuristic", func(b *testing.B) {
		tokenizer := &HeuristicTokenizer{}
		for range b.N {
			_ = tokenizer.CountTokens(sampleText)
		}
	})

	b.Run("Tiktoken_cl100k_base", func(b *testing.B) {
		tokenizer := NewTiktokenTokenizer("cl100k_base")
		for range b.N {
			_ = tokenizer.CountTokens(sampleText)
		}
	})

	b.Run("Tiktoken_direct", func(b *testing.B) {
		tokenizer := NewTiktokenTokenizer("cl100k_base")
		for range b.N {
			_ = tokenizer.CountTokens(sampleText)
		}
	})
}
