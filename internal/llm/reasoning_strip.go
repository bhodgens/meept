package llm

import (
	"regexp"
	"strings"
)

// thinkBlockRe matches inline <think>...</think> reasoning blocks
// (case-insensitive, across newlines). Some thinking models inline their
// chain-of-thought into the assistant content even when asked not to.
var thinkBlockRe = regexp.MustCompile(`(?is)<think>.*?</think>`)

// unclosedThinkRe matches a leading <think> block whose closing tag never
// arrives (e.g. a truncated/stop-sequence-ended response): everything from
// the opening tag to end-of-string.
var unclosedThinkRe = regexp.MustCompile(`(?is)^\s*<think>.*$`)

// reasoningContentLineRe matches a leading "reasoning_content": "..." text
// fragment some servers inline at the top of the content when the reasoning
// channel is misconfigured.
var reasoningContentLineRe = regexp.MustCompile(`(?is)^\s*"?reasoning_content"?\s*:\s*"(?:[^"\\]|\\.)*"\s*`)

// StripThinking removes reasoning wire-forms from model content text:
// closed <think>...</think> blocks anywhere, a leading unclosed <think>
// block, and a leading reasoning_content fragment. Trimmed. Exported so
// the agent loop / conversation layer can strip history-side too.
func StripThinking(content string) string {
	out := thinkBlockRe.ReplaceAllString(content, "")
	out = unclosedThinkRe.ReplaceAllString(out, "")
	out = reasoningContentLineRe.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}
