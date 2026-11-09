package llm

import "testing"

// TestStripThinkingExported pins the exported StripThinking symbol
// (internal/llm/reasoning_strip.go) that the agent conversation layer
// (Leaf 03) consumes.
func TestStripThinkingExported(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "multiple closed blocks anywhere",
			input: "Before<think>one</think> middle <think>two</think>After<think>three</think>End",
			want:  "Before middle AfterEnd",
		},
		{
			name:  "leading unclosed block to end-of-string",
			input: "<think>reasoning cut off mid-thought",
			want:  "",
		},
		{
			name:  "leading reasoning_content fragment",
			input: `reasoning_content: "I should look at the tests first"` + "\nThe actual answer",
			want:  "The actual answer",
		},
		{
			name:  "no reasoning passes byte-identical",
			input: "  Plain answer, no reasoning wire-forms.  ",
			want:  "Plain answer, no reasoning wire-forms.",
		},
		{
			name:  "result is trimmed",
			input: "\n\t <think>why</think> \n Trimmed answer \t\n",
			want:  "Trimmed answer",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			// Live defect 2026-11-09 voice path: llama.cpp consumed the
			// opening tag, the chain-of-thought plus a lone closing tag
			// leaked into content, and the talk loop spoke it to the
			// caller's speaker before the real reply followed.
			name:  "openless leak: text before a lone closing tag drops",
			input: "\nWe need to understand what the user is asking. Thus we ask.\n</think>\nCould you clarify what you would like?",
			want:  "Could you clarify what you would like?",
		},
		{
			name:  "mid-text openless closer keeps only following text",
			input: "Preamble reasoning here.</think>Real answer.",
			want:  "Real answer.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripThinking(tt.input)
			if got != tt.want {
				t.Errorf("StripThinking(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
