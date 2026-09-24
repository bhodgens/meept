package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// TestStripClaimsEvidence_BareEnvelope verifies the unfenced claims/evidence
// envelope mandated by the step prompt is removed and the prose kept
// (e2e run 2026-09-23 EHpc4r: the raw envelope reached the reply guard, which
// replaced the whole reply with a canned line).
func TestStripClaimsEvidence_BareEnvelope(t *testing.T) {
	in := "I created the file at /var/work/project/hello.txt.\n\n" +
		`{"claims":["Created hello.txt at /var/work/project/hello.txt"],` +
		`"evidence":[{"type":"file_exists","path":"/var/work/project/hello.txt","size":5}]}`
	got := StripClaimsEvidence(in)
	if !strings.Contains(got, "hello.txt") || strings.Contains(got, "\"claims\"") {
		t.Errorf("StripClaimsEvidence kept envelope or dropped prose: %q", got)
	}
}

// TestStripClaimsEvidence_EnvelopeOnly verifies an envelope-only result strips
// to empty so the caller can fall through to the next-best step.
func TestStripClaimsEvidence_EnvelopeOnly(t *testing.T) {
	in := `{"claims":["did thing"],"evidence":[{"type":"file_exists"}]}`
	if got := StripClaimsEvidence(in); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestStripClaimsEvidence_Fenced also handled: StripClaimsEvidence removes
// the envelope whether fenced or bare.
func TestStripClaimsEvidence_FencedLeftAlone(t *testing.T) {
	in := "```json\n{\"claims\":[],\"evidence\":[{\"type\":\"file_exists\"}]}\n```"
	got := StripClaimsEvidence(in)
	if strings.Contains(got, "\"claims\"") {
		t.Errorf("envelope not removed: %q", got)
	}
}

// TestBestStepResult_StripsEnvelope pins the chat-handler seam end to end:
// the sync-wait reply built from a step result must not carry the envelope.
func TestBestStepResult_StripsEnvelope(t *testing.T) {
	steps := []*task.TaskStep{
		{Sequence: 2, State: task.StepApproved, Result: `{"claims":["x"],"evidence":[]}`},
		{Sequence: 1, State: task.StepApproved, Result: "prose answer naming hello.txt"},
	}
	got := bestStepResult(steps)
	if strings.Contains(got, "\"claims\"") {
		t.Errorf("envelope leaked into reply: %q", got)
	}
}
