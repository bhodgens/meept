package memory

import "regexp"

// memoryContextSpanRE matches a complete injected `<memory-context>` fence,
// including any leading whitespace/newlines that carried it, so stripped
// content does not leave blank gaps behind.
var memoryContextSpanRE = regexp.MustCompile(`(?s)\s*<memory-context>.*?</memory-context>\s*`)

// SanitizeForStore strips injected `<memory-context>...</memory-context>`
// spans (with surrounding whitespace) from s. Memory-recording paths call it
// before persisting raw content so a memory that was echoed back into a
// conversation cannot be re-recorded and feed back into future prompts
// (feedback-loop guard, mirrors TAM l0-recorder.ts).
//
// This is the narrow export surface for subpackages (internal/memory/l0);
// the stripping logic lives here and must not be duplicated by callers.
func SanitizeForStore(s string) string {
	return memoryContextSpanRE.ReplaceAllString(s, "")
}
