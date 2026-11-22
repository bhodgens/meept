package task

import "testing"

// The task-payload envelope strip must stay byte-identical in semantics to
// agent.StripClaimsEvidenceOrProse (the canonical implementation it mirrors;
// internal/task must never import internal/agent). These are the same shapes
// agent.envelope_prose_test.go pins, replayed across the fork boundary.

func TestStripClaimsEvidenceOrProse_EnvelopeRemoved(t *testing.T) {
	raw := `Some prose before.

{"claims":["did the thing"],"evidence":[{"type":"file","path":"/tmp/x","description":"the file exists"}]}

Trailing prose.`
	want := "Some prose before.\n\n\n\nTrailing prose."
	if got := StripClaimsEvidenceOrProse(raw); got != want {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want %q", got, want)
	}
}

func TestStripClaimsEvidenceOrProse_WholeEnvelopeRecoversProse(t *testing.T) {
	raw := `{"claims":["wrote the report","validated syntax"],"evidence":["the report exists at report.md"]}`
	want := "- wrote the report\n- validated syntax\n- the report exists at report.md"
	if got := StripClaimsEvidenceOrProse(raw); got != want {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want %q", got, want)
	}
}

func TestStripClaimsEvidenceOrProse_ExecutorReport(t *testing.T) {
	raw := `job job-123 completed by agent coder: {"status":"success","accomplished":["fixed the parser"],"observations":["tests pass"]}`
	want := "- fixed the parser\n- tests pass"
	if got := StripClaimsEvidenceOrProse(raw); got != want {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want %q", got, want)
	}
}

func TestStripClaimsEvidenceOrProse_TruncatedExecutorReport(t *testing.T) {
	raw := `job job-9 completed by agent coder: {"status":"success","accomplished":["wrote the doc"],"not_done":["rev`
	want := "- wrote the doc\n- not done: rev"
	if got := StripClaimsEvidenceOrProse(raw); got != want {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want %q", got, want)
	}
}

func TestStripClaimsEvidenceOrProse_PlainProseUnchanged(t *testing.T) {
	raw := "  plain user prose, no envelope here.\n"
	if got := StripClaimsEvidenceOrProse(raw); got != "plain user prose, no envelope here." {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want trimmed plain prose", got)
	}
}

func TestStripClaimsEvidenceOrProse_StatusJSONNotExecutorReport(t *testing.T) {
	// A bare {"status": ...} object with no report list keys is NOT an
	// executor report and must pass through untouched (minus trim).
	raw := `{"status":"ok","latency_ms":12}`
	if got := StripClaimsEvidenceOrProse(raw); got != raw {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want input unchanged", got)
	}
}

func TestStripStepForDisplay_StripsDescriptionAndResult(t *testing.T) {
	step := &TaskStep{
		ID:          "st-1",
		Description: "## Session context (most recent work in this conversation)\n- earlier work\n\n## Your Task\n\nwrite the summary",
		Result:      `job j-1 completed by agent coder: {"status":"success","accomplished":["summary written"]}`,
	}
	got := StripStepForDisplay(step)
	if got.Description != "write the summary" {
		t.Fatalf("Description = %q, want %q", got.Description, "write the summary")
	}
	if got.Result != "- summary written" {
		t.Fatalf("Result = %q, want %q", got.Result, "- summary written")
	}
}

func TestStripStepForDisplay_NilSafe(t *testing.T) {
	if got := StripStepForDisplay(nil); got != nil {
		t.Fatalf("StripStepForDisplay(nil) = %+v, want nil", got)
	}
}
