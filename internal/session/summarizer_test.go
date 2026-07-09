package session

import (
	"testing"
)

func TestExtractSimpleResult(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantDesc string
	}{
		{
			name:     "what are your capabilities",
			input:    "what are your capabilities",
			wantName: "capabilities", // Skips fillers "what", "are", "your", takes first substantive word
			wantDesc: "what are your capabilities",
		},
		{
			name:     "explore the code for the button",
			input:    "explore the code for the button",
			wantName: "explore", // "explore" is not a filler, but "the" is, so only one word
			wantDesc: "explore the code for the button",
		},
		{
			name:     "how do I fix the null pointer",
			input:    "how do I fix the null pointer",
			wantName: "fix", // Skips fillers, takes "fix"
			wantDesc: "how do i fix the null pointer",
		},
		{
			name:     "simple greeting",
			input:    "hello",
			wantName: "hello",
			wantDesc: "hello",
		},
		{
			name:     "empty input",
			input:    "",
			wantName: "chat",
			wantDesc: "new conversation",
		},
		{
			name:     "all fillers",
			input:    "what is the",
			wantName: "what", // Falls back to first word when all are fillers
			wantDesc: "what is the",
		},
		{
			name:     "with punctuation",
			input:    "What are your capabilities?",
			wantName: "capabilities",
			wantDesc: "what are your capabilities?",
		},
		{
			name:     "long message gets truncated description",
			input:    "I want to understand how the authentication system works in this project and what methods are available",
			wantName: "want", // "I" is filler, "want" is first non-filler
			wantDesc: "i want to understand how the authentication system works ...",
		},
		{
			name:     "tell me about",
			input:    "tell me about the database schema",
			wantName: "about", // "tell", "me" are fillers; "about" is first non-filler
			wantDesc: "tell me about the database schema",
		},
		{
			name:     "i need help with",
			input:    "i need help with fixing this bug",
			wantName: "need help", // "i" is filler, "need help" are first two non-fillers
			wantDesc: "i need help with fixing this bug",
		},
		{
			name:     "debug authentication system",
			input:    "debug authentication system",
			wantName: "debug authentication", // Two substantive words
			wantDesc: "debug authentication system",
		},
		{
			name:     "fix null pointer in auth",
			input:    "fix null pointer in auth",
			wantName: "fix null",
			wantDesc: "fix null pointer in auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSimpleResult(tt.input)
			if result.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", result.Name, tt.wantName)
			}
			if result.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", result.Description, tt.wantDesc)
			}
		})
	}
}
