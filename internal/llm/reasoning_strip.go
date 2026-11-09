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

// openlessCloseRe matches reasoning that leaked with the OPENING tag
// already consumed server-side (llama.cpp reasoning parsing strips the
// opener but the model emitted it before the first parseable boundary):
// the content carries a lone </think> with the chain-of-thought text
// before it. Everything up to and including the first closing tag is
// reasoning; the user reply follows it. Implemented with a non-greedy
// DOTALL match up to the first </think> (Go regexp has no lookahead, so
// the original (?:(?!...</think>).) form does not compile).
var openlessCloseRe = regexp.MustCompile(`(?s)^.*?</think>`)

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
	// Openless leak: a lone </think> with no opener (the opening tag was
	// consumed upstream) means everything BEFORE it is leaked reasoning -
	// drop up to and including the first closer, keep the real reply.
	out = openlessCloseRe.ReplaceAllString(out, "")
	out = reasoningContentLineRe.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}
