package security

import (
	"testing"
)

func TestSanitizeC0Controls(t *testing.T) {
	sanitizer := NewInputSanitizer(StrictnessStandard)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "NUL character removed",
			input:    "hello\x00world",
			expected: "helloworld",
		},
		{
			name:     "backspace removed",
			input:    "hello\x08world",
			expected: "helloworld",
		},
		{
			name:     "escape removed",
			input:    "hello\x1bworld",
			expected: "helloworld",
		},
		{
			name:     "DEL removed",
			input:    "hello\x7fworld",
			expected: "helloworld",
		},
		{
			name:     "tab preserved",
			input:    "hello\tworld",
			expected: "hello\tworld",
		},
		{
			name:     "newline preserved",
			input:    "hello\nworld",
			expected: "hello\nworld",
		},
		{
			name:     "carriage return preserved",
			input:    "hello\rworld",
			expected: "hello\rworld",
		},
		{
			name:     "multi-byte UTF-8 intact",
			input:    "hello\x00\u4e16\u754c\x08",
			expected: "hello\u4e16\u754c",
		},
		{
			name:     "mixed controls",
			input:    "\x00\x08\x1b\x7f\t\n\r",
			expected: "\t\n\r",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result.CleanText != tt.expected {
				t.Errorf("Sanitize(%q) = %q; want %q", tt.input, result.CleanText, tt.expected)
			}
		})
	}
}
