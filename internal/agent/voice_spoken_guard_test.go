package agent

// Pins for the voice-spoken guard (defect A, 2026-11-10 link recording
// /tmp/link-rec-1794333239 transcript b): the secretary's SPOKEN reply
// carried the loop's honesty label plus the evidence-template vocabulary
// and TTS read it to the caller. Two properties pinned here:
//   - the SHARED machine-shaped guard (reply_guard.go detectors) is
//     unchanged: the recorded leak shape passes it exactly as before
//     (the text lane ships the label — TestChainStability_NudgeBudgetRecovery),
//     so the voice lane needs its own superset classifier;
//   - the Validation anchor ships byte-identical for every default-config
//     (chat/task) loop and sheds ONLY the JSON evidence-format block when
//     OmitEvidenceFormatTemplate is set.

import (
	"strings"
	"testing"
)

// recordedLeakReply is the transcript-b leak shape, verbatim in its
// machine-shaped parts: [unverified label] + evidence-template vocabulary
// (tool_call_id / success=true / memory_id).
const recordedLeakReply = "[unverified: no tools were executed this turn] Correction: I did not create or modify any files this turn.\n\n" +
	"Verified evidence from an earlier real tool execution:\n" +
	"- memory_retain succeeded, saving the call state fact. Evidence: tool_call_id call_ad75ede0b24b4933aa30100b returned success=true and memory_id 3a0dae49-96c3-4f33-b888-285e57fd39f2.\n\n" +
	"Accomplished:\n- Greeted the caller and kept the line open as requested."

func TestSpokenGuard_CatchesRecordedLeak(t *testing.T) {
	tok, trips := SpokenReplyViolation(recordedLeakReply)
	if !trips {
		t.Fatal("recorded leak shape must trip the spoken guard")
	}
	if tok == "" {
		t.Fatal("tripped classification must name the trigger")
	}
}

func TestSpokenGuard_SharedShapesAreSupersets(t *testing.T) {
	// A raw tool-result dump (the shared guard's shape) is spoken garbage
	// too — the spoken classifier must catch it via the shared detector.
	dump := `{"memory_id":"abc","success":true}`
	if _, trips := SpokenReplyViolation(dump); !trips {
		t.Fatal("raw JSON dump must trip the spoken guard")
	}
}

func TestSpokenGuard_ProsePassesAndIsIdempotent(t *testing.T) {
	prose := []string{
		"the caller reached voicemail, i'll pass your message along.",
		"sure — i can take a note. who's calling and what do they need?",
		VoiceSpokenFallbackLine,                  // the canned line must never re-trip
		"the evidence shows the build is green.", // bare word, not a JSON key
	}
	for _, p := range prose {
		if _, trips := SpokenReplyViolation(p); trips {
			t.Errorf("clean prose %q trips the spoken guard (false positive)", p)
		}
		// Idempotent: classifying twice changes nothing.
		if _, trips := SpokenReplyViolation(p); trips {
			t.Errorf("second pass of %q disagrees — classifier not idempotent", p)
		}
	}
}

func TestSpokenGuard_LeakShapePassesSharedGuard(t *testing.T) {
	// Text-lane posture pin: the recorded leak passes the SHARED guard
	// byte-identical (the chat lane deliberately ships the [unverified:
	// label per TestChainStability_NudgeBudgetRecovery). If this ever
	// trips, the text lane changed — do NOT quietly align the voice fix
	// to it without re-reading that pin.
	out, _, replaced := classifyReplyGuard(recordedLeakReply)
	if replaced || out != recordedLeakReply {
		t.Fatalf("shared guard altered the leak shape (replaced=%v) — text-lane posture changed", replaced)
	}
}

// anchorExpectedFull is the pre-defect-A anchor literal, reconstructed as
// the byte-identity golden for default-config loops.
const anchorExpectedFull = `## Validation & Escalation Instructions

**Tool use comes first.** If the task requires reading, writing, running, or
extracting anything, you MUST call the appropriate tool. A tool call is the
only way work happens in this system — describing work, or returning data in
prose, does not perform it. Do not answer "extract this into JSON" with a
JSON blob in prose: call the tool that does the extraction.

**Before reporting completion**, you must verify:
1. All described work in the task has been completed
2. Evidence (file hashes, exit codes, command output) supports your claims
3. No error indicators remain in the output

**Evidence format** (include in your final response, AFTER any tool calls have
returned — this summarizes work already done, it is not a substitute for it):
` + "```" + `json
{
  "claims": ["description of what was done"],
  "evidence": [{"type": "file_exists", "path": "/path/to/file"}]
}
` + "```" + `

**If you cannot complete the task**: Report status as "partial" or "failed" with specific reasons.
**If blocked**: Describe what you need and suggest next steps.`

func TestValidationAnchor_ChatLaneByteIdentical(t *testing.T) {
	loop := NewAgentLoop("anchor-pin", t.TempDir())
	got := loop.buildValidationAnchorInstructions()
	if got != anchorExpectedFull {
		t.Fatalf("default-config anchor drifted from the pre-fix literal:\ngot:\n%s\nwant:\n%s", got, anchorExpectedFull)
	}
}

func TestValidationAnchor_ToolsLessSlimDropsOnlyTheTemplate(t *testing.T) {
	loop := NewAgentLoop("anchor-pin-slim", t.TempDir(),
		WithAgentConfig(AgentConfig{OmitEvidenceFormatTemplate: true}))
	got := loop.buildValidationAnchorInstructions()
	if strings.Contains(got, "**Evidence format**") || strings.Contains(got, `"claims"`) {
		t.Fatalf("slimmed anchor still carries the JSON evidence template:\n%s", got)
	}
	// The prose rules stay byte-identical to the full anchor's.
	if !strings.Contains(got, "**Tool use comes first.**") {
		t.Error("slimmed anchor lost the tool-use prose rule")
	}
	if !strings.Contains(got, "**Before reporting completion**, you must verify:") {
		t.Error("slimmed anchor lost the completion-checklist prose")
	}
	if !strings.Contains(got, `Report status as "partial" or "failed"`) {
		t.Error("slimmed anchor lost the partial/failed reporting line")
	}
	// The slimmed anchor is the full anchor minus the template block.
	full := anchorExpectedFull
	start := strings.Index(full, "**Evidence format**")
	end := strings.Index(full, "**If you cannot complete the task**")
	if start < 0 || end < start {
		t.Fatal("golden literal drifted — update anchorExpectedFull")
	}
	want := full[:start] + full[end:]
	if got != want {
		t.Errorf("slimmed anchor != full anchor minus template block:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
