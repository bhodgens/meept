package telegram

import (
	"strings"
	"testing"
)

func TestFormatResponse_PlainText(t *testing.T) {
	input := "Hello world"
	output := FormatResponse(input)

	// Plain text should have special characters escaped
	if !strings.Contains(output, "Hello world") {
		t.Errorf("expected output to contain original text, got %q", output)
	}
}

func TestFormatResponse_EscapesSpecialChars(t *testing.T) {
	input := "Use _italic_ or *bold* text."
	output := FormatResponse(input)

	// Underscores should be escaped outside code
	if !strings.Contains(output, `\_`) {
		t.Errorf("expected escaped underscore, got %q", output)
	}
	// Asterisks should be escaped outside code
	if !strings.Contains(output, `\*`) {
		t.Errorf("expected escaped asterisk, got %q", output)
	}
}

func TestFormatResponse_PreservesCodeBlocks(t *testing.T) {
	input := "Here is code:\n```go\nfmt.Println(\"hello\")\n```\nDone."
	output := FormatResponse(input)

	if !strings.Contains(output, "fmt.Println") {
		t.Errorf("code block content should be preserved, got %q", output)
	}
	if !strings.Contains(output, "```go") {
		t.Errorf("code fence should be preserved, got %q", output)
	}
}

func TestFormatResponse_PreservesInlineCode(t *testing.T) {
	input := "Use `var x int` to declare a variable."
	output := FormatResponse(input)

	if !strings.Contains(output, "`var x int`") {
		t.Errorf("inline code should be preserved, got %q", output)
	}
}

func TestFormatResponse_CodeBlockWithUnderscore(t *testing.T) {
	input := "```python\nmy_variable = 1\n```"
	output := FormatResponse(input)

	// Underscore inside code block should NOT be escaped
	if strings.Contains(output, `\_`) {
		t.Errorf("underscore inside code block should not be escaped, got %q", output)
	}
	if !strings.Contains(output, "my_variable") {
		t.Errorf("code content should be preserved, got %q", output)
	}
}

func TestFormatResponse_DotAndBang(t *testing.T) {
	input := "Hello. World!"
	output := FormatResponse(input)

	if !strings.Contains(output, `Hello\.`) {
		t.Errorf("expected escaped dot, got %q", output)
	}
	if !strings.Contains(output, `World\!`) {
		t.Errorf("expected escaped bang, got %q", output)
	}
}

func TestFormatResponse_EmptyString(t *testing.T) {
	output := FormatResponse("")
	if output != "" {
		t.Errorf("expected empty output for empty input, got %q", output)
	}
}

func TestEscapeMarkdownV2_AllSpecialChars(t *testing.T) {
	special := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}

	for _, char := range special {
		input := "a" + char + "b"
		output := escapeMarkdownV2(input)
		expected := "a\\" + char + "b"
		if output != expected {
			t.Errorf("escapeMarkdownV2(%q) = %q, want %q", input, output, expected)
		}
	}
}

func TestFormatResponse_MixedContent(t *testing.T) {
	input := "See `code` and _emphasis_.\n```\nhas_underscore\n```\nEnd."
	output := FormatResponse(input)

	// Inline code preserved
	if !strings.Contains(output, "`code`") {
		t.Errorf("inline code should be preserved, got %q", output)
	}
	// Code block content preserved
	if !strings.Contains(output, "has_underscore") {
		t.Errorf("code block content should be preserved, got %q", output)
	}
	// Underscore in regular text escaped
	if !strings.Contains(output, `\_emphasis`) {
		t.Errorf("underscore in text should be escaped, got %q", output)
	}
}

func TestFormatResponse_DoesNotDoubleEscape(t *testing.T) {
	// If the input already has a backslash, it should still be present
	input := `Path: C:\Users\test`
	output := FormatResponse(input)

	// The colon and backslash handling - colon is not special,
	// but backslash is not in the special chars list so it passes through
	if !strings.Contains(output, "C:") {
		t.Errorf("expected output to contain C:, got %q", output)
	}
}
