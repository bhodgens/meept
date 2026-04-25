package telegram

import (
	"fmt"
	"regexp"
	"strings"
)

// markdownV2SpecialChars lists characters that Telegram MarkdownV2 requires
// to be escaped outside of code blocks and nested entities.
var markdownV2SpecialChars = []string{
	"_", "*", "[", "]", "(", ")", "~", "`",
	">", "#", "+", "-", "=", "|", "{", "}", ".", "!",
}

// codeBlockRe matches fenced code blocks (```...```).
var codeBlockRe = regexp.MustCompile("(?s)```.*?```")

// inlineCodeRe matches inline code (`...`).
var inlineCodeRe = regexp.MustCompile("`[^`]+`")

// FormatResponse converts agent output to Telegram MarkdownV2 format.
// It preserves code blocks and inline code while escaping special characters
// in the surrounding text.
func FormatResponse(content string) string {
	// Protect code blocks by replacing them with placeholders.
	codeBlocks := codeBlockRe.FindAllString(content, -1)
	for i, block := range codeBlocks {
		content = strings.Replace(content, block, placeholder("CB", i), 1)
	}

	// Protect inline code.
	inlineCodes := inlineCodeRe.FindAllString(content, -1)
	for i, code := range inlineCodes {
		content = strings.Replace(content, code, placeholder("IC", i), 1)
	}

	// Escape special MarkdownV2 characters in the remaining text.
	content = escapeMarkdownV2(content)

	// Restore code blocks (unchanged).
	for i, block := range codeBlocks {
		content = strings.Replace(content, placeholder("CB", i), block, 1)
	}

	// Restore inline code (unchanged).
	for i, code := range inlineCodes {
		content = strings.Replace(content, placeholder("IC", i), code, 1)
	}

	return content
}

// escapeMarkdownV2 escapes all special MarkdownV2 characters in s.
func escapeMarkdownV2(s string) string {
	for _, char := range markdownV2SpecialChars {
		s = strings.ReplaceAll(s, char, "\\"+char)
	}
	return s
}

// placeholder returns a sentinel string that will not be present in normal
// agent output so we can safely swap code spans in and out.
func placeholder(prefix string, idx int) string {
	return fmt.Sprintf("\x00%s%d\x00", prefix, idx)
}
