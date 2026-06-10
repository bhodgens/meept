package metrics

import (
	"testing"
)

func TestNewResponseAnalyzer(t *testing.T) {
	analyzer := NewResponseAnalyzer()
	if analyzer == nil {
		t.Fatal("NewResponseAnalyzer returned nil")
	}
	if len(analyzer.lazyPatterns) == 0 {
		t.Error("Expected lazy patterns to be initialized")
	}
}

func TestResponseAnalyzer_Analyze_Basic(t *testing.T) {
	analyzer := NewResponseAnalyzer()

	tests := []struct {
		name       string
		response   string
		tokenCount int
		want       *ResponseQuality
	}{
		{
			name:       "empty response",
			response:   "",
			tokenCount: 0,
			want: &ResponseQuality{
				WellFormed:      true,
				ParseErrors:     nil,
				HasCodeBlocks:   false,
				HasExplanations: false,
				IsLazy:          false,
				LazyReason:      "",
				TokenCount:      0,
				CodeTokenPct:    0,
			},
		},
		{
			name:       "response with code blocks",
			response:   "Here's the code:\n```go\nfunc main() {}\n```",
			tokenCount: 100,
			want: &ResponseQuality{
				WellFormed:      true,
				HasCodeBlocks:   true,
				HasExplanations: true,
				TokenCount:      100,
			},
		},
		{
			name:       "response with explanation",
			response:   "I'll help you with that problem.",
			tokenCount: 50,
			want: &ResponseQuality{
				WellFormed:      true,
				HasCodeBlocks:   false,
				HasExplanations: true,
				TokenCount:      50,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.Analyze(tt.response, tt.tokenCount)
			if got.WellFormed != tt.want.WellFormed {
				t.Errorf("WellFormed = %v, want %v", got.WellFormed, tt.want.WellFormed)
			}
			if got.HasCodeBlocks != tt.want.HasCodeBlocks {
				t.Errorf("HasCodeBlocks = %v, want %v", got.HasCodeBlocks, tt.want.HasCodeBlocks)
			}
			if got.HasExplanations != tt.want.HasExplanations {
				t.Errorf("HasExplanations = %v, want %v", got.HasExplanations, tt.want.HasExplanations)
			}
			if got.TokenCount != tt.want.TokenCount {
				t.Errorf("TokenCount = %v, want %v", got.TokenCount, tt.want.TokenCount)
			}
		})
	}
}

func TestResponseAnalyzer_LazyDetection(t *testing.T) {
	analyzer := NewResponseAnalyzer()

	tests := []struct {
		name       string
		response   string
		wantLazy   bool
		wantReason string
	}{
		{
			name:       "rest of code pattern",
			response:   "// rest of code goes here",
			wantLazy:   true,
			wantReason: "//\\s*rest of code",
		},
		{
			name:       "rest of file pattern",
			response:   "# rest of the file",
			wantLazy:   true,
			wantReason: "#\\s*rest of the file",
		},
		{
			name:       "ellipsis existing code",
			response:   "... existing code",
			wantLazy:   true,
			wantReason: "\\.\\.\\.\\s*existing code",
		},
		{
			name:       "hash ellipsis pattern",
			response:   "# ...",
			wantLazy:   true,
			wantReason: "#\\s*\\.\\.\\.",
		},
		{
			name:       "etc comment",
			response:   "// etc.",
			wantLazy:   true,
			wantReason: "//\\s*etc\\.",
		},
		{
			name:       "normal response not lazy",
			response:   "Here is the implementation of the function.",
			wantLazy:   false,
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.Analyze(tt.response, 100)
			if got.IsLazy != tt.wantLazy {
				t.Errorf("IsLazy = %v, want %v for response: %q", got.IsLazy, tt.wantLazy, tt.response)
			}
			if tt.wantLazy && got.LazyReason == "" {
				t.Error("Expected LazyReason to be set for lazy response")
			}
		})
	}
}

func TestIsWellFormed(t *testing.T) {
	tests := []struct {
		name       string
		editFormat string
		response   string
		want       bool
	}{
		{
			name:       "editblock valid",
			editFormat: "editblock",
			response:   "<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE",
			want:       true,
		},
		{
			name:       "editblock invalid - missing search",
			editFormat: "editblock",
			response:   "=======\nnew\n>>>>>>> REPLACE",
			want:       false,
		},
		{
			name:       "editblock invalid - missing replace",
			editFormat: "editblock",
			response:   "<<<<<<< SEARCH\nold\n=======",
			want:       false,
		},
		{
			name:       "editblock-fenced valid",
			editFormat: "editblock-fenced",
			response:   "```\n<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE\n```",
			want:       true,
		},
		{
			name:       "editblock-fenced missing fence",
			editFormat: "editblock-fenced",
			response:   "<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE",
			want:       false,
		},
		{
			name:       "udiff valid",
			editFormat: "udiff",
			response:   "--- a/file.txt\n+++ b/file.txt\n@@ -1,2 +1,3 @@",
			want:       true,
		},
		{
			name:       "udiff missing minus",
			editFormat: "udiff",
			response:   "+++ b/file.txt\n@@ -1,2 +1,3 @@",
			want:       false,
		},
		{
			name:       "udiff missing plus",
			editFormat: "udiff",
			response:   "--- a/file.txt\n@@ -1,2 +1,3 @@",
			want:       false,
		},
		{
			name:       "unknown format returns true",
			editFormat: "unknown",
			response:   "any content",
			want:       true,
		},
		{
			name:       "empty format returns true",
			editFormat: "",
			response:   "any content",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWellFormed(tt.response, tt.editFormat)
			if got != tt.want {
				t.Errorf("isWellFormed(%q, %q) = %v, want %v", tt.response, tt.editFormat, got, tt.want)
			}
		})
	}
}