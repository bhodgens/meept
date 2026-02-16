package tui

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/types"
)

func TestDefaultStyles(t *testing.T) {
	styles := DefaultStyles()
	if styles == nil {
		t.Fatal("DefaultStyles returned nil")
	}

	// Check that key styles are initialized
	if styles.Title.GetBold() != true {
		t.Error("Title should be bold")
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		seconds  float64
		contains []string
	}{
		{-1, []string{"n/a"}},
		{0, []string{"00s"}},
		{45, []string{"45s"}},
		{90, []string{"1m", "30s"}},
		{3661, []string{"1h", "1m", "1s"}},
		{90061, []string{"1d", "1h", "1m", "1s"}},
	}

	for _, tt := range tests {
		result := types.FormatUptime(tt.seconds)
		for _, substr := range tt.contains {
			if !strings.Contains(result, substr) {
				t.Errorf("types.FormatUptime(%f) = %q, expected to contain %q", tt.seconds, result, substr)
			}
		}
	}
}

func TestRenderProgressBar(t *testing.T) {
	styles := DefaultStyles()

	tests := []struct {
		width   int
		percent float64
	}{
		{10, 0.0},
		{10, 0.5},
		{10, 1.0},
		{10, -0.1}, // Should clamp to 0
		{10, 1.5},  // Should clamp to 1
		{20, 0.25},
	}

	for _, tt := range tests {
		result := RenderProgressBar(tt.width, tt.percent, styles)
		if !strings.HasPrefix(result, "[") || !strings.HasSuffix(result, "]") {
			t.Errorf("RenderProgressBar(%d, %f) = %q, expected brackets", tt.width, tt.percent, result)
		}
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"hi", 2, "hi"},
		{"hello", 3, "hel"},
		{"", 5, ""},
		{"test", 4, "test"},
		{"testing", 4, "t..."},
	}

	for _, tt := range tests {
		result := types.TruncateString(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("types.TruncateString(%q, %d) = %q, expected %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestRepeatChar(t *testing.T) {
	tests := []struct {
		char     rune
		n        int
		expected string
	}{
		{'=', 5, "====="},
		{'-', 3, "---"},
		{'x', 0, ""},
		{'a', -1, ""},
	}

	for _, tt := range tests {
		result := repeatChar(tt.char, tt.n)
		if result != tt.expected {
			t.Errorf("repeatChar(%q, %d) = %q, expected %q", tt.char, tt.n, result, tt.expected)
		}
	}
}
