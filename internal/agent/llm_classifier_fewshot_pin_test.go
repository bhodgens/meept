package agent

import (
	"strings"
	"testing"
)

// Issue #53 direction 1: few-shot anchoring in the classification prompt.
// The 8B classifier wandered on readback tails and imperative quickplan
// phrasings; a short per-lane few-shot block anchors the adjudicated surface
// forms. The size ceiling keeps the 10s classifier timeout budget safe —
// future prompt edits may not bloat past it.

// classifierPromptMaxBytes is the hard ceiling for buildClassificationPrompt
// output (with a representative input). Baseline at few-shot introduction:
// 2056 bytes; few-shot block lands well under 3KB total. If a future edit
// trips this, shrink the prompt instead of raising the ceiling — the
// classifier runs under a 10s timeout.
const classifierPromptMaxBytes = 3072

func TestClassifierPrompt_FewShotMarkersPresent(t *testing.T) {
	c := &LLMClassifier{}
	prompt := c.buildClassificationPrompt("create a file named notes.txt then tell me the path")

	for _, marker := range []string{
		// code: action + readback tail stays in the code lane.
		"create a file named X then tell me the path",
		// quickplan: imperative multi-step execution.
		"implement the plan tasks in order",
		// recall: verification readback of past work.
		"did the change get made?",
		// recall: session artifact recall.
		"what files did you make?",
	} {
		if !strings.Contains(prompt, marker) {
			t.Errorf("classification prompt missing few-shot marker %q\nprompt:\n%s", marker, prompt)
		}
	}
}

func TestClassifierPrompt_SizeCeiling(t *testing.T) {
	c := &LLMClassifier{}
	prompt := c.buildClassificationPrompt("create a file named notes.txt in the current directory containing the word hello, then tell me the full path")
	if got := len(prompt); got > classifierPromptMaxBytes {
		t.Errorf("classification prompt grew to %d bytes (ceiling %d) — shrink it, the 10s classifier timeout budget does not allow bloat", got, classifierPromptMaxBytes)
	}
}

// Issue #53 direction 1 (task 2): the multi-intent prompt must carry the
// readback-collapse rule — an action plus a follow-up report of its result is
// ONE intent, so "create X then tell me the path" is not split into two.
func TestClassifyMultiPrompt_CarriesReadbackRule(t *testing.T) {
	prompt := buildClassifyMultiPrompt("create a file named notes.txt then tell me the path")
	lower := strings.ToLower(prompt)
	for _, marker := range []string{
		"one intent",
		"tell me",
		"show me",
	} {
		if !strings.Contains(lower, marker) {
			t.Errorf("ClassifyMulti prompt missing readback-rule marker %q\nprompt:\n%s", marker, prompt)
		}
	}
}
